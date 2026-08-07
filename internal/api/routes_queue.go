package api

// The queue master switch.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerQueue(reg *Registry, a *app.App) {
	// Separate from pausing a task: this decides whether anything new starts at
	// all.
	reg.Add(http.MethodGet, "/api/queue", "the master switch, the stop mark, and how many downloads are in flight",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Queue())
		})
	reg.Add(http.MethodPost, "/api/queue", `halt or release the queue, and arm "finish this one, then stop"`,
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Halted   *bool   `json:"halted,omitempty"`
				StopMark *string `json:"stopMark,omitempty"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Halted != nil {
				a.SetHalted(*body.Halted)
			}
			if body.StopMark != nil {
				a.SetStopMark(*body.StopMark)
			}
			writeJSON(w, a.Queue())
		})
}
