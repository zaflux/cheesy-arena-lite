package field

import (
	"testing"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
)

func TestOpenFieldAssignTeamRequiresOpenFieldMode(t *testing.T) {
	arena := setupTestArena(t)

	err := arena.OpenFieldAssignTeam(254, "R1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is not active")
	}
	assert.Nil(t, arena.AllianceStations["R1"].Team)

	err = arena.OpenFieldClearStation("R1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Open Field mode is not active")
	}
}

func TestOpenFieldAssignTeamValidation(t *testing.T) {
	arena := setupTestArena(t)
	arena.OpenFieldMode = true

	err := arena.OpenFieldAssignTeam(254, "R4")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Invalid alliance station")
	}
	err = arena.OpenFieldClearStation("N1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Invalid alliance station")
	}
	err = arena.OpenFieldAssignTeam(-5, "R1")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Invalid team number")
	}
}

func TestOpenFieldAssignAndClearTeam(t *testing.T) {
	arena := setupTestArena(t)
	arena.OpenFieldMode = true
	assert.Nil(t, arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "aaaaaaaa"}))

	assert.Nil(t, arena.OpenFieldAssignTeam(254, "R2"))
	if assert.NotNil(t, arena.AllianceStations["R2"].Team) {
		assert.Equal(t, 254, arena.AllianceStations["R2"].Team.Id)
		assert.Equal(t, "aaaaaaaa", arena.AllianceStations["R2"].Team.WpaKey)
	}
	assert.Equal(t, 254, arena.CurrentMatch.Red2)
	assert.Equal(t, "R2", arena.getAssignedAllianceStation(254))

	// A team that doesn't exist in the database can still be assigned.
	assert.Nil(t, arena.OpenFieldAssignTeam(1114, "B3"))
	if assert.NotNil(t, arena.AllianceStations["B3"].Team) {
		assert.Equal(t, 1114, arena.AllianceStations["B3"].Team.Id)
	}
	assert.Equal(t, 1114, arena.CurrentMatch.Blue3)

	// Assigning the same team to the same station is a no-op.
	assert.Nil(t, arena.OpenFieldAssignTeam(254, "R2"))
	assert.Equal(t, 254, arena.AllianceStations["R2"].Team.Id)

	// Moving a team to another station clears its previous station.
	assert.Nil(t, arena.OpenFieldAssignTeam(254, "B1"))
	assert.Nil(t, arena.AllianceStations["R2"].Team)
	assert.Equal(t, 0, arena.CurrentMatch.Red2)
	assert.Equal(t, 254, arena.AllianceStations["B1"].Team.Id)
	assert.Equal(t, 254, arena.CurrentMatch.Blue1)

	// Replacing the team in an occupied station.
	assert.Nil(t, arena.OpenFieldAssignTeam(1678, "B1"))
	assert.Equal(t, 1678, arena.AllianceStations["B1"].Team.Id)
	assert.Equal(t, 1678, arena.CurrentMatch.Blue1)
	assert.Equal(t, "", arena.getAssignedAllianceStation(254))

	// Clearing stations.
	assert.Nil(t, arena.OpenFieldClearStation("B1"))
	assert.Nil(t, arena.AllianceStations["B1"].Team)
	assert.Equal(t, 0, arena.CurrentMatch.Blue1)
	assert.Nil(t, arena.OpenFieldAssignTeam(0, "B3"))
	assert.Nil(t, arena.AllianceStations["B3"].Team)
	assert.Equal(t, 0, arena.CurrentMatch.Blue3)

	// Clearing an empty station is fine.
	assert.Nil(t, arena.OpenFieldClearStation("B3"))
}

func TestOpenFieldAssignTeamDisablesRobot(t *testing.T) {
	arena := setupTestArena(t)
	arena.OpenFieldMode = true

	assert.Nil(t, arena.OpenFieldAssignTeam(254, "R1"))
	arena.AllianceStations["R1"].OpenFieldEnabled = true
	assert.Nil(t, arena.OpenFieldAssignTeam(1114, "R1"))
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)

	// Moving an enabled team out of its station disables it.
	arena.AllianceStations["R1"].OpenFieldEnabled = true
	assert.Nil(t, arena.OpenFieldAssignTeam(1114, "B2"))
	assert.False(t, arena.AllianceStations["R1"].OpenFieldEnabled)

	arena.AllianceStations["B2"].OpenFieldEnabled = true
	assert.Nil(t, arena.OpenFieldClearStation("B2"))
	assert.False(t, arena.AllianceStations["B2"].OpenFieldEnabled)
}
