package api

// Liveness, the event stream, and the password lock.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

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
