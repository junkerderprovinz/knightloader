package api

// Liveness, the event stream, and the password lock.

import (
	"encoding/json"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// appearanceFields is the allowlist both halves of /api/appearance are built
// from, so a field cannot be readable without being writable or the other way
// round.
var appearanceFields = []string{
	"shape", "accent", "rainbow", "rainbowReactive", "rainbowRotate", "rainbowSeed", "rainbowPalette",
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

	// The same seven fields, written. The palette lives on the instance rather
	// than per client because colours are handed out by position, and every
	// client has to agree on them. Only the named fields are applied, so this
	// is no second door into /api/settings.
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

	// The password lock. These routes stay reachable while locked out, since
	// they are how you get back in.
	reg.AddOpen(http.MethodGet, "/api/auth",
		"whether a password is set and whether this client is logged in; open because the login screen asks it first",
		func(w http.ResponseWriter, r *http.Request) {
			in := authenticated(a, r)
			out := map[string]any{
				"enabled":       a.Auth.Enabled(),
				"authenticated": in,
			}
			// An anonymous caller learns only whether a password is set; the
			// second factor and the recovery codes left are security
			// configuration, for a session or a token that may administer.
			if permits(a, r, apitoken.ScopeAdmin) {
				out["twoFactor"] = a.Auth.TwoFactorEnabled()
				out["recoveryLeft"] = a.Auth.RecoveryLeft()
			}
			writeJSON(w, out)
		})
	// The login throttle lives as long as the handler; routes_twofactor.go says
	// why a second factor needs one.
	gate := newLoginGate()
	reg.AddOpen(http.MethodPost, "/api/auth/login", "exchange the password (and a second-factor code, when one is armed) for a session; open because it is the way in",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
				// Code is an authenticator code or a recovery code, ignored when
				// no second factor is armed.
				Code string `json:"code"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if gate.blocked(r) {
				http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
				return
			}
			if !a.Auth.Check(body.Password) {
				gate.fail(r)
				// The same answer on every instance, so it does not reveal
				// whether a second factor exists.
				http.Error(w, "wrong password", http.StatusUnauthorized)
				return
			}
			if a.Auth.TwoFactorEnabled() {
				if body.Code == "" {
					// The password was right and the screen now asks for the
					// code. Not counted by the throttle: every honest login
					// passes here once, and only with the right password.
					writeJSON(w, map[string]any{
						"enabled": true, "authenticated": false, "twoFactorRequired": true,
					})
					return
				}
				if !a.Auth.CheckSecond(body.Code) {
					gate.fail(r)
					// codeRejected tells "ask for a code" apart from "the code
					// was wrong".
					writeJSONStatus(w, http.StatusUnauthorized, map[string]any{
						"enabled": true, "authenticated": false,
						"twoFactorRequired": true, "codeRejected": true,
					})
					return
				}
			}
			gate.pass(r)
			setSession(w, r, a.Auth.Issue())
			writeJSON(w, map[string]any{"enabled": true, "authenticated": true})
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
			// A token minted under the old password state would bypass the new
			// one. Best effort: a failed revoke must not undo a password change
			// that is already persisted.
			_ = a.APITokens.RevokeAll()
			if body.New != "" {
				setSession(w, r, a.Auth.Issue()) // don't lock out the person who just set it
			} else {
				clearSession(w, r)
			}
			writeJSON(w, map[string]bool{"enabled": a.Auth.Enabled(), "authenticated": true})
		})
}
