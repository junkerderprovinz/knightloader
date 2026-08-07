package api

// Credentials: which slots are filled, and what the service said when asked.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerAccounts(reg *Registry, a *app.App) {
	// The account list is state, never secrets: which slots are filled, where the
	// value comes from, and what a test said.
	reg.Add(http.MethodGet, "/api/accounts", "every credential slot and whether it is filled; never the credential itself",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.AccountStates())
		})
	reg.Add(http.MethodPost, "/api/accounts/{service}/test", "ask the service whether the stored credential works",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.TestAccount(r.PathValue("service")))
		})
	reg.Add(http.MethodPost, "/api/accounts", "store or clear a credential; an empty secret clears it",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Service string `json:"service"`
				Secret  string `json:"secret"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which service is this credential for?", http.StatusBadRequest)
				return
			}
			if err := a.SetAccount(body.Service, body.Secret); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
