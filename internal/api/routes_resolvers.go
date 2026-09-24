package api

// The resolver registry's own facts: the order configured services are tried
// in, and the headless-JD sidecar's status. Neither carries a credential.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

func registerResolvers(reg *Registry, a *app.App) {
	// host is optional: absent, this is the whole registered order; given, it
	// is narrowed to the chain that applies to that host, in the order it is
	// walked.
	reg.Add(http.MethodGet, "/api/resolvers/priority",
		"which configured service is asked first, optionally narrowed to one host (?host=)",
		func(w http.ResponseWriter, r *http.Request) {
			// a.ResolverPriority rather than the registry's own AllInfo and
			// PriorityFor, which answer its frozen registration-time order;
			// dispatch walks the dynamic one. See ResolverPriority.
			writeJSON(w, a.ResolverPriority(r.URL.Query().Get("host")))
		})

	// The drag-and-drop half of the same card. An empty list is the reset: it
	// puts the ladder back to the automatic order every install starts with.
	//
	// Through PatchSettings rather than the Settings pages' shared draft, like
	// the yt-dlp preset below: this fires from a card on the Accounts page,
	// which holds no draft, while a tab's unrelated settings edits may still
	// be unsaved. A whole-document PUT would save those too.
	reg.Add(http.MethodPost, "/api/resolvers/priority",
		"save the hand-arranged order services are asked in; an empty list restores the automatic order",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Order []string `json:"order"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Re-read after the save rather than echoed from the request:
			// sanitize drops blanks and repeats, so a card redrawn from the
			// request would show an order the server is not using.
			rows, err := a.SaveResolverOrder(body.Order)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, rows)
		})

	reg.Add(http.MethodGet, "/api/resolvers/jd",
		"whether the headless-JD sidecar is configured, reachable, and which revision it runs",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.JDStatus())
		})

	// The "Variante" badge's read path (TaskList.tsx's PackageGroup header):
	// what a package's host would stage right now, saved or
	// ytdlp.DefaultHosterPreset() when nothing is saved for it, as a new link
	// would see it.
	reg.Add(http.MethodGet, "/api/ytdlp/preset",
		"a hoster's own \"Variante\" preset (?host=), or the default if none is saved",
		func(w http.ResponseWriter, r *http.Request) {
			host := strings.TrimSpace(r.URL.Query().Get("host"))
			if host == "" {
				http.Error(w, "which host is this preset for?", http.StatusBadRequest)
				return
			}
			writeJSON(w, a.HosterPresetFor(host))
		})

	// The badge's write path. Through PatchSettings rather than the settings
	// draft (see SetHosterPreset in app_ytdlp_variants.go): it fires from a
	// popover reachable at any moment, not from that draft's Save button.
	reg.Add(http.MethodPost, "/api/ytdlp/preset", "save one hoster's own \"Variante\" preset",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host string `json:"host"`
				ytdlp.HosterPreset
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if strings.TrimSpace(body.Host) == "" {
				http.Error(w, "which host is this preset for?", http.StatusBadRequest)
				return
			}
			if err := a.SetHosterPreset(body.Host, body.HosterPreset); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
