// Per-station page where a team picks its team/alliance station and enables/disables its robot.

package web

import "net/http"

func (web *Web) stationControlHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented yet.", http.StatusNotImplemented)
}

func (web *Web) stationControlWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented yet.", http.StatusNotImplemented)
}
