package api

// Links coming in, and the trace of the ones that did not make it.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func registerLinks(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/links", "stage pasted links in the collector; returns the tasks created",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Links   string `json:"links"` // newline-separated, like JD's paste box
				Package string `json:"package"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			urls := strings.FieldsFunc(body.Links, func(r rune) bool { return r == '\n' || r == '\r' })
			created := a.AddLinks(urls, body.Package)
			if created == nil {
				created = []*core.Task{} // an empty result is [] for clients, never null
			}
			writeJSON(w, created)
		})

	// The trace for links that never became tasks. A link folded away with
	// nothing to show for it looks exactly like a bug in the paste box.
	reg.Add(http.MethodGet, "/api/collector/skipped", "links that were folded into one already in the list, and why",
		func(w http.ResponseWriter, r *http.Request) {
			skipped := a.SkippedLinks()
			if skipped == nil {
				skipped = []app.SkippedLink{}
			}
			writeJSON(w, skipped)
		})
	reg.Add(http.MethodDelete, "/api/collector/skipped", "empty that trace",
		func(w http.ResponseWriter, r *http.Request) {
			a.ClearSkipped()
			w.WriteHeader(http.StatusNoContent)
		})
}
