package api

// Liveness, the event stream, the look, and what the Security page says
// beside the sign-in.

import (
	"encoding/json"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// appearanceFields is the allowlist both halves of /api/appearance are built
// from, so a field cannot be readable without being writable or the other way
// round.
var appearanceFields = []string{
	"shape", "accent", "theme", "rainbow", "rainbowReactive", "rainbowRotate", "rainbowSeed", "rainbowPalette",
	"torrentCard", "finishedCard",
}

func appearanceOf(s settings.Settings) map[string]any {
	return map[string]any{
		"shape":           s.Shape,
		"accent":          s.Accent,
		"theme":           s.Theme,
		"rainbow":         s.Rainbow,
		"rainbowReactive": s.RainbowReactive,
		"rainbowRotate":   s.RainbowRotate,
		"rainbowSeed":     s.RainbowSeed,
		"rainbowPalette":  s.RainbowPalette,
		"torrentCard":     s.TorrentCard,
		"finishedCard":    s.FinishedCard,
	}
}

func registerSystem(reg *Registry, a *app.App) {
	// `status` and `version` must stay as they are: the phone app compares the
	// literal "ok", and the Click'n'Load bridge refuses to start on anything
	// else. `commit` is always present, empty when unknown, so a caller can
	// tell an old server from a build that does not know its revision.
	reg.AddOpen(http.MethodGet, "/api/health",
		"liveness, the running version and the commit it was built from; open so a container orchestrator can probe a locked instance",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]string{"status": "ok", "version": buildinfo.Version, "commit": buildinfo.Revision()})
		})

	reg.Add(http.MethodGet, "/api/ws", "the live task, queue and activity stream",
		func(w http.ResponseWriter, r *http.Request) {
			serveWS(a, w, r)
		})

	// Only the cosmetics, so a client matching the instance's look (including
	// a relay sibling) never needs /api/settings, which stays off the relay
	// allowlist.
	reg.Add(http.MethodGet, "/api/appearance",
		"the instance's own accent, corner shape, theme, rainbow settings and which cards its download list is split into - what a client needs to match its look, and nothing else",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, appearanceOf(a.Settings.Get()))
		})

	// The same fields, written. The palette lives on the instance rather than
	// per client because colours are handed out by position, and every client
	// has to agree on them. Only the named fields are applied, so this is no
	// second door into /api/settings.
	reg.Add(http.MethodPost, "/api/appearance",
		"set the instance's accent, corner shape, theme, rainbow settings and list cards - the same fields GET answers with, and nothing else",
		func(w http.ResponseWriter, r *http.Request) {
			var body map[string]json.RawMessage
			if !decodeJSON(w, r, &body) {
				return
			}
			patch := map[string]json.RawMessage{}
			for _, f := range appearanceFields {
				if v, ok := body[f]; ok {
					patch[f] = v
				}
			}
			if len(patch) == 0 {
				http.Error(w, "no appearance field named", http.StatusBadRequest)
				return
			}
			applied, err := a.PatchSettings(patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, appearanceOf(applied))
		})

	reg.Add(http.MethodGet, "/api/system/access",
		"what the Security page says beside the sign-in: until when the copies from before the encryption key are kept, and whether a reverse proxy sent requests lately",
		func(w http.ResponseWriter, r *http.Request) {
			out := map[string]any{"appKey": a.AppKey != nil, "behindProxy": reg.proxySeen.recently()}
			if until, ok := a.KeyCopiesUntil(); ok {
				out["appKeyCopiesUntil"] = until
			}
			writeJSON(w, out)
		})
}
