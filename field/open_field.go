// Open Field mode: lets teams drive outside of the match framework, with each alliance station controlling its own
// robot's enabled and autonomous/teleop state while the arena otherwise continues to act as an FMS.

package field

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Team254/cheesy-arena-lite/game"
	"github.com/Team254/cheesy-arena-lite/model"
)

const OpenFieldMatchName = "Open Field"

var errOpenFieldNotImplemented = errors.New("Open Field mode is not implemented yet.")

// Turns Open Field mode on or off. Can only be enabled while no match is in progress.
func (arena *Arena) SetOpenFieldMode(enabled bool) error {
	if enabled == arena.OpenFieldMode {
		return nil
	}
	if enabled && arena.MatchState != PreMatch {
		return fmt.Errorf("Cannot enter Open Field mode while there is a match in progress or with results pending.")
	}

	arena.openFieldMutex.Lock()
	for _, allianceStation := range arena.AllianceStations {
		allianceStation.resetOpenFieldState()
	}
	arena.OpenFieldMode = enabled
	arena.sendDsPacket(false, false)
	arena.openFieldMutex.Unlock()

	// Swap in a test match that carries over the teams currently on the field, without reconfiguring the network.
	displayName := "Test Match"
	if enabled {
		displayName = OpenFieldMatchName
	}
	arena.CurrentMatch = &model.Match{
		Type:        "test",
		DisplayName: displayName,
		Red1:        arena.stationTeamId("R1"),
		Red2:        arena.stationTeamId("R2"),
		Red3:        arena.stationTeamId("R3"),
		Blue1:       arena.stationTeamId("B1"),
		Blue2:       arena.stationTeamId("B2"),
		Blue3:       arena.stationTeamId("B3"),
	}
	arena.MatchLoadNotifier.Notify()
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Enables or disables the robot in the given alliance station while in Open Field mode.
func (arena *Arena) OpenFieldSetEnabled(station string, enabled bool) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	if enabled {
		if allianceStation.OpenFieldEnabled {
			return nil
		}
		if err = arena.checkCanOpenFieldEnable(allianceStation); err != nil {
			return err
		}
		allianceStation.OpenFieldEnabled = true
		allianceStation.clearOpenFieldAutoTimer()
	} else {
		allianceStation.OpenFieldEnabled = false
		allianceStation.clearOpenFieldAutoTimer()
	}
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Sets whether the robot in the given alliance station runs in autonomous (true) or teleop (false) mode.
func (arena *Arena) OpenFieldSetAuto(station string, auto bool) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	if allianceStation.OpenFieldAuto == auto {
		return nil
	}
	if allianceStation.OpenFieldEnabled {
		return fmt.Errorf("Cannot change the mode of the robot in station %s while it is enabled.", station)
	}
	allianceStation.OpenFieldAuto = auto
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Runs a timed autonomous period for the given alliance station, after which the robot is disabled.
func (arena *Arena) OpenFieldRunAuto(station string) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	if allianceStation.OpenFieldEnabled {
		return fmt.Errorf("Cannot start autonomous for station %s while the robot is already enabled.", station)
	}
	if err = arena.checkCanOpenFieldEnable(allianceStation); err != nil {
		return err
	}
	allianceStation.OpenFieldAuto = true
	allianceStation.OpenFieldEnabled = true
	allianceStation.openFieldAutoEndTime = time.Now().Add(time.Duration(game.MatchTiming.AutoDurationSec) * time.Second)
	allianceStation.OpenFieldAutoRemainingSec = float64(game.MatchTiming.AutoDurationSec)
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Immediately disables every robot on the field.
func (arena *Arena) OpenFieldDisableAll() {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	for _, allianceStation := range arena.AllianceStations {
		allianceStation.OpenFieldEnabled = false
		allianceStation.clearOpenFieldAutoTimer()
	}
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
}

// Emergency-stops the robot in the given alliance station while in Open Field mode. The e-stop remains latched until
// it is cleared via OpenFieldClearEstop.
func (arena *Arena) OpenFieldEstop(station string) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	allianceStation.Estop = true
	allianceStation.OpenFieldEnabled = false
	allianceStation.clearOpenFieldAutoTimer()
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Clears a latched emergency stop for the given alliance station while in Open Field mode. The robot remains disabled.
func (arena *Arena) OpenFieldClearEstop(station string) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	if arena.isPlcTeamEstopPressed(station) {
		return fmt.Errorf("Cannot clear the emergency stop for station %s while its stop button is still pressed.",
			station)
	}
	allianceStation.Estop = false
	allianceStation.Astop = false
	allianceStation.OpenFieldEnabled = false
	allianceStation.clearOpenFieldAutoTimer()
	arena.sendDsPacket(false, false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// Returns nil if the robot in the given station could be enabled right now, or an error describing why not.
func (arena *Arena) CheckCanOpenFieldEnable(station string) error {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()

	allianceStation, err := arena.getOpenFieldStation(station)
	if err != nil {
		return err
	}
	return arena.checkCanOpenFieldEnable(allianceStation)
}

func (arena *Arena) getOpenFieldStation(station string) (*AllianceStation, error) {
	if !arena.OpenFieldMode {
		return nil, fmt.Errorf("Open Field mode is not active.")
	}
	allianceStation, ok := arena.AllianceStations[station]
	if !ok {
		return nil, fmt.Errorf("Invalid alliance station '%s'.", station)
	}
	return allianceStation, nil
}

func (arena *Arena) checkCanOpenFieldEnable(allianceStation *AllianceStation) error {
	if arena.Plc.IsEnabled() && !arena.Plc.IsHealthy {
		return fmt.Errorf("Cannot enable robot while PLC is not healthy.")
	}
	if arena.Plc.GetFieldEstop() {
		return fmt.Errorf("Cannot enable robot while field emergency stop is active.")
	}
	if allianceStation.Estop || allianceStation.Astop {
		return fmt.Errorf("Cannot enable robot while its emergency stop is active.")
	}
	if allianceStation.Bypass {
		return fmt.Errorf("Cannot enable robot while its station is bypassed.")
	}
	if allianceStation.Team == nil {
		return fmt.Errorf("Cannot enable robot while no team is assigned to the station.")
	}
	if allianceStation.DsConn == nil {
		return fmt.Errorf("Cannot enable robot until its driver station is connected.")
	}
	if !allianceStation.DsConn.RobotLinked {
		return fmt.Errorf("Cannot enable robot until it is linked to its driver station.")
	}
	return nil
}

// Enforces safety conditions and auto timers for all stations in Open Field mode. Must be called with the Open Field
// mutex held. Returns true if any station's state changed such that a DS packet should be sent right away.
func (arena *Arena) updateOpenField() bool {
	changed := false
	fieldEstop := arena.Plc.GetFieldEstop()
	plcUnhealthy := arena.Plc.IsEnabled() && !arena.Plc.IsHealthy
	for _, allianceStation := range arena.AllianceStations {
		if allianceStation.OpenFieldEnabled {
			if fieldEstop || plcUnhealthy || allianceStation.Estop || allianceStation.Astop ||
				allianceStation.Bypass || allianceStation.Team == nil || allianceStation.DsConn == nil ||
				!allianceStation.DsConn.RobotLinked {
				allianceStation.OpenFieldEnabled = false
				allianceStation.clearOpenFieldAutoTimer()
				changed = true
			}
		}

		if !allianceStation.openFieldAutoEndTime.IsZero() {
			remaining := time.Until(allianceStation.openFieldAutoEndTime).Seconds()
			if remaining <= 0 {
				allianceStation.OpenFieldEnabled = false
				allianceStation.clearOpenFieldAutoTimer()
				changed = true
			} else {
				allianceStation.OpenFieldAutoRemainingSec = remaining
			}
		}
	}
	return changed
}

// Returns the value to send to the driver station for the remaining number of seconds in the "match".
func (allianceStation *AllianceStation) openFieldMatchSecondsRemaining() int {
	if !allianceStation.openFieldAutoEndTime.IsZero() {
		return int(math.Ceil(math.Max(time.Until(allianceStation.openFieldAutoEndTime).Seconds(), 0)))
	}
	if allianceStation.OpenFieldAuto {
		return game.MatchTiming.AutoDurationSec
	}
	return game.MatchTiming.TeleopDurationSec
}

func (allianceStation *AllianceStation) clearOpenFieldAutoTimer() {
	allianceStation.openFieldAutoEndTime = time.Time{}
	allianceStation.OpenFieldAutoRemainingSec = 0
}

func (allianceStation *AllianceStation) resetOpenFieldState() {
	allianceStation.OpenFieldEnabled = false
	allianceStation.OpenFieldAuto = false
	allianceStation.clearOpenFieldAutoTimer()
}

// Clears the Open Field state of the given station, e.g. when its team or driver station connection changes.
func (arena *Arena) resetOpenFieldStation(station string) {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()
	if allianceStation, ok := arena.AllianceStations[station]; ok {
		wasEnabled := allianceStation.OpenFieldEnabled
		allianceStation.resetOpenFieldState()
		if wasEnabled && arena.OpenFieldMode {
			arena.sendDsPacket(false, false)
		}
	}
}

// Disables the robot in the given station and cancels any running auto timer, e.g. when its driver station connects
// or disconnects, so that it doesn't unexpectedly re-enable.
func (arena *Arena) disableOpenFieldStation(station string) {
	arena.openFieldMutex.Lock()
	defer arena.openFieldMutex.Unlock()
	if allianceStation, ok := arena.AllianceStations[station]; ok {
		wasEnabled := allianceStation.OpenFieldEnabled
		allianceStation.OpenFieldEnabled = false
		allianceStation.clearOpenFieldAutoTimer()
		if wasEnabled && arena.OpenFieldMode {
			arena.sendDsPacket(false, false)
		}
	}
}

func (arena *Arena) stationTeamId(station string) int {
	if team := arena.AllianceStations[station].Team; team != nil {
		return team.Id
	}
	return 0
}

func (arena *Arena) isPlcTeamEstopPressed(station string) bool {
	redEstops, blueEstops := arena.Plc.GetTeamEstops()
	switch station {
	case "R1":
		return redEstops[0]
	case "R2":
		return redEstops[1]
	case "R3":
		return redEstops[2]
	case "B1":
		return blueEstops[0]
	case "B2":
		return blueEstops[1]
	case "B3":
		return blueEstops[2]
	}
	return false
}
