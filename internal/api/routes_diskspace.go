package api

// How much room is left where downloads land.
//
// WHY IT IS A ROUTE AT ALL. The interface cannot work this out for itself, and
// not for want of trying: a browser has no way to stat a folder, Task.Dir only
// ever carries an explicit per-task override, and the real destination comes
// out of app.dirFor, which applies a rule's folder, then the category's, then
// the path template, then the per-package level. Anything computed in the
// browser from the task list would show the download folder for every row and
// be wrong on every install that uses categories.
//
// THIS MACHINE'S DISKS, AND ONLY THIS MACHINE'S. The route is deliberately not
// on the federation forwarder's list (routes_federation.go) and not on the
// relay allowlist (routes_relay.go, whose own comment says a new route stays
// outside it until somebody decides otherwise). Neither is an oversight to be
// tidied up: a peer's answer would describe THAT box's volumes, and a row of
// them shown while a peer's list is on screen names the wrong machine's disks
// with total confidence. What draws this has to withhold it when the scope is
// not local, the same way the speed limit field already does.
//
// SECURITY, because this answers to whoever holds a session. It sends folder
// paths - the same paths /api/folders already lists, and the same ones the
// settings page shows in a text box - together with byte counts for the volumes
// they sit on. No file names, no contents, nothing about any folder the app
// would not itself write into. reg.Add and never AddOpen: there is no
// credential of its own in this request, so it rides the session guard like
// everything else under /api/.
//
// It writes nothing. See app.DiskReport, which explains at length why the
// obvious way to check a folder (settings.Validate) is the one thing that must
// never be called from here.

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
