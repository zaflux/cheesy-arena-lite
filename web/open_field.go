// Admin page for turning Open Field mode on/off and monitoring/disabling all stations.

package web

import "net/http"

func (web *Web) openFieldHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented yet.", http.StatusNotImplemented)
}

func (web *Web) openFieldWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented yet.", http.StatusNotImplemented)
}
