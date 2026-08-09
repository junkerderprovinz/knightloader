package api

// The resolver registry's own facts, surfaced read-only: the deterministic
// order configured services are tried in (resolver.Registry.AllInfo/
// PriorityFor), and the headless-JD sidecar's own status - neither a
// credential, both informational.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerResolvers(reg *Registry, a *app.App) {
	// host is optional: absent, this is the whole registered order (every
	// service, regardless of what it matches); given, it is narrowed to the
	// chain that actually applies to that one host, in the order it is
	// walked - see resolver.Registry.PriorityFor's own doc comment for why a
	// deterministic order nobody could see used to be, in every way that
	// matters to the person who configured it, the same as no order at all.
	reg.Add(http.MethodGet, "/api/resolvers/priority",
		"which configured service is asked first, optionally narrowed to one host (?host=)",
		func(w http.ResponseWriter, r *http.Request) {
			host := strings.TrimSpace(r.URL.Query().Get("host"))
			if host == "" {
				writeJSON(w, a.Registry.AllInfo())
				return
			}
			writeJSON(w, a.Registry.PriorityFor(host))
		})

	reg.Add(http.MethodGet, "/api/resolvers/jd",
		"whether the headless-JD sidecar is configured, reachable, and which revision it runs",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.JDStatus())
		})
}
