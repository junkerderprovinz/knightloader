package api

// The speed record: one route over the pair of in-memory rings this instance
// keeps of its own aggregate download speed (internal/app/app_speedhistory.go).
//
// One route rather than the pair routes_stats.go serves, because both arrays
// have the same consumer, the speed window in web/src/lib/speedHistory.ts,
// which seeds the hero curve and the shell meter from one fetch.
//
// No query parameters. Both arrays are bounded by construction, and a ?window=
// or ?limit= would take that promise away without buying anything: the caps
// are in the answer, so a client that wants the last sixty buckets takes them
// itself.
//
// On neither forwarding list (routes_federation.go, routes_relay.go), since a
// peer's ring describes the peer's traffic. Scoped to a peer, the shell meter
// gets no seed and draws its own window from that peer's task stream.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerSpeedHistory(reg *Registry, a *app.App) {
	// The summary is what the self-describing index at GET /api/ prints, so it
	// spells out that an empty record after a restart is the design.
	reg.Add(http.MethodGet, "/api/stats/speed",
		"the aggregate download speed this instance has been recording since it started: the last two minutes a second apart and the last hour ten seconds apart, held in memory only and empty again after a restart",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.SpeedHistory())
		})
}
