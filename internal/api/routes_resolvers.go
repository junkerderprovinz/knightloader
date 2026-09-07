package api

// The resolver registry's own facts, surfaced read-only: the deterministic
// order configured services are tried in (resolver.Registry.AllInfo/
// PriorityFor), and the headless-JD sidecar's own status - neither a
// credential, both informational.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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
			// a.ResolverPriority, not a.Registry's own AllInfo/
			// PriorityFor: the registry answers its frozen
			// registration-time order, and dispatch stopped walking that
			// order when dynamicPrio arrived - see ResolverPriority's own
			// doc comment.
			writeJSON(w, a.ResolverPriority(r.URL.Query().Get("host")))
		})

	// The drag-and-drop half of that same card (jdp, 2026-09-07: "Die
	// Prioritätsreihenfolge soll per drag and drop anordenbar sein"). An
	// empty list is not an error, it is the reset: it puts the ladder back
	// to the automatic order, which is what every install starts with.
	//
	// Through PatchSettings rather than the Settings pages' shared draft,
	// for the same reason the yt-dlp preset below is: this fires from a card
	// on the Accounts page, which is not the settings shell and holds no
	// draft, at a moment when a browser tab's own unrelated settings edits
	// may still be unsaved. A whole-document PUT from here would save those
	// too.
	reg.Add(http.MethodPost, "/api/resolvers/priority",
		"save the hand-arranged order services are asked in; an empty list restores the automatic order",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Order []string `json:"order"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			raw, err := json.Marshal(body.Order)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := a.PatchSettings(map[string]json.RawMessage{"resolverOrder": raw}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The registry's own view, re-read after the save rather than
			// echoed from the request: sanitize drops blanks and repeats,
			// so what was stored is not necessarily what was sent, and a
			// card that redrew from the request would show an order the
			// server is not using.
			writeJSON(w, a.ResolverPriority(""))
		})

	reg.Add(http.MethodGet, "/api/resolvers/jd",
		"whether the headless-JD sidecar is configured, reachable, and which revision it runs",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.JDStatus())
		})

	// The "Variante" gear badge's own read path (TaskList.tsx's PackageGroup
	// header): what a package's own host would stage as its five rows right
	// now - saved (app.hosterPresetFor), or ytdlp.DefaultHosterPreset() when
	// nothing has been saved for it yet, exactly as a new link would see it.
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

	// The gear badge's own write path - see SetHosterPreset's own doc
	// comment (app_ytdlp_variants.go) for why this goes through
	// PatchSettings rather than the general settings draft the rest of the
	// Settings pages save through: it fires from a popover reachable at any
	// moment while a browser tab's own unrelated settings edits may still be
	// unsaved, not from that draft's own explicit Save button.
	reg.Add(http.MethodPost, "/api/ytdlp/preset", "save one hoster's own \"Variante\" preset",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host        string          `json:"host"`
				Variants    []ytdlp.Variant `json:"variants"`
				Quality     ytdlp.Quality   `json:"quality"`
				AudioFormat string          `json:"audioFormat"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if strings.TrimSpace(body.Host) == "" {
				http.Error(w, "which host is this preset for?", http.StatusBadRequest)
				return
			}
			if err := a.SetHosterPreset(body.Host, ytdlp.HosterPreset{
				Variants:    body.Variants,
				Quality:     body.Quality,
				AudioFormat: body.AudioFormat,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
