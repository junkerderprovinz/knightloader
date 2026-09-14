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
	// THE THIRD FIELD WAS ADDED DELIBERATELY, which is what
	// TestTheOldHealthRouteIsUntouched asks for in writing: "adding one is
	// safe, and this is the place to decide that deliberately". `status` and
	// `version` are unchanged and stay unchanged - the phone app compares the
	// literal "ok", the container HEALTHCHECK reads the exit code, and the
	// Click'n'Load bridge refuses to start on anything else.
	//
	// `commit` is the source revision, and it is ALWAYS PRESENT AND SOMETIMES
	// EMPTY rather than omitted when unknown: a caller that has to distinguish
	// "this server is too old to tell me" from "this build does not know its
	// own revision" can do neither if the key comes and goes. buildinfo.Revision
	// carries the full account of where the string comes from and why it may be
	// empty.
	reg.AddOpen(http.MethodGet, "/api/health",
		"liveness, the running version and the commit it was built from; open so a container orchestrator can probe a locked instance",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]string{"status": "ok", "version": buildinfo.Version, "commit": buildinfo.Revision()})
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
			in := authenticated(a, r)
			out := map[string]any{
				"enabled":       a.Auth.Enabled(),
				"authenticated": in,
			}
			// What defences this instance has is for somebody already inside.
			// An anonymous caller learns only whether a password is set, which
			// the login screen has to be told anyway; whether there is a second
			// factor on top of it, and how much of the recovery sheet is left,
			// are answers only the settings card needs and only a session gets.
			if in {
				out["twoFactor"] = a.Auth.TwoFactorEnabled()
				out["recoveryLeft"] = a.Auth.RecoveryLeft()
			}
			writeJSON(w, out)
		})
	// The throttle in front of the login. Built here so it lives as long as the
	// handler does - see routes_twofactor.go for why a second factor cannot
	// ship without one.
	gate := newLoginGate()
	reg.AddOpen(http.MethodPost, "/api/auth/login", "exchange the password (and a second-factor code, when one is armed) for a session; open because it is the way in",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
				// Code is a six-digit code from the authenticator app, or one of
				// the recovery codes. Ignored on an instance with no second
				// factor, so a client that always sends the field is fine.
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
				// Deliberately says nothing about whether a second factor
				// exists: the answer to a wrong password is the same on every
				// instance, so it cannot be used to survey which ones are worth
				// coming back to.
				http.Error(w, "wrong password", http.StatusUnauthorized)
				return
			}
			if a.Auth.TwoFactorEnabled() {
				if body.Code == "" {
					// Not a refusal - the first half worked and the server is
					// asking for the second. 200, no session, and a flag the
					// screen reads to know which step it is on.
					//
					// Deliberately NOT counted by the throttle. The sign-in
					// screen sends the password first and the code second, so
					// every honest login passes through here exactly once;
					// counting it would spend an eighth of the burst on being
					// asked a question, and it can only be reached by somebody
					// who already has the password.
					writeJSON(w, map[string]any{
						"enabled": true, "authenticated": false, "twoFactorRequired": true,
					})
					return
				}
				if !a.Auth.CheckSecond(body.Code) {
					gate.fail(r)
					// codeRejected is what separates "the screen should ask for
					// a code" from "the code it asked for was wrong". Without
					// it the sign-in screen can only show one of those two
					// states, and a mistyped digit reads as the question being
					// asked again for no reason.
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
