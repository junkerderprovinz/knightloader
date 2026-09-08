package api

// Database maintenance: how big the store has grown, what the last integrity
// check or compaction found, and the two-word request that starts another one.
//
// WHY BOTH VERBS SHARE ONE PATH. The GET is the whole state and the POST
// answers with the same document, so a client that starts a run has the new
// state in hand without a second round trip, and the polling it then does is
// the same call it already made on mount. One shape, one decoder, one place for
// the field the next wave adds.
//
// ASYNCHRONOUS, AND THAT IS THE DESIGN RATHER THAN AN OPTIMISATION. A
// compaction rewrites the entire database; on a multi-gigabyte store that
// outlives any browser's timeout and any reverse proxy's, and there is no
// status code for "still going". So the POST hands the work to
// app.StartMaintenance, answers 202 Accepted with the state as it stands, and
// the page polls the GET while `running` is non-empty. A synchronous handler
// would give the operator a dead tab and no way to find out whether their
// database was being rewritten behind it.
//
// DELIBERATELY NOT ON relayForwardable (routes_relay.go), and this is not a
// list somebody forgot to complete. That allowlist is what a group sibling may
// reach on this instance, and its own comment says everything outside the
// task/link/queue set is GET only - "a sibling may look, never change". A peer
// able to POST here could freeze every write on this box for ten minutes, which
// is a denial of service dressed as a feature. The GET is left off too: it
// carries this machine's own data-directory paths, and a row of them rendered
// while somebody is looking at a peer would name the wrong machine's disk with
// total confidence - the same argument registerDiskSpace makes in routes.go.
//
// SECURITY: reg.Add and never AddOpen. There is no credential of its own in
// this request, so it rides the session guard like everything else under /api/.
// The paths it sends go no further than the person already looking at their own
// settings pages; they are deliberately NOT in the diagnostics bundle, which is
// a file meant to be attached to a public bug report. See routes_diagnostics.go.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// maintenanceRequest is the POST body. One field, because the three actions
// differ only in which verb runs: everything else about a pass - what it
// records, how it is polled, how it is refused - is identical, and three routes
// would be three copies of that.
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
				// Named, rather than a bare 400. The three verbs are a closed
				// set and a client that sent a fourth has a typo, not a
				// version problem; saying which three there are turns a
				// debugging session into a glance at the response.
				http.Error(w, "action must be one of check, compact or analyze", http.StatusBadRequest)
				return
			}
			if err := a.StartMaintenance(kind); err != nil {
				if errors.Is(err, app.ErrMaintenanceBusy) {
					// 409 and not 429: this is not a rate limit that will pass
					// on its own schedule, it is a conflict with a specific
					// thing that is happening, and the state in the answer
					// says what that thing is so the page can show it rather
					// than inventing a message.
					writeJSONStatus(w, http.StatusConflict, a.MaintenanceState())
					return
				}
				// ErrMaintenanceClosing, or anything a later wave adds. 503,
				// because the request was fine and this server is on its way
				// out - a 500 would send somebody looking for a bug.
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			// 202 rather than 200: the work has been accepted and has not
			// happened. The body is the state as it stands, which already
			// carries running=<kind>, so the page can go straight into its
			// poll without a second call to find out what it just started.
			writeJSONStatus(w, http.StatusAccepted, a.MaintenanceState())
		})
}
