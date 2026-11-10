// Admin page for turning Open Field mode on/off and monitoring/disabling all stations.

package web

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/Team254/cheesy-arena-lite/websocket"
	"github.com/mitchellh/mapstructure"
)

// Shows the Open Field mode control page for the FTA.
func (web *Web) openFieldHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/open_field.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teamIds := make(map[string]int)
	for station, allianceStation := range web.arena.AllianceStations {
		if allianceStation.Team != nil {
			teamIds[station] = allianceStation.Team.Id
		}
	}
	data := struct {
		*model.EventSettings
		OpenFieldMode bool
		Stations      []stationControlOption
		TeamIds       map[string]int
	}{web.arena.EventSettings, web.arena.OpenFieldMode, stationControlOptions, teamIds}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the Open Field mode control page.
func (web *Web) openFieldWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()

	// Subscribe the websocket to the notifiers whose messages will be passed on to the client, in a separate goroutine.
	go ws.HandleNotifiers(web.arena.ArenaStatusNotifier, web.arena.MatchLoadNotifier)

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}

		switch messageType {
		case "setOpenFieldMode":
			enabled, ok := data.(bool)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			err = web.arena.SetOpenFieldMode(enabled)
		case "assignTeam":
			args := struct {
				TeamId  int
				Station string
			}{}
			if err = mapstructure.Decode(data, &args); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = web.arena.OpenFieldAssignTeam(args.TeamId, args.Station); err != nil {
				ws.WriteError(err.Error())
			}
			continue
		case "clearStation":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			if err = web.arena.OpenFieldClearStation(station); err != nil {
				ws.WriteError(err.Error())
			}
			continue
		case "disableStation":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			err = web.arena.OpenFieldSetEnabled(station, false)
		case "disableAll":
			web.arena.OpenFieldDisableAll()
		default:
			ws.WriteError(fmt.Sprintf("Invalid message type '%s'.", messageType))
			continue
		}
		if err != nil {
			ws.WriteError(err.Error())
			continue
		}
		web.arena.ArenaStatusNotifier.Notify()
	}
}
