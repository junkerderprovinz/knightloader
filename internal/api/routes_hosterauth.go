package api

// Native hoster logins: KL's own host list, per-host username/password and
// the three-way sync status against the headless-JD sidecar - see
// internal/hosterauth. Never returns a credential, only whether one is set
// and what JD currently says about it (hosterauth.LoginState carries no
// password field at all, the same "state, never secrets" rule
// registerAccounts already states for the debrid/apiKey accounts).

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

	// Stores or updates one host's native login. The dialogue calling this
	// must have already told the user, before this fires, that the password
	// is sent to and stored by the JD sidecar - that disclosure is the
	// frontend's job (see web/src/components/HosterLoginSection.tsx), not
	// something this endpoint can enforce, but it is the reason this route
	// exists at all rather than being folded into /api/accounts.
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
