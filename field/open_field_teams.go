// Team-to-station assignment for Open Field mode.

package field

import (
	"fmt"

	"github.com/Team254/cheesy-arena-lite/model"
)

// Alliance stations in the order expected by the network configuration functions.
var openFieldStationOrder = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

// Places the given team into the given alliance station while in Open Field mode, removing it from any other station.
func (arena *Arena) OpenFieldAssignTeam(teamId int, station string) error {
	if err := arena.checkOpenFieldStation(station); err != nil {
		return err
	}
	if teamId < 0 {
		return fmt.Errorf("Invalid team number %d.", teamId)
	}
	if teamId == 0 {
		return arena.OpenFieldClearStation(station)
	}

	team, err := arena.EnsureTeamWpaKey(teamId)
	if err != nil {
		return err
	}

	allianceStation := arena.AllianceStations[station]
	if allianceStation.Team != nil && allianceStation.Team.Id == teamId {
		// The team is already in the requested station; EnsureTeamWpaKey has fixed up any missing key.
		return nil
	}

	// Remove the team from any other station it is currently occupying.
	if otherStation := arena.getAssignedAllianceStation(teamId); otherStation != "" && otherStation != station {
		if err = arena.openFieldVacateStation(otherStation); err != nil {
			return err
		}
	}

	arena.openFieldDisableStation(station)
	if err = arena.assignTeam(teamId, station); err != nil {
		return err
	}
	// Use the record returned by EnsureTeamWpaKey so that the network is configured with the team's WPA key.
	allianceStation.Team = team
	arena.openFieldSetMatchTeam(station, teamId)

	arena.openFieldApplyAssignments()
	return nil
}

// Removes whatever team is in the given alliance station while in Open Field mode.
func (arena *Arena) OpenFieldClearStation(station string) error {
	if err := arena.checkOpenFieldStation(station); err != nil {
		return err
	}
	if err := arena.openFieldVacateStation(station); err != nil {
		return err
	}
	arena.openFieldApplyAssignments()
	return nil
}

// Returns an error if Open Field mode is off or the given station is invalid.
func (arena *Arena) checkOpenFieldStation(station string) error {
	if !arena.OpenFieldMode {
		return fmt.Errorf("Open Field mode is not active.")
	}
	if _, ok := arena.AllianceStations[station]; !ok {
		return fmt.Errorf("Invalid alliance station '%s'.", station)
	}
	return nil
}

// Disables the robot in the given station and removes its team, without reconfiguring the network.
func (arena *Arena) openFieldVacateStation(station string) error {
	arena.openFieldDisableStation(station)
	if err := arena.assignTeam(0, station); err != nil {
		return err
	}
	arena.openFieldSetMatchTeam(station, 0)
	return nil
}

// Makes sure the robot in the given station is disabled before its team assignment changes.
func (arena *Arena) openFieldDisableStation(station string) {
	allianceStation := arena.AllianceStations[station]
	if allianceStation.OpenFieldEnabled {
		// Prefer the full disable logic (which also cancels any running timed auto), but always clear the flag.
		_ = arena.OpenFieldSetEnabled(station, false)
		allianceStation.OpenFieldEnabled = false
	}
}

// Mirrors the station's team into the current match record so that displays show it.
func (arena *Arena) openFieldSetMatchTeam(station string, teamId int) {
	if arena.CurrentMatch == nil {
		return
	}
	switch station {
	case "R1":
		arena.CurrentMatch.Red1 = teamId
	case "R2":
		arena.CurrentMatch.Red2 = teamId
	case "R3":
		arena.CurrentMatch.Red3 = teamId
	case "B1":
		arena.CurrentMatch.Blue1 = teamId
	case "B2":
		arena.CurrentMatch.Blue2 = teamId
	case "B3":
		arena.CurrentMatch.Blue3 = teamId
	}
}

// Reconfigures the field network for the current station teams and notifies listeners of the change.
func (arena *Arena) openFieldApplyAssignments() {
	var teams [6]*model.Team
	for i, station := range openFieldStationOrder {
		teams[i] = arena.AllianceStations[station].Team
	}
	arena.setupNetwork(teams)
	arena.MatchLoadNotifier.Notify()
	arena.ArenaStatusNotifier.Notify()
}
