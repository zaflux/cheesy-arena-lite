package field

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Team254/cheesy-arena-lite/game"
	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
)

// Assigns a team with a fake, robot-linked driver station connection to the given station.
func setupOpenFieldStation(t *testing.T, arena *Arena, teamId int, station string) *DriverStationConnection {
	assert.Nil(t, arena.assignTeam(teamId, station))
	dsConn := &DriverStationConnection{TeamId: teamId, AllianceStation: station, RobotLinked: true, DsLinked: true}
	// Keep the fake connection from timing out when packets are sent.
	dsConn.lastPacketTime = time.Now().Add(time.Hour)
	arena.AllianceStations[station].DsConn = dsConn
	return dsConn
}

func setupOpenFieldArena(t *testing.T) *Arena {
	arena := setupTestArena(t)
	assert.Nil(t, arena.SetOpenFieldMode(true))
	return arena
}

func TestOpenFieldModeToggle(t *testing.T) {
	arena := setupTestArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	setupOpenFieldStation(t, arena, 254, "R1")

	// Can't enter Open Field mode while a match is underway.
	arena.MatchState = AutoPeriod
	err := arena.SetOpenFieldMode(true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Cannot enter Open Field mode")
	}
	assert.False(t, arena.OpenFieldMode)
	arena.MatchState = PreMatch

	assert.Nil(t, arena.SetOpenFieldMode(true))
	assert.True(t, arena.OpenFieldMode)
	assert.Equal(t, "test", arena.CurrentMatch.Type)
	assert.Equal(t, OpenFieldMatchName, arena.CurrentMatch.DisplayName)
	assert.Equal(t, 254, arena.CurrentMatch.Red1)
	assert.NotNil(t, arena.AllianceStations["R1"].DsConn) // Team and DS connection are carried over.
	assert.Nil(t, arena.SetOpenFieldMode(true))           // Idempotent.

	// Enable a robot, then turn the mode off and check that everything is disabled and reset.
	assert.Nil(t, arena.OpenFieldSetAuto("R1", true))
	assert.Nil(t, arena.OpenFieldRunAuto("R1"))
	assert.True(t, arena.AllianceStations["R1"].DsConn.Enabled)
	assert.Nil(t, arena.SetOpenFieldMode(false))
	assert.False(t, arena.OpenFieldMode)
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, arena.AllianceStations["R1"].OpenFieldAuto)
	assert.Equal(t, 0.0, arena.AllianceStations["R1"].OpenFieldAutoRemainingSec)
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	assert.False(t, arena.AllianceStations["R1"].DsConn.Enabled)
	assert.Equal(t, "Test Match", arena.CurrentMatch.DisplayName)
	assert.Equal(t, 254, arena.CurrentMatch.Red1)

	// Open Field operations are refused when the mode is off.
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is not active")
	}
	assert.NotNil(t, arena.OpenFieldSetAuto("R1", true))
	assert.NotNil(t, arena.OpenFieldRunAuto("R1"))
}

func TestOpenFieldMatchStartRefused(t *testing.T) {
	arena := setupOpenFieldArena(t)
	for _, allianceStation := range arena.AllianceStations {
		allianceStation.Bypass = true
	}

	err := arena.StartMatch()
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is active")
	}
	assert.Equal(t, PreMatch, arena.MatchState)
	assert.NotNil(t, arena.checkCanStartMatch())

	err = arena.StartTimeout(60)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is active")
	}
	assert.Equal(t, PreMatch, arena.MatchState)

	err = arena.LoadMatch(&model.Match{Type: "qualification", DisplayName: "1"})
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is active")
	}
	assert.Equal(t, OpenFieldMatchName, arena.CurrentMatch.DisplayName)

	// Once the mode is turned off, the match can be started again.
	assert.Nil(t, arena.SetOpenFieldMode(false))
	assert.Nil(t, arena.StartMatch())
}

func TestOpenFieldPerStationPacketBits(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	arena.Database.CreateTeam(&model.Team{Id: 1114})
	arena.Database.CreateTeam(&model.Team{Id: 148})
	r1 := setupOpenFieldStation(t, arena, 254, "R1")
	b2 := setupOpenFieldStation(t, arena, 1114, "B2")
	b3 := setupOpenFieldStation(t, arena, 148, "B3")

	arena.Update()
	assert.False(t, r1.Enabled)
	assert.False(t, b2.Enabled)
	assert.False(t, b3.Enabled)

	// Enable R1 in teleop, B2 in auto, and leave B3 disabled.
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.Nil(t, arena.OpenFieldSetAuto("B2", true))
	assert.Nil(t, arena.OpenFieldSetEnabled("B2", true))

	// The packet is sent right away without waiting for the arena loop.
	assert.True(t, r1.Enabled)
	assert.False(t, r1.Auto)
	assert.True(t, b2.Enabled)
	assert.True(t, b2.Auto)
	assert.False(t, b3.Enabled)

	packet := r1.encodeControlPacket(arena)
	assert.Equal(t, byte(0x04), packet[3])
	packet = b2.encodeControlPacket(arena)
	assert.Equal(t, byte(0x06), packet[3])
	packet = b3.encodeControlPacket(arena)
	assert.Equal(t, byte(0x00), packet[3])

	// The values persist across regular periodic packets.
	arena.lastDsPacketTime = time.Unix(0, 0)
	arena.Update()
	assert.True(t, r1.Enabled)
	assert.True(t, b2.Enabled)
	assert.True(t, b2.Auto)
	assert.False(t, b3.Enabled)

	// Changing mode while enabled is refused.
	err := arena.OpenFieldSetAuto("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "while it is enabled")
	}
	assert.False(t, r1.Auto)

	// Disabling a single station only affects that station.
	assert.Nil(t, arena.OpenFieldSetEnabled("B2", false))
	assert.True(t, r1.Enabled)
	assert.False(t, b2.Enabled)

	// Disable all.
	assert.Nil(t, arena.OpenFieldSetEnabled("B3", true))
	assert.True(t, b3.Enabled)
	arena.OpenFieldDisableAll()
	assert.False(t, r1.Enabled)
	assert.False(t, b2.Enabled)
	assert.False(t, b3.Enabled)
	for _, allianceStation := range arena.AllianceStations {
		assert.False(t, allianceStation.OpenFieldEnabled)
	}

	// Invalid station.
	err = arena.OpenFieldSetEnabled("R4", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Invalid alliance station")
	}
}

func TestOpenFieldEnableRefusals(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})

	// No team assigned.
	err := arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "no team is assigned")
	}

	// Team assigned but no DS connected.
	assert.Nil(t, arena.assignTeam(254, "R1"))
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "driver station is connected")
	}
	assert.NotNil(t, arena.OpenFieldRunAuto("R1"))

	// DS connected but robot not linked.
	dsConn := setupOpenFieldStation(t, arena, 254, "R1")
	dsConn.RobotLinked = false
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "linked")
	}
	assert.NotNil(t, arena.CheckCanOpenFieldEnable("R1"))
	dsConn.RobotLinked = true
	assert.Nil(t, arena.CheckCanOpenFieldEnable("R1"))

	// Bypassed.
	arena.AllianceStations["R1"].Bypass = true
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "bypassed")
	}
	arena.AllianceStations["R1"].Bypass = false

	// E-stopped.
	arena.AllianceStations["R1"].Estop = true
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "emergency stop")
	}
	err = arena.OpenFieldRunAuto("R1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "emergency stop")
	}
	arena.AllianceStations["R1"].Estop = false
	arena.AllianceStations["R1"].Astop = true
	assert.NotNil(t, arena.OpenFieldSetEnabled("R1", true))
	arena.AllianceStations["R1"].Astop = false

	// Disabling is always allowed.
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", false))

	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.True(t, dsConn.Enabled)
}

func TestOpenFieldPlcSafety(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	dsConn := setupOpenFieldStation(t, arena, 254, "R1")
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.True(t, dsConn.Enabled)

	// An unconnected PLC reports as unhealthy with all e-stops active.
	arena.Plc.SetAddress("1.2.3.4")
	assert.True(t, arena.Plc.GetFieldEstop())
	arena.Update()
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, dsConn.Enabled)

	err := arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "PLC is not healthy")
	}
	arena.Plc.IsHealthy = true
	err = arena.OpenFieldSetEnabled("R1", true)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "field emergency stop")
	}

	// The station's physical stop button is latched and can't be cleared while it is still pressed.
	assert.True(t, arena.AllianceStations["R1"].Estop)
	err = arena.OpenFieldClearEstop("R1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "still pressed")
	}
	assert.True(t, arena.AllianceStations["R1"].Estop)

	// The DS packet never carries the enabled bit while the field e-stop is active, even if the state says enabled.
	arena.AllianceStations["R1"].OpenFieldEnabled = true
	arena.AllianceStations["R1"].Estop = false
	arena.sendDsPacket(false, false)
	assert.False(t, dsConn.Enabled)
}

func TestOpenFieldEstopLatchAndClear(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	dsConn := setupOpenFieldStation(t, arena, 254, "R1")
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))

	// Stop button pressed and then released: the e-stop stays latched and the robot stays disabled.
	arena.handleEstop("R1", true)
	arena.Update()
	assert.True(t, arena.AllianceStations["R1"].Estop)
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, dsConn.Enabled)
	assert.True(t, dsConn.Estop)
	arena.handleEstop("R1", false)
	arena.Update()
	assert.True(t, arena.AllianceStations["R1"].Estop)
	assert.False(t, dsConn.Enabled)

	// Clearing the e-stop leaves the robot disabled until it is explicitly re-enabled.
	assert.Nil(t, arena.OpenFieldClearEstop("R1"))
	assert.False(t, arena.AllianceStations["R1"].Estop)
	assert.False(t, dsConn.Estop)
	assert.False(t, dsConn.Enabled)
	arena.Update()
	assert.False(t, arena.AllianceStations["R1"].Estop)
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.True(t, dsConn.Enabled)

	// Software e-stop.
	assert.Nil(t, arena.OpenFieldEstop("R1"))
	assert.True(t, arena.AllianceStations["R1"].Estop)
	assert.False(t, dsConn.Enabled)
	assert.True(t, dsConn.Estop)
	arena.Update()
	assert.True(t, arena.AllianceStations["R1"].Estop)
	assert.NotNil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.Nil(t, arena.OpenFieldClearEstop("R1"))

	// Outside Open Field mode, the e-stop isn't latched before a match (existing behavior).
	assert.Nil(t, arena.SetOpenFieldMode(false))
	arena.handleEstop("R1", true)
	assert.True(t, arena.AllianceStations["R1"].Estop)
	arena.handleEstop("R1", false)
	assert.False(t, arena.AllianceStations["R1"].Estop)
	assert.NotNil(t, arena.OpenFieldClearEstop("R1"))
}

func TestOpenFieldDisconnectAndTeamChange(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	arena.Database.CreateTeam(&model.Team{Id: 1114})
	dsConn := setupOpenFieldStation(t, arena, 254, "R1")
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))

	// Robot link lost.
	dsConn.RobotLinked = false
	arena.Update()
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, dsConn.Enabled)
	dsConn.RobotLinked = true
	arena.lastDsPacketTime = time.Unix(0, 0)
	arena.Update()
	assert.False(t, dsConn.Enabled) // Doesn't re-enable by itself.

	// DS disconnected.
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	arena.AllianceStations["R1"].DsConn = nil
	arena.Update()
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	arena.AllianceStations["R1"].DsConn = dsConn
	arena.lastDsPacketTime = time.Unix(0, 0)
	arena.Update()
	assert.False(t, dsConn.Enabled)

	// DS disconnect helper used by the TCP handler.
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	arena.disableOpenFieldStation("R1")
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)

	// Team removed or replaced.
	assert.Nil(t, arena.OpenFieldSetAuto("R1", true))
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.Nil(t, arena.assignTeam(1114, "R1"))
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, arena.AllianceStations["R1"].OpenFieldAuto)
	dsConn = setupOpenFieldStation(t, arena, 1114, "R1")
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.Nil(t, arena.assignTeam(0, "R1"))
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
	assert.False(t, dsConn.Enabled)
}

func TestOpenFieldRunAuto(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	arena.Database.CreateTeam(&model.Team{Id: 1114})
	r1 := setupOpenFieldStation(t, arena, 254, "R1")
	r2 := setupOpenFieldStation(t, arena, 1114, "R2")

	assert.Nil(t, arena.OpenFieldRunAuto("R1"))
	assert.True(t, r1.Enabled)
	assert.True(t, r1.Auto)
	assert.True(t, arena.AllianceStations["R1"].OpenFieldAuto)
	assert.Equal(t, float64(game.MatchTiming.AutoDurationSec), arena.AllianceStations["R1"].OpenFieldAutoRemainingSec)
	packet := r1.encodeControlPacket(arena)
	assert.Equal(t, byte(0x06), packet[3])
	assert.Equal(t, byte(game.MatchTiming.AutoDurationSec), packet[21])
	assert.False(t, r2.Enabled)

	// Can't start auto again while running.
	assert.NotNil(t, arena.OpenFieldRunAuto("R1"))

	// Partway through, remaining time is reported.
	arena.AllianceStations["R1"].openFieldAutoEndTime = time.Now().Add(5500 * time.Millisecond)
	arena.Update()
	assert.True(t, r1.Enabled)
	assert.InDelta(t, 5.5, arena.AllianceStations["R1"].OpenFieldAutoRemainingSec, 0.1)
	packet = r1.encodeControlPacket(arena)
	assert.Equal(t, byte(6), packet[21])

	// Timer expiry disables the robot.
	arena.AllianceStations["R1"].openFieldAutoEndTime = time.Now().Add(-time.Millisecond)
	arena.Update()
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)
	assert.False(t, r1.Enabled)
	assert.Equal(t, 0.0, arena.AllianceStations["R1"].OpenFieldAutoRemainingSec)
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	assert.True(t, arena.AllianceStations["R1"].OpenFieldAuto)

	// Disabling cancels the timer.
	assert.Nil(t, arena.OpenFieldRunAuto("R1"))
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", false))
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	assert.False(t, r1.Enabled)

	// E-stop cancels the timer.
	assert.Nil(t, arena.OpenFieldRunAuto("R1"))
	assert.Nil(t, arena.OpenFieldEstop("R1"))
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	assert.False(t, r1.Enabled)
	assert.Nil(t, arena.OpenFieldClearEstop("R1"))

	// Disable all cancels the timer.
	assert.Nil(t, arena.OpenFieldRunAuto("R1"))
	arena.OpenFieldDisableAll()
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	assert.False(t, r1.Enabled)

	// Manually enabling in auto mode is untimed.
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", true))
	assert.True(t, r1.Auto)
	assert.True(t, arena.AllianceStations["R1"].openFieldAutoEndTime.IsZero())
	packet = r1.encodeControlPacket(arena)
	assert.Equal(t, byte(game.MatchTiming.AutoDurationSec), packet[21])
	assert.Nil(t, arena.OpenFieldSetEnabled("R1", false))

	// Teleop reports the teleop duration as the match time.
	assert.Nil(t, arena.OpenFieldSetAuto("R1", false))
	packet = r1.encodeControlPacket(arena)
	teleop := game.MatchTiming.TeleopDurationSec
	assert.Equal(t, byte(teleop>>8), packet[20])
	assert.Equal(t, byte(teleop&0xff), packet[21])
}

func TestOpenFieldArenaStatusIncludesStationState(t *testing.T) {
	arena := setupOpenFieldArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 254})
	setupOpenFieldStation(t, arena, 254, "R1")
	assert.Nil(t, arena.OpenFieldRunAuto("R1"))

	message := arena.generateArenaStatusMessage()
	json, err := jsonMarshal(message)
	assert.Nil(t, err)
	assert.Contains(t, json, `"OpenFieldMode":true`)
	assert.Contains(t, json, `"OpenFieldEnabled":true`)
	assert.Contains(t, json, `"OpenFieldAuto":true`)
	assert.Contains(t, json, `"OpenFieldAutoRemainingSec":`)
}

func jsonMarshal(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}
