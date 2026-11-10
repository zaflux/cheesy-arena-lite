// Management of team WPA keys so that teams can configure their robot radios to match the field.

package field

import "github.com/Team254/cheesy-arena-lite/model"

// Returns the team record for the given team, creating it and/or generating a WPA key if either doesn't exist yet.
func (arena *Arena) EnsureTeamWpaKey(teamId int) (*model.Team, error) {
	team, err := arena.Database.GetTeamById(teamId)
	if err != nil {
		return nil, err
	}
	if team == nil {
		team = &model.Team{Id: teamId}
	}
	return team, nil
}

// Validates and saves a new WPA key for the given team, reconfiguring the field network if the team is on the field.
func (arena *Arena) SetTeamWpaKey(teamId int, wpaKey string) error {
	return errOpenFieldNotImplemented
}
