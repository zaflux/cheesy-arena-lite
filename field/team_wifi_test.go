package field

import (
	"testing"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/Team254/cheesy-arena-lite/network"
	"github.com/stretchr/testify/assert"
)

type fakeAccessPoint struct {
	configuredTeams [][6]*model.Team
}

func (ap *fakeAccessPoint) SetSettings(string, string, string, int, int, string, bool) {}
func (ap *fakeAccessPoint) Run()                                                       {}
func (ap *fakeAccessPoint) ConfigureAdminWifi() error                                  { return nil }
func (ap *fakeAccessPoint) GetTeamWifiStatuses() [6]network.TeamWifiStatus {
	return [6]network.TeamWifiStatus{}
}
func (ap *fakeAccessPoint) ConfigureTeamWifi(teams [6]*model.Team) error {
	ap.configuredTeams = append(ap.configuredTeams, teams)
	return nil
}

type fakeSwitch struct{}

func (sw *fakeSwitch) ConfigureTeamEthernet([6]*model.Team) error { return nil }

func setupTeamWifiTestArena(t *testing.T) (*Arena, *fakeAccessPoint) {
	arena := setupTestArena(t)
	ap := &fakeAccessPoint{}
	arena.accessPoint = ap
	arena.networkSwitch = &fakeSwitch{}
	arena.EventSettings.NetworkSecurityEnabled = true
	return arena, ap
}

func TestGenerateAndValidateWpaKey(t *testing.T) {
	key := GenerateTeamWpaKey()
	assert.Equal(t, TeamWpaKeyLength, len(key))
	assert.Nil(t, ValidateWpaKey(key))
	assert.NotEqual(t, key, GenerateTeamWpaKey())

	assert.Nil(t, ValidateWpaKey("12345678"))
	assert.Nil(t, ValidateWpaKey("a b-c_d.e!f@g#h%i^j&k*l(m)n+o=p?"))
	assert.Nil(t, ValidateWpaKey("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk"))
	assert.NotNil(t, ValidateWpaKey(""))
	assert.NotNil(t, ValidateWpaKey("1234567"))
	assert.NotNil(t, ValidateWpaKey("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl"))
	for _, key := range []string{"abc'defgh", "abc\"defgh", "abc\\defgh", "abc`defgh", "abc$defgh", "abc\ndefgh",
		"abcdéfghi"} {
		assert.NotNil(t, ValidateWpaKey(key), key)
	}
}

func TestEnsureTeamWpaKey(t *testing.T) {
	arena, ap := setupTeamWifiTestArena(t)

	// Creates the team if it doesn't exist.
	team, err := arena.EnsureTeamWpaKey(254)
	assert.Nil(t, err)
	assert.Equal(t, 254, team.Id)
	assert.Equal(t, TeamWpaKeyLength, len(team.WpaKey))
	dbTeam, _ := arena.Database.GetTeamById(254)
	if assert.NotNil(t, dbTeam) {
		assert.Equal(t, team.WpaKey, dbTeam.WpaKey)
	}

	// Returns the existing key without changing it.
	team2, err := arena.EnsureTeamWpaKey(254)
	assert.Nil(t, err)
	assert.Equal(t, team.WpaKey, team2.WpaKey)

	// Generates a key for an existing team that doesn't have one.
	assert.Nil(t, arena.Database.CreateTeam(&model.Team{Id: 1114, Name: "Simbotics"}))
	team, err = arena.EnsureTeamWpaKey(1114)
	assert.Nil(t, err)
	assert.Equal(t, "Simbotics", team.Name)
	assert.Equal(t, TeamWpaKeyLength, len(team.WpaKey))
	assert.Empty(t, ap.configuredTeams)

	// Fixes up and reconfigures the network for an on-field team that had no key.
	assert.Nil(t, arena.assignTeam(148, "B2"))
	assert.Equal(t, "", arena.AllianceStations["B2"].Team.WpaKey)
	team, err = arena.EnsureTeamWpaKey(148)
	assert.Nil(t, err)
	assert.Equal(t, team.WpaKey, arena.AllianceStations["B2"].Team.WpaKey)
	if assert.Equal(t, 1, len(ap.configuredTeams)) {
		assert.Equal(t, team.WpaKey, ap.configuredTeams[0][4].WpaKey)
	}

	// Rejects invalid team numbers.
	_, err = arena.EnsureTeamWpaKey(0)
	assert.NotNil(t, err)
	_, err = arena.EnsureTeamWpaKey(100000)
	assert.NotNil(t, err)
}

func TestSetTeamWpaKey(t *testing.T) {
	arena, ap := setupTeamWifiTestArena(t)

	// Team not on the field: saved without reconfiguring the network.
	assert.Nil(t, arena.SetTeamWpaKey(254, "cheesypoofs"))
	team, _ := arena.Database.GetTeamById(254)
	assert.Equal(t, "cheesypoofs", team.WpaKey)
	assert.Empty(t, ap.configuredTeams)

	// Invalid keys are rejected and not saved.
	assert.NotNil(t, arena.SetTeamWpaKey(254, "short"))
	assert.NotNil(t, arena.SetTeamWpaKey(254, "it's-a-key"))
	team, _ = arena.Database.GetTeamById(254)
	assert.Equal(t, "cheesypoofs", team.WpaKey)

	// Team on the field: the station copy is updated and the network is reconfigured with all station teams.
	assert.Nil(t, arena.assignTeam(254, "R2"))
	assert.Nil(t, arena.SetTeamWpaKey(1114, "simbotics"))
	assert.Nil(t, arena.assignTeam(1114, "B3"))
	assert.Empty(t, ap.configuredTeams)
	assert.Nil(t, arena.SetTeamWpaKey(254, "newpoofskey"))
	assert.Equal(t, "newpoofskey", arena.AllianceStations["R2"].Team.WpaKey)
	if assert.Equal(t, 1, len(ap.configuredTeams)) {
		assert.Nil(t, ap.configuredTeams[0][0])
		assert.Equal(t, 254, ap.configuredTeams[0][1].Id)
		assert.Equal(t, "newpoofskey", ap.configuredTeams[0][1].WpaKey)
		assert.Equal(t, 1114, ap.configuredTeams[0][5].Id)
	}

	// Setting the same key again is a no-op.
	assert.Nil(t, arena.SetTeamWpaKey(254, "newpoofskey"))
	assert.Equal(t, 1, len(ap.configuredTeams))

	// Refused while the team's own robot is enabled.
	arena.AllianceStations["R2"].OpenFieldEnabled = true
	err := arena.SetTeamWpaKey(254, "anotherkey")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "while its robot is enabled")
	}
	arena.AllianceStations["R2"].OpenFieldEnabled = false

	// Refused for an on-field team while another robot is enabled, since the whole AP restarts.
	arena.AllianceStations["B3"].OpenFieldEnabled = true
	err = arena.SetTeamWpaKey(254, "anotherkey")
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "other robots are enabled")
	}
	// Teams that aren't on the field can still change their keys.
	assert.Nil(t, arena.SetTeamWpaKey(148, "robowranglers"))
	arena.AllianceStations["B3"].OpenFieldEnabled = false

	// Refused for an on-field team during a match.
	arena.MatchState = AutoPeriod
	assert.NotNil(t, arena.SetTeamWpaKey(254, "anotherkey"))
	arena.MatchState = PostMatch
	assert.Nil(t, arena.SetTeamWpaKey(254, "anotherkey"))
	team, _ = arena.Database.GetTeamById(254)
	assert.Equal(t, "anotherkey", team.WpaKey)
	assert.Equal(t, 2, len(ap.configuredTeams))
}
