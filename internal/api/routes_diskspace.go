package api

// How much room is left where downloads land. A browser cannot work this out
// for itself: the real destination comes out of app.dirFor, which applies a
// rule's folder, then the category's, then the path template, then the
// per-package level, while Task.Dir only carries an explicit override.
//
// The route is on neither forwarding list (routes_federation.go,
// routes_relay.go), since a peer's answer describes that box's volumes and
// would be drawn under this box's task list. Whatever shows it has to withhold
// it when the scope is not local, the same way the speed limit field does.
//
// It answers to whoever holds a session and sends folder paths that
// /api/folders already lists, plus byte counts for the volumes they sit on.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerDiskSpace(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/diskspace",
		"free and used space on every folder a download can land in, plus what the queue still owes each one",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.DiskReport())
		})
}
