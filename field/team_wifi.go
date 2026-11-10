// Management of team WPA keys so that teams can configure their robot radios to match the field.

package field

import (
	"fmt"
	"log"
	"strings"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/dchest/uniuri"
)

const (
	TeamWpaKeyLength      = 8
	MinWpaKeyLength       = 8
	MaxWpaKeyLength       = 63
	MaxTeamNumber         = 99999
	disallowedWpaKeyChars = "'\"\\`$"
)

var allianceStationOrder = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

// Returns a new random WPA key in the same format as the bulk generator on the team setup page.
func GenerateTeamWpaKey() string {
	return uniuri.NewLen(TeamWpaKeyLength)
}

// Returns an error if the given WPA key can't be used for a team network. Beyond the WPA2 requirement of 8-63
// printable ASCII characters, quotes, backslashes, backticks and dollar signs are rejected because the OpenWRT access
// point is configured via a shell heredoc of single-quoted values.
func ValidateWpaKey(wpaKey string) error {
	if len(wpaKey) < MinWpaKeyLength || len(wpaKey) > MaxWpaKeyLength {
		return fmt.Errorf("WPA key must be between %d and %d characters.", MinWpaKeyLength, MaxWpaKeyLength)
	}
	for _, char := range wpaKey {
		if char < 0x20 || char > 0x7e {
			return fmt.Errorf("WPA key may only contain printable ASCII characters.")
		}
		if strings.ContainsRune(disallowedWpaKeyChars, char) {
			return fmt.Errorf("WPA key may not contain any of the following characters: %s", disallowedWpaKeyChars)
		}
	}
	return nil
}

// Returns the team record for the given team, creating it and/or generating a WPA key if either doesn't exist yet.
func (arena *Arena) EnsureTeamWpaKey(teamId int) (*model.Team, error) {
	if teamId <= 0 || teamId > MaxTeamNumber {
		return nil, fmt.Errorf("Invalid team number %d.", teamId)
	}
	team, err := arena.Database.GetTeamById(teamId)
	if err != nil {
		return nil, err
	}
	if team == nil {
		team = &model.Team{Id: teamId, WpaKey: GenerateTeamWpaKey()}
		if err = arena.Database.CreateTeam(team); err != nil {
			return nil, err
		}
	} else if team.WpaKey == "" {
		team.WpaKey = GenerateTeamWpaKey()
		if err = arena.Database.UpdateTeam(team); err != nil {
			return nil, err
		}
	} else {
		return team, nil
	}
	log.Printf("Generated WPA key for team %d.", teamId)

	// If the team is already on the field without a key, the network configuration will have failed; retry it now.
	if arena.updateStationTeamWpaKey(teamId, team.WpaKey) {
		if err = arena.checkCanChangeFieldWpaKey(teamId); err != nil {
			log.Printf("Not reconfiguring network for new team %d WPA key: %v", teamId, err)
		} else {
			arena.setupNetworkForCurrentStations()
		}
	}
	return team, nil
}

// Validates and saves a new WPA key for the given team, reconfiguring the field network if the team is on the field.
func (arena *Arena) SetTeamWpaKey(teamId int, wpaKey string) error {
	if err := ValidateWpaKey(wpaKey); err != nil {
		return err
	}
	team, err := arena.EnsureTeamWpaKey(teamId)
	if err != nil {
		return err
	}
	if team.WpaKey == wpaKey {
		return nil
	}

	if arena.teamStation(teamId) != "" {
		if err = arena.checkCanChangeFieldWpaKey(teamId); err != nil {
			return err
		}
	}

	team.WpaKey = wpaKey
	if err = arena.Database.UpdateTeam(team); err != nil {
		return err
	}
	log.Printf("Updated WPA key for team %d.", teamId)

	if arena.updateStationTeamWpaKey(teamId, wpaKey) {
		arena.setupNetworkForCurrentStations()
	}
	return nil
}

// Returns an error if reconfiguring the field network for an on-field team's key change would disrupt driving robots.
// Reconfiguring the access point briefly drops every team network on it, not just the one being changed.
func (arena *Arena) checkCanChangeFieldWpaKey(teamId int) error {
	switch arena.MatchState {
	case PreMatch, PostMatch, TimeoutActive, PostTimeout:
	default:
		return fmt.Errorf("Cannot change the WPA key for team %d while it is on the field during a match.", teamId)
	}
	for _, stationName := range allianceStationOrder {
		station := arena.AllianceStations[stationName]
		if station.OpenFieldEnabled {
			if station.Team != nil && station.Team.Id == teamId {
				return fmt.Errorf("Cannot change the WPA key for team %d while its robot is enabled.", teamId)
			}
			return fmt.Errorf(
				"Cannot change the WPA key for team %d while it is on the field and other robots are enabled; "+
					"the field network restarts when keys change.",
				teamId,
			)
		}
	}
	return nil
}

// Returns the alliance station that the given team is currently in, or the empty string if it isn't on the field.
func (arena *Arena) teamStation(teamId int) string {
	for _, stationName := range allianceStationOrder {
		if team := arena.AllianceStations[stationName].Team; team != nil && team.Id == teamId {
			return stationName
		}
	}
	return ""
}

// Updates the in-memory copy of the team in any alliance station it occupies. Returns true if it is on the field.
func (arena *Arena) updateStationTeamWpaKey(teamId int, wpaKey string) bool {
	onField := false
	for _, stationName := range allianceStationOrder {
		if team := arena.AllianceStations[stationName].Team; team != nil && team.Id == teamId {
			team.WpaKey = wpaKey
			onField = true
		}
	}
	return onField
}

func (arena *Arena) setupNetworkForCurrentStations() {
	var teams [6]*model.Team
	for i, stationName := range allianceStationOrder {
		teams[i] = arena.AllianceStations[stationName].Team
	}
	arena.setupNetwork(teams)
}
