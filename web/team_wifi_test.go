package web

import (
	"testing"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
)

func TestTeamWifiLookup(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/team_wifi")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Look Up Team Wi-Fi Password")
	assert.NotContains(t, recorder.Body.String(), "Change WPA Key")

	// Looking up an unknown team creates it with a random key.
	recorder = web.getHttpResponse("/team_wifi?team=254")
	assert.Equal(t, 200, recorder.Code)
	team, _ := web.arena.Database.GetTeamById(254)
	if assert.NotNil(t, team) {
		assert.Equal(t, 8, len(team.WpaKey))
		assert.Contains(t, recorder.Body.String(), team.WpaKey)
	}
	assert.Contains(t, recorder.Body.String(), "Team 254 Robot Radio Settings")
	assert.Contains(t, recorder.Body.String(), "FRC Radio Configuration Utility")

	// Existing keys are shown unchanged, with VH-109 instructions for a VH-113 field.
	web.arena.Database.CreateTeam(&model.Team{Id: 1114, WpaKey: "simbotics"})
	web.arena.EventSettings.ApType = "vh113"
	recorder = web.getHttpResponse("/team_wifi?team=1114")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "simbotics")
	assert.Contains(t, recorder.Body.String(), "VH-109")
	assert.Contains(t, recorder.Body.String(), "10.11.14.1")

	recorder = web.getHttpResponse("/team_wifi?team=abc")
	assert.Equal(t, 400, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Invalid team number")
	recorder = web.getHttpResponse("/team_wifi?team=-5")
	assert.Equal(t, 400, recorder.Code)
}

func TestTeamWifiSetKey(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "aaaaaaaa"})

	recorder := web.postHttpResponse("/team_wifi", "team=254&action=set&wpaKey=cheesypoofs")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, "/team_wifi?team=254&saved=true", recorder.Header().Get("Location"))
	team, _ := web.arena.Database.GetTeamById(254)
	assert.Equal(t, "cheesypoofs", team.WpaKey)
	recorder = web.getHttpResponse("/team_wifi?team=254&saved=true")
	assert.Contains(t, recorder.Body.String(), "WPA key saved")

	// Invalid keys are rejected.
	recorder = web.postHttpResponse("/team_wifi", "team=254&action=set&wpaKey=short")
	assert.Equal(t, 400, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "between 8 and 63 characters")
	recorder = web.postHttpResponse("/team_wifi", "team=254&action=set&wpaKey=it%27s-a-key")
	assert.Equal(t, 400, recorder.Code)
	team, _ = web.arena.Database.GetTeamById(254)
	assert.Equal(t, "cheesypoofs", team.WpaKey)

	// Generates a random key.
	recorder = web.postHttpResponse("/team_wifi", "team=254&action=generate&wpaKey=")
	assert.Equal(t, 303, recorder.Code)
	team, _ = web.arena.Database.GetTeamById(254)
	assert.NotEqual(t, "cheesypoofs", team.WpaKey)
	assert.Equal(t, 8, len(team.WpaKey))

	// Refused while the team's robot is enabled.
	web.arena.AllianceStations["R1"].Team = team
	web.arena.AllianceStations["R1"].OpenFieldEnabled = true
	recorder = web.postHttpResponse("/team_wifi", "team=254&action=set&wpaKey=enabledkey")
	assert.Equal(t, 400, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "while its robot is enabled")
	assert.Contains(t, recorder.Body.String(), "Station R1")

	recorder = web.postHttpResponse("/team_wifi", "team=abc&wpaKey=whatever1")
	assert.Equal(t, 400, recorder.Code)
}

func TestTeamWifiSelfServiceDisabled(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.AdminPassword = "admin"
	web.arena.EventSettings.TeamWifiSelfServiceDisabled = true
	web.arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "aaaaaaaa"})

	recorder := web.getHttpResponse("/team_wifi?team=254")
	assert.Equal(t, 307, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "aaaaaaaa")
	recorder = web.postHttpResponse("/team_wifi", "team=254&action=set&wpaKey=cheesypoofs")
	assert.Equal(t, 307, recorder.Code)
	team, _ := web.arena.Database.GetTeamById(254)
	assert.Equal(t, "aaaaaaaa", team.WpaKey)

	// Admins can still use the page.
	recorder = web.postHttpResponse("/login", "username=admin&password=admin")
	cookie := recorder.Header().Get("Set-Cookie")
	recorder = web.getHttpResponseWithHeaders("/team_wifi?team=254", map[string]string{"Cookie": cookie})
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "aaaaaaaa")

	// Self-service is enabled by default.
	web = setupTestWeb(t)
	assert.False(t, web.arena.EventSettings.TeamWifiSelfServiceDisabled)
}

func TestSetupSettingsTeamWifiSelfService(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), "name=\"teamWifiSelfServiceEnabled\" checked")

	recorder = web.postHttpResponse("/setup/settings", "name=Chezy Champs&elimType=single&numElimAlliances=8")
	assert.Equal(t, 303, recorder.Code)
	assert.True(t, web.arena.EventSettings.TeamWifiSelfServiceDisabled)

	recorder = web.postHttpResponse("/setup/settings",
		"name=Chezy Champs&elimType=single&numElimAlliances=8&teamWifiSelfServiceEnabled=on")
	assert.Equal(t, 303, recorder.Code)
	assert.False(t, web.arena.EventSettings.TeamWifiSelfServiceDisabled)
}
