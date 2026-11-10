package web

import (
	"testing"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/Team254/cheesy-arena-lite/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestStationControl(t *testing.T) {
	web := setupTestWeb(t)

	// Without a station, the page should offer a choice of stations.
	recorder := web.getHttpResponse("/station_control")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "Select the alliance station")
	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.Contains(t, body, "/station_control?station="+station)
	}

	// An invalid station also falls back to the station picker.
	recorder = web.getHttpResponse("/station_control?station=N1")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Select the alliance station")

	recorder = web.getHttpResponse("/station_control?station=B2")
	assert.Equal(t, 200, recorder.Code)
	body = recorder.Body.String()
	assert.Contains(t, body, "Blue 2 &ndash; Station Control")
	assert.Contains(t, body, "Open Field mode is off")
	assert.Contains(t, body, "DISABLE")
	assert.Contains(t, body, "var station = \"B2\";")

	// The page should show the team in the station and hide the Open Field warning when the mode is on.
	web.arena.OpenFieldMode = true
	assert.Nil(t, web.arena.OpenFieldAssignTeam(254, "B2"))
	recorder = web.getHttpResponse("/station_control?station=B2")
	assert.Equal(t, 200, recorder.Code)
	body = recorder.Body.String()
	assert.Contains(t, body, "/team_wifi?team=254")
	assert.Contains(t, body, "id=\"openFieldOff\" class=\"alert alert-warning\" style=\"display: none;\"")
}

func TestStationControlIsPublic(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.AdminPassword = "admin"

	recorder := web.getHttpResponse("/station_control?station=R1")
	assert.Equal(t, 200, recorder.Code)
}

func TestStationControlWebsocket(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "aaaaaaaa"})

	server, wsUrl := web.startTestServer()
	defer server.Close()

	// An invalid station should be rejected.
	_, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/station_control/websocket?station=X1", nil)
	assert.NotNil(t, err)

	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/station_control/websocket?station=R3", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Should get a few status updates right after connection.
	messages := readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")

	// Claiming a station requires Open Field mode.
	ws.Write("claimStation", map[string]interface{}{"teamId": 254})
	assert.Contains(t, readWebsocketError(t, ws), "Open Field mode is not active")
	assert.Nil(t, web.arena.AllianceStations["R3"].Team)

	web.arena.OpenFieldMode = true
	ws.Write("claimStation", map[string]interface{}{"teamId": 0})
	assert.Contains(t, readWebsocketError(t, ws), "valid team number")

	ws.Write("claimStation", map[string]interface{}{"teamId": 254})
	messages = readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")
	if assert.NotNil(t, web.arena.AllianceStations["R3"].Team) {
		assert.Equal(t, 254, web.arena.AllianceStations["R3"].Team.Id)
	}
	assert.Equal(t, 254, web.arena.CurrentMatch.Red3)

	ws.Write("releaseStation", nil)
	messages = readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")
	assert.Nil(t, web.arena.AllianceStations["R3"].Team)
	assert.Equal(t, 0, web.arena.CurrentMatch.Red3)

	// Malformed and unknown messages.
	ws.Write("setAuto", "yes")
	assert.Contains(t, readWebsocketError(t, ws), "Failed to parse 'setAuto' message")
	ws.Write("blorpy", nil)
	assert.Contains(t, readWebsocketError(t, ws), "Invalid message type 'blorpy'")
}
