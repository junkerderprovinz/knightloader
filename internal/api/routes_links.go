package api

// Links coming in, the holding area the filter puts them in, and the trace of
// the ones that never became a task at all.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
)

func registerLinks(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/links",
		"stage links in the collector; optional per-batch destination, priority, unpacking switch, comment, the two passwords, whether they overwrite a matching Packagizer rule, and which entrance they arrived by; returns the tasks created",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Links   string `json:"links"` // newline-separated, like JD's paste box
				Package string `json:"package"`
				// Origin is the entrance the links came through, such as a
				// Click'n'Load bridge on the user's desktop. Absent means the
				// paste box.
				Origin string `json:"origin"`
				// Passwords ride along with a Click'n'Load submission, the one
				// moment an archive password is known without asking for it.
				Passwords []string `json:"passwords"`

				// The add-links form's optional per-batch options, decoded into
				// app.LinkBatchOptions.
				Dir              string `json:"dir"`
				Password         string `json:"password"`
				DownloadPassword string `json:"downloadPassword"`
				Comment          string `json:"comment"`
				Priority         *int   `json:"priority"`
				AutoExtract      *bool  `json:"autoExtract"`
				// Overrule makes Priority, AutoExtract and Comment win over a
				// matching Packagizer rule (see app.LinkBatchOptions.Overrule).
				Overrule bool `json:"overrule"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			origin := app.OriginPaste
			if body.Origin != "" {
				known, ok := app.KnownOrigin(body.Origin)
				if !ok {
					// Refused rather than filed as pasted, because a wrong
					// entrance would be believed.
					http.Error(w, "unknown origin "+strconv.Quote(body.Origin), http.StatusBadRequest)
					return
				}
				origin = known
			}
			urls := extractLinks(a.Settings.Get().PreParserEnabled, body.Links)
			var created []*core.Task
			if len(body.Passwords) > 0 {
				// Click'n'Load offers several candidate passwords rather than the
				// form's one, so it keeps its own path.
				created = a.AddLinksWithPasswords(urls, body.Package, body.Passwords, origin)
			} else {
				var err error
				created, err = a.AddLinksWithOptions(urls, body.Package, origin, app.LinkBatchOptions{
					Dir:              body.Dir,
					Password:         body.Password,
					DownloadPassword: body.DownloadPassword,
					Comment:          body.Comment,
					Priority:         body.Priority,
					AutoExtract:      body.AutoExtract,
					Overrule:         body.Overrule,
				})
				if err != nil {
					// Usually a destination folder that was just typed and is
					// wrong.
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if created == nil {
				created = []*core.Task{} // an empty result is [] for clients, never null
			}
			writeJSON(w, created)
		})

	// A browser sees held links on the task stream; this is for other clients.
	reg.Add(http.MethodGet, "/api/collector/filtered", "links the link filter is holding, with the rule and the reason",
		func(w http.ResponseWriter, r *http.Request) {
			held := a.FilteredLinks()
			if held == nil {
				held = []*core.Task{}
			}
			writeJSON(w, held)
		})

	// One request for the whole set, so the restored links enter the queue
	// together and in order.
	reg.Add(http.MethodPost, "/api/collector/filtered/restore",
		"put links the filter is holding back in the collector, with the filter waived for those links (no ids = all of them)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				IDs []string `json:"ids"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			restored := a.RestoreFiltered(body.IDs)
			if restored == nil {
				restored = []*core.Task{}
			}
			writeJSON(w, restored)
		})

	// Ids come from the query because not every client and proxy handles a
	// DELETE body. Nothing on disk is touched; a held link never downloaded.
	reg.Add(http.MethodDelete, "/api/collector/filtered", "delete links the filter is holding (?ids=a,b, or no ids for all of them)",
		func(w http.ResponseWriter, r *http.Request) {
			removed := a.ClearFiltered(idsFromQuery(r))
			writeJSON(w, map[string]any{"removed": len(removed), "ids": removed})
		})

	// Links that never became tasks, so a folded duplicate does not look like a
	// bug in the paste box.
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

// extractLinks turns the paste box's raw text into the URLs to stage. With the
// pre-parser enabled, linkscan finds links anywhere in the text; without it
// every line is one link, verbatim, as an escape hatch when linkscan misreads a
// paste.
func extractLinks(enabled bool, blob string) []string {
	if !enabled {
		return strings.FieldsFunc(blob, func(r rune) bool { return r == '\n' || r == '\r' })
	}
	return linkscan.Extract(blob)
}

// idsFromQuery reads a comma-separated ?ids= list; empty or absent means all.
// Blanks from a trailing comma are dropped.
func idsFromQuery(r *http.Request) []string {
	raw := r.URL.Query().Get("ids")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}
