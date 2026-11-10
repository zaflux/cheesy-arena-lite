package web

import (
	"testing"

	"github.com/Team254/cheesy-arena-lite/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestOpenField(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/open_field")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "Open Field - Untitled Event - Cheesy Arena")
	assert.Contains(t, body, "DISABLE ALL ROBOTS")
	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.Contains(t, body, "id=\"station"+station+"\"")
	}

	web.arena.OpenFieldMode = true
	assert.Nil(t, web.arena.OpenFieldAssignTeam(1503, "R2"))
	recorder = web.getHttpResponse("/open_field")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "value=\"1503\"")
}

func TestOpenFieldRequiresAdmin(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.AdminPassword = "admin"

	recorder := web.getHttpResponse("/open_field")
	assert.Equal(t, 307, recorder.Code)
	recorder = web.getHttpResponse("/open_field/websocket")
	assert.Equal(t, 307, recorder.Code)
}

func TestOpenFieldWebsocket(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/open_field/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Should get a few status updates right after connection.
	messages := readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")

	ws.Write("assignTeam", map[string]interface{}{"teamId": 254, "station": "B1"})
	assert.Contains(t, readWebsocketError(t, ws), "Open Field mode is not active")

	web.arena.OpenFieldMode = true
	ws.Write("assignTeam", map[string]interface{}{"teamId": 254, "station": "B1"})
	messages = readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")
	if assert.NotNil(t, web.arena.AllianceStations["B1"].Team) {
		assert.Equal(t, 254, web.arena.AllianceStations["B1"].Team.Id)
	}

	ws.Write("assignTeam", map[string]interface{}{"teamId": 254, "station": "Z9"})
	assert.Contains(t, readWebsocketError(t, ws), "Invalid alliance station")

	ws.Write("clearStation", "B1")
	messages = readWebsocketMultiple(t, ws, 2)
	assert.Contains(t, messages, "arenaStatus")
	assert.Contains(t, messages, "matchLoad")
	assert.Nil(t, web.arena.AllianceStations["B1"].Team)

	// Malformed and unknown messages.
	ws.Write("setOpenFieldMode", "on")
	assert.Contains(t, readWebsocketError(t, ws), "Failed to parse 'setOpenFieldMode' message")
	ws.Write("clearStation", 5)
	assert.Contains(t, readWebsocketError(t, ws), "Failed to parse 'clearStation' message")
	ws.Write("disableStation", nil)
	assert.Contains(t, readWebsocketError(t, ws), "Failed to parse 'disableStation' message")
	ws.Write("blorpy", nil)
	assert.Contains(t, readWebsocketError(t, ws), "Invalid message type 'blorpy'")
}
