package api

// Liveness, the event stream, and the password lock.

import (
	"encoding/json"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// appearanceFields is the allowlist BOTH halves of /api/appearance are built
// from - one list, so a field can never be readable and not writable, or worse,
// writable and not readable. Anything not named here is not an appearance
// field, whatever a caller puts in the body.
var appearanceFields = []string{
	"shape", "accent", "rainbow", "rainbowReactive", "rainbowRotate", "rainbowSeed", "rainbowPalette",
}

func registerSystem(reg *Registry, a *app.App) {
	reg.AddOpen(http.MethodGet, "/api/health",
		"liveness and the running version; open so a container orchestrator can probe a locked instance",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]string{"status": "ok", "version": buildinfo.Version})
		})

	reg.Add(http.MethodGet, "/api/ws", "the live task, queue and activity stream",
		func(w http.ResponseWriter, r *http.Request) {
			serveWS(a, w, r)
		})

	// Just the cosmetics, so that wanting them is not a reason to hand over
	// anything else. The phone app wears whatever accent and corner shape the
	// instance it is looking at uses, and it used to get them by reading the
	// WHOLE settings document and picking seven fields out - which was a fair
	// trade against inventing an endpoint right up until a group sibling could
	// make that call over the relay. Now the narrow thing exists, /api/settings
	// stays off the relay allowlist, and nobody reads a download path to find
	// out which shade of orange to paint a button.
	reg.Add(http.MethodGet, "/api/appearance",
		"the instance's own accent, corner shape and rainbow settings - what a client needs to match its look, and nothing else",
		func(w http.ResponseWriter, r *http.Request) {
			s := a.Settings.Get()
			writeJSON(w, map[string]any{
				"shape":           s.Shape,
				"accent":          s.Accent,
				"rainbow":         s.Rainbow,
				"rainbowReactive": s.RainbowReactive,
				"rainbowRotate":   s.RainbowRotate,
				"rainbowSeed":     s.RainbowSeed,
				"rainbowPalette":  s.RainbowPalette,
			})
		})

	// The same seven fields, written.
	//
	// It exists because the app could show the instance's palette and not touch
	// it (jdp, 2026-09-01: "wo sind die farbfelder für den regenbogenmodus?",
	// and before that "alle farbfelder lassen sich nicht bearbeiten"). The
	// alternative was a palette kept locally on the phone, which breaks the one
	// property that makes a palette worth having: colours are handed out by
	// POSITION, so two clients looking at the same instance have to agree on
	// which colour position three is, or the same card is teal in a browser and
	// pink on a phone.
	//
	// A named list of seven, never a settings patch: this is deliberately not a
	// second door into /api/settings, which stays off the relay allowlist. A
	// caller reaching this route can repaint the instance and can do nothing
	// else - and repainting is already less than what the same caller can do
	// through the queue routes it has had all along.
	reg.Add(http.MethodPost, "/api/appearance",
		"set the instance's accent, corner shape and rainbow settings - the same seven fields GET answers with, and nothing else",
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
			writeJSON(w, map[string]any{
				"shape":           applied.Shape,
				"accent":          applied.Accent,
				"rainbow":         applied.Rainbow,
				"rainbowReactive": applied.RainbowReactive,
				"rainbowRotate":   applied.RainbowRotate,
				"rainbowSeed":     applied.RainbowSeed,
				"rainbowPalette":  applied.RainbowPalette,
			})
		})

	// The password lock. These routes stay reachable while locked out — they are
	// how you get back in.
	reg.AddOpen(http.MethodGet, "/api/auth",
		"whether a password is set and whether this client is logged in; open because the login screen asks it first",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]bool{
				"enabled":       a.Auth.Enabled(),
				"authenticated": authenticated(a, r),
			})
		})
	reg.AddOpen(http.MethodPost, "/api/auth/login", "exchange the password for a session; open because it is the way in",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if !a.Auth.Check(body.Password) {
				http.Error(w, "wrong password", http.StatusUnauthorized)
				return
			}
			setSession(w, r, a.Auth.Issue())
			writeJSON(w, map[string]bool{"enabled": true, "authenticated": true})
		})
	reg.AddOpen(http.MethodPost, "/api/auth/logout", "drop this client's session; open because logging out of an expired session must work",
		func(w http.ResponseWriter, r *http.Request) {
			clearSession(w, r)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPut, "/api/auth/password", "set, change or remove the instance password",
		func(w http.ResponseWriter, r *http.Request) {
			// Setting the first password is open (the instance is unprotected
			// anyway); changing or removing one requires the current password and an
			// existing session, which the guard enforces.
			var body struct {
				Current string `json:"current"`
				New     string `json:"new"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if err := a.Auth.SetPassword(body.Current, body.New); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// A token minted under the old password state (no password, or a
			// password this change just replaced) is a standing bypass of
			// whatever protection this call just put in place — see
			// apitoken.Store.RevokeAll's own doc comment for the live
			// reproduction this closes. Best-effort: a failed revoke must not
			// block the password change that already succeeded and is already
			// persisted.
			_ = a.APITokens.RevokeAll()
			if body.New != "" {
				setSession(w, r, a.Auth.Issue()) // don't lock out the person who just set it
			} else {
				clearSession(w, r)
			}
			writeJSON(w, map[string]bool{"enabled": a.Auth.Enabled(), "authenticated": true})
		})
}
