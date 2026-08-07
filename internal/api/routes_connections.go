package api

// The connection manager's two verbs that are not a settings save.
//
// The list of connections itself deliberately has no endpoint here. It is part
// of the settings document, it is served redacted by GET /api/settings and
// written by PUT /api/settings, and that path already validates every row and
// merges back the passwords the client was never shown. A second write path for
// the same field would be a second place for that merge to be forgotten, and the
// symptom of forgetting it — every proxy password cleared on the next save — is
// one nobody notices until a download fails days later.
//
// What is left is the two things a save cannot do: read a pasted list, and find
// out whether a row actually works.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

func registerConnections(reg *Registry, a *app.App) {
	// Parse only. Nothing is stored, because the whole reason the parser names
	// the lines it refuses is that somebody is meant to read them before the list
	// is committed — and because writing here would collide with the settings
	// draft the page is already holding.
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
			// The password is the one field the client cannot send: it was never
			// shown one. Merge is the same machinery a save goes through, so a test
			// answers about exactly the connection a save would write — including
			// its refusal to carry a password to a row whose endpoint has been
			// edited, which is why testing an edited host asks for the password
			// again instead of quietly probing with the old one.
			merged := proxycfg.Merge([]proxycfg.Entry{body.Entry}, a.Settings.Get().Connections)
			// Bounded by the request's own context: a browser that navigated away
			// must not leave a goroutine waiting on a proxy that will never answer.
			writeJSON(w, proxycfg.Probe(r.Context(), merged[0], strings.TrimSpace(body.Target)))
		})
}
