package api

// Database maintenance: how big the store has grown, what the last integrity
// check or compaction found, and the request that starts another one.
//
// Both verbs share one path and one document, so a client that starts a run
// has the new state without a second round trip and polls with the same call
// it made on mount.
//
// The POST is asynchronous because a compaction rewrites the entire database,
// which on a multi-gigabyte store outlives any browser or proxy timeout. It
// hands the work to app.StartMaintenance, answers 202 with the state as it
// stands, and the page polls the GET while running is non-empty.
//
// On neither forwarding list (routes_relay.go): a peer able to POST here could
// freeze every write on this box for ten minutes, and the GET carries this
// machine's own data-directory paths, which drawn under a peer's list would
// name the wrong machine's disk. Those paths stay out of the diagnostics
// bundle too, since that file is attached to public bug reports; see
// routes_diagnostics.go.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// maintenanceRequest is the POST body. The three actions differ only in which
// verb runs; what a pass records, how it is polled and how it is refused are
// the same, so they share one route.
type maintenanceRequest struct {
	Action string `json:"action"`
}

func registerDBMaintenance(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/system/maintenance",
		"database size, what the last check or compaction found, and whether one is running now",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.MaintenanceState())
		})

	reg.Add(http.MethodPost, "/api/system/maintenance",
		"start an integrity check, a compaction or a statistics refresh; answers 409 while one is already running",
		func(w http.ResponseWriter, r *http.Request) {
			var req maintenanceRequest
			if !decodeJSON(w, r, &req) {
				return
			}
			kind, ok := app.ParseMaintenanceKind(req.Action)
			if !ok {
				// The three verbs are a closed set, so naming them saves a
				// client with a typo from reading the source.
				http.Error(w, "action must be one of check, compact or analyze", http.StatusBadRequest)
				return
			}
			if err := a.StartMaintenance(kind); err != nil {
				if errors.Is(err, app.ErrMaintenanceBusy) {
					// 409 and not 429: this is a conflict with a run that is
					// happening, not a rate limit, and the state in the answer
					// says which run so the page need not invent a message.
					writeJSONStatus(w, http.StatusConflict, a.MaintenanceState())
					return
				}
				// ErrMaintenanceClosing. 503 because the request was fine and
				// the server is on its way out; a 500 would send somebody
				// looking for a bug.
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			// 202: the work has been accepted, not done. The body carries
			// running=<kind>, so the page goes straight into its poll.
			writeJSONStatus(w, http.StatusAccepted, a.MaintenanceState())
		})
}
