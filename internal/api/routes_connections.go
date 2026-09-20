package api

// The connection manager's two verbs that are not a settings save: reading a
// pasted list, and finding out whether a row works.
//
// The list itself has no endpoint here. It is part of the settings document,
// and PUT /api/settings already validates every row and merges back the
// passwords the client was never shown. A second write path would be a second
// place to forget that merge, and forgetting it clears every proxy password on
// the next save.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

func registerConnections(reg *Registry, a *app.App) {
	// Parse only. The refused lines are meant to be read before the list is
	// committed, and a write here would collide with the settings draft the
	// page is holding.
	reg.Add(http.MethodPost, "/api/connections/import",
		"read a pasted proxy list into rows, naming every line it refuses and why; stores nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Text string `json:"text"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// The stored list goes in so a line naming a connection that is already
			// configured is refused rather than added a second time.
			writeJSON(w, proxycfg.ParseList(body.Text, a.Settings.Get().Connections))
		})

	reg.Add(http.MethodPost, "/api/connections/test",
		"reach one connection and report how far it got, taking the password from the stored row when the client has none",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Entry  proxycfg.Entry `json:"entry"`
				Target string         `json:"target"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// The client was never shown the password, so a test goes through
			// the same merge a save does and probes exactly the connection a
			// save would write. Merge refuses to carry a password to a row
			// whose endpoint was edited, so testing an edited host asks for
			// the password again rather than probing with the old one.
			merged := proxycfg.Merge([]proxycfg.Entry{body.Entry}, a.Settings.Get().Connections)
			// Bounded by the request's own context: a browser that navigated away
			// must not leave a goroutine waiting on a proxy that will never answer.
			writeJSON(w, proxycfg.Probe(r.Context(), merged[0], strings.TrimSpace(body.Target)))
		})
}
