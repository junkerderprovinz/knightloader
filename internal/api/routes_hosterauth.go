package api

// Native hoster logins: the host list, per-host username and password, and the
// three-way sync status against the headless-JD sidecar (internal/hosterauth).
// Never returns a credential, only whether one is set and what JD says about
// it; hosterauth.LoginState has no password field, the same state-never-secrets
// rule registerAccounts follows.

import (
	"context"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerHosterAuth(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/hosterauth/hosts", "hosts the 'add a login' picker offers - JD's own list, or a curated fallback",
		func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			writeJSON(w, a.HosterHosts(ctx))
		})

	reg.Add(http.MethodGet, "/api/hosterauth/logins", "every stored native hoster login and its sync status against JD - never the password",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.HosterLogins())
		})

	// The password reaches the JD sidecar and is stored there, which the
	// dialogue has to say before it calls this
	// (web/src/components/HosterLoginSection.tsx). That disclosure is why
	// these logins are not folded into /api/accounts.
	reg.Add(http.MethodPost, "/api/hosterauth/logins", "store or update one host's native login and reconcile it into JD",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host     string `json:"host"`
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Host == "" {
				http.Error(w, "which host is this login for?", http.StatusBadRequest)
				return
			}
			if err := a.SetHosterLogin(body.Host, body.Username, body.Password); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	// Its own route rather than a field on the save above, so flipping the
	// switch does not make the page re-send the credential.
	reg.Add(http.MethodPost, "/api/hosterauth/logins/enabled", "switch one host's stored login on or off without deleting it",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host    string `json:"host"`
				Enabled bool   `json:"enabled"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Host == "" {
				http.Error(w, "which host?", http.StatusBadRequest)
				return
			}
			if err := a.SetHosterLoginEnabled(body.Host, body.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/hosterauth/logins/remove", "remove one host's stored native login",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host string `json:"host"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Host == "" {
				http.Error(w, "which host?", http.StatusBadRequest)
				return
			}
			if err := a.RemoveHosterLogin(body.Host); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
