package api

// The speed record: one route over the pair of in-memory rings this instance
// keeps of its own aggregate download speed (internal/app/app_speedhistory.go).
//
// ONE ROUTE AND NOT TWO, unlike the volume pair next door. routes_stats.go has
// two because it serves two different callers with two different appetites -
// the status bar wants one number on every page load and must not pull 42
// buckets to draw it. Here both arrays have the same single consumer, the
// speed window in the browser (web/src/lib/speedHistory.ts), which seeds the
// hero curve and the shell meter from one fetch. Splitting them would mean two
// requests on every page load to fill one store.
//
// NO QUERY PARAMETERS, following the rule routes_stats.go:14-18 states for the
// volume curves: both arrays are bounded by construction, "which is a promise a
// limit parameter would quietly take away". A ?window= or a ?limit= here would
// turn a fixed 4 KB answer into something a client could ask to be arbitrarily
// large or arbitrarily truncated, and there is nothing either would buy - the
// caps are in the answer, so a client that wants sixty of the hundred and
// twenty takes the last sixty itself.
//
// DELIBERATELY NOT FORWARDABLE. It is on neither allowlist: not
// routes_federation.go:75-79 (what a browser may ask this instance to relay to
// a peer) and not routes_relay.go:441-496 (what a group sibling may ask of
// this instance). A peer's ring describes the PEER's traffic, and a curve
// showing this box's last hour under a figure describing somebody else's box is
// what routes.go:69-75 already calls the worst kind of wrong for disk space.
// Served through neither list, the shell meter simply gets no seed while it is
// scoped to a peer and draws its own live window from that peer's task stream,
// which is correct rather than merely safe.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerSpeedHistory(reg *Registry, a *app.App) {
	// The summary is not tidiness. It is what the self-describing index at
	// GET /api/ prints, and it is where an operator reading a flat curve at
	// three in the morning finds out that an empty record after a restart is
	// the design and not a fault.
	reg.Add(http.MethodGet, "/api/stats/speed",
		"the aggregate download speed this instance has been recording since it started: the last two minutes a second apart and the last hour ten seconds apart, held in memory only and empty again after a restart",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.SpeedHistory())
		})
}
