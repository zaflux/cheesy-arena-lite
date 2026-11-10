// Page where a team can look up or set the WPA key for its robot radio.

package web

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Team254/cheesy-arena-lite/field"
	"github.com/Team254/cheesy-arena-lite/model"
)

// Shows the team number lookup form, or the SSID and WPA key for the requested team.
func (web *Web) teamWifiGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.canUseTeamWifi(w, r) {
		return
	}

	teamParam := strings.TrimSpace(r.URL.Query().Get("team"))
	if teamParam == "" {
		web.renderTeamWifi(w, r, nil, "", "")
		return
	}
	teamId, err := strconv.Atoi(teamParam)
	if err != nil {
		web.renderTeamWifi(w, r, nil, fmt.Sprintf("Invalid team number '%s'.", teamParam), "")
		return
	}
	team, err := web.arena.EnsureTeamWpaKey(teamId)
	if err != nil {
		web.renderTeamWifi(w, r, nil, err.Error(), "")
		return
	}
	successMessage := ""
	if r.URL.Query().Get("saved") == "true" {
		successMessage = "WPA key saved. Re-program your robot radio with the new key."
	}
	web.renderTeamWifi(w, r, team, "", successMessage)
}

// Sets a custom or newly generated random WPA key for the given team.
func (web *Web) teamWifiPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.canUseTeamWifi(w, r) {
		return
	}

	teamId, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("team")))
	if err != nil {
		web.renderTeamWifi(w, r, nil, "Invalid team number.", "")
		return
	}
	wpaKey := r.PostFormValue("wpaKey")
	if r.PostFormValue("action") == "generate" {
		wpaKey = field.GenerateTeamWpaKey()
	}

	if err = web.arena.SetTeamWpaKey(teamId, wpaKey); err != nil {
		team, ensureErr := web.arena.EnsureTeamWpaKey(teamId)
		if ensureErr != nil {
			team = nil
		}
		web.renderTeamWifi(w, r, team, err.Error(), "")
		return
	}
	log.Printf("WPA key for team %d changed via team Wi-Fi page from %s.", teamId, r.RemoteAddr)
	http.Redirect(w, r, fmt.Sprintf("/team_wifi?team=%d&saved=true", teamId), 303)
}

// Returns true if the current user may view and change team WPA keys. When team self-service is disabled in the
// event settings, this requires an admin login (redirecting to the login page otherwise).
func (web *Web) canUseTeamWifi(w http.ResponseWriter, r *http.Request) bool {
	if !web.arena.EventSettings.TeamWifiSelfServiceDisabled {
		return true
	}
	return web.userIsAdmin(w, r)
}

func (web *Web) renderTeamWifi(
	w http.ResponseWriter, r *http.Request, team *model.Team, errorMessage, successMessage string,
) {
	if errorMessage != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	template, err := web.parseFiles("templates/team_wifi.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	station := ""
	radioAddress := ""
	if team != nil {
		radioAddress = fmt.Sprintf("10.%d.%d.1", team.Id/100, team.Id%100)
		for _, stationName := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
			if stationTeam := web.arena.AllianceStations[stationName].Team; stationTeam != nil &&
				stationTeam.Id == team.Id {
				station = stationName
			}
		}
	}
	data := struct {
		*model.EventSettings
		Team            *model.Team
		Station         string
		ErrorMessage    string
		SuccessMessage  string
		MinWpaKeyLength int
		MaxWpaKeyLength int
		RadioAddress    string
	}{web.arena.EventSettings, team, station, errorMessage, successMessage, field.MinWpaKeyLength,
		field.MaxWpaKeyLength, radioAddress}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}
