// Per-station page where a team picks its team/alliance station and enables/disables its robot.

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

type stationControlOption struct {
	Station string
	Name    string
	IsRed   bool
}

var stationControlOptions = []stationControlOption{
	{"R1", "Red 1", true}, {"R2", "Red 2", true}, {"R3", "Red 3", true},
	{"B1", "Blue 1", false}, {"B2", "Blue 2", false}, {"B3", "Blue 3", false},
}

// Returns the human-readable name of the given alliance station, or the empty string if it is invalid.
func stationControlName(station string) string {
	for _, option := range stationControlOptions {
		if option.Station == station {
			return option.Name
		}
	}
	return ""
}

// Shows the station control page for the station given in the query string, or a station picker if none is given.
func (web *Web) stationControlHandler(w http.ResponseWriter, r *http.Request) {
	station := r.URL.Query().Get("station")
	stationName := stationControlName(station)
	if stationName == "" {
		station = ""
	}
	teamId := 0
	if station != "" && web.arena.AllianceStations[station].Team != nil {
		teamId = web.arena.AllianceStations[station].Team.Id
	}

	template, err := web.parseFiles("templates/station_control.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		Station       string
		StationName   string
		IsRed         bool
		TeamId        int
		OpenFieldMode bool
		Stations      []stationControlOption
	}{web.arena.EventSettings, station, stationName, len(station) > 0 && station[0] == 'R', teamId,
		web.arena.OpenFieldMode, stationControlOptions}
	err = template.ExecuteTemplate(w, "base_no_navbar", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for a station control client to claim its station and enable/disable its robot.
func (web *Web) stationControlWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	station := r.URL.Query().Get("station")
	if stationControlName(station) == "" {
		handleWebErr(w, fmt.Errorf("Invalid alliance station '%s'.", station))
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
		case "claimStation":
			args := struct {
				TeamId int
			}{}
			if err = mapstructure.Decode(data, &args); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if args.TeamId <= 0 {
				ws.WriteError("Please enter a valid team number.")
				continue
			}
			if err = web.arena.OpenFieldAssignTeam(args.TeamId, station); err != nil {
				ws.WriteError(err.Error())
			}
			continue
		case "releaseStation":
			if err = web.arena.OpenFieldClearStation(station); err != nil {
				ws.WriteError(err.Error())
			}
			continue
		case "disable":
			err = web.arena.OpenFieldSetEnabled(station, false)
		case "enable":
			err = web.arena.OpenFieldSetEnabled(station, true)
		case "setAuto":
			auto, ok := data.(bool)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			err = web.arena.OpenFieldSetAuto(station, auto)
		case "runAuto":
			err = web.arena.OpenFieldRunAuto(station)
		case "estop":
			err = web.arena.OpenFieldEstop(station)
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
