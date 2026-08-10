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
		"stage links in the collector; optional per-batch destination, priority, unpacking switch, comment, the two passwords, whether they overwrite a matching Packagizer rule, and which entrance they arrived by — returns the tasks created",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Links   string `json:"links"` // newline-separated, like JD's paste box
				Package string `json:"package"`
				// Origin is the entrance the caller decoded these links from, for
				// the relays that know one. A Click'n'Load bridge runs on the
				// user's desktop because CnL cannot reach a NAS, and this is the
				// only place it can say that a browser button — not the paste box
				// — is where these came from. Absent means the paste box, which is
				// what the interface sends and what a link posted by hand is.
				Origin string `json:"origin"`
				// Passwords ride along with a Click'n'Load submission, the one
				// moment an archive password is known without asking for it. The
				// bridge has sent them since it was written and nothing here read
				// them, so on every bridged deployment the password was dropped
				// and the extraction then asked for one that had already been
				// handed over.
				Passwords []string `json:"passwords"`

				// The rest are the add-links form's own per-batch options
				// (build-plan.md §8A) — see app.LinkBatchOptions, which every one
				// of these is decoded straight into. All are optional, and a
				// request that sends none of them behaves exactly as it always
				// has: this is what keeps the branch below backward compatible
				// with a Click'n'Load submission's plain "passwords" list, and
				// with a bare paste that names neither.
				Dir              string `json:"dir"`
				Password         string `json:"password"`
				DownloadPassword string `json:"downloadPassword"`
				Comment          string `json:"comment"`
				Priority         *int   `json:"priority"`
				AutoExtract      *bool  `json:"autoExtract"`
				// Overrule makes Priority, AutoExtract and Comment win over a
				// Packagizer rule that would otherwise set the same property —
				// see app.LinkBatchOptions.Overrule for why Dir and the two
				// passwords are never subject to it.
				Overrule bool `json:"overrule"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			origin := app.OriginPaste
			if body.Origin != "" {
				known, ok := app.KnownOrigin(body.Origin)
				if !ok {
					// Refused rather than filed as pasted. A wrong entrance is worse
					// than none: it is the answer to "why is this here", and one that
					// quietly means "we could not tell" is believed.
					http.Error(w, "unknown origin "+strconv.Quote(body.Origin), http.StatusBadRequest)
					return
				}
				origin = known
			}
			urls := extractLinks(a.Settings.Get().PreParserEnabled, body.Links)
			var created []*core.Task
			if len(body.Passwords) > 0 {
				// Click'n'Load's own shape, untouched: several candidate passwords
				// a page offered rather than the one box a form has, so it keeps
				// its own path rather than being squeezed through LinkBatchOptions.
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
					// The one field here that can genuinely be wrong rather than
					// merely absent — a destination a person just typed deserves
					// its own sentence back, not a batch quietly staged to the
					// wrong folder.
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if created == nil {
				created = []*core.Task{} // an empty result is [] for clients, never null
			}
			writeJSON(w, created)
		})

	// The holding area. A link the filter refused is a task carrying skipped and
	// the reason, so a browser already has it from the task stream and does not
	// need this — it is here for the clients that are not a browser, and so the
	// self-describing index says the holding area exists at all.
	reg.Add(http.MethodGet, "/api/collector/filtered", "links the link filter is holding, with the rule and the reason",
		func(w http.ResponseWriter, r *http.Request) {
			held := a.FilteredLinks()
			if held == nil {
				held = []*core.Task{}
			}
			writeJSON(w, held)
		})

	// Restore is a POST and takes its ids in a body rather than in the path,
	// because it is one decision about a set of links: restoring forty one
	// request at a time would put forty rows through the queue in an order
	// nobody chose, and forty chances for the fifth to fail on its own.
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

	// Clearing takes its ids from the query, because DELETE with a body is not
	// something every client and proxy on the way here handles. No ids empties
	// the holding area; nothing on disk is touched, because a held link never
	// downloaded anything.
	reg.Add(http.MethodDelete, "/api/collector/filtered", "delete links the filter is holding (?ids=a,b, or no ids for all of them)",
		func(w http.ResponseWriter, r *http.Request) {
			removed := a.ClearFiltered(idsFromQuery(r))
			writeJSON(w, map[string]any{"removed": len(removed), "ids": removed})
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

// extractLinks turns the paste box's raw text into the URLs to stage.
//
// Enabled is internal/settings' PreParserEnabled, JD's own
// AddLinksPreParserEnabled by another name: on, internal/linkscan scans the
// whole blob for links wherever they sit in it, rejoins one a mail client
// wrapped across a line break, and falls back to reading a bare
// "host/path" line as a link with no scheme in front of it. Off is the
// literal behaviour this route always had - one line taken as one link,
// verbatim - kept reachable because a paste that linkscan mis-reads needs
// an escape hatch that does not depend on linkscan agreeing with itself.
func extractLinks(enabled bool, blob string) []string {
	if !enabled {
		return strings.FieldsFunc(blob, func(r rune) bool { return r == '\n' || r == '\r' })
	}
	return linkscan.Extract(blob)
}

// idsFromQuery reads a comma-separated ?ids= list. An empty or absent parameter
// is "all of them", which is why blanks are dropped rather than passed on: a
// trailing comma would otherwise contribute an id that matches nothing, turning
// "these two" into "these two and one that is not there" rather than into "all".
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
