package api

// Named, individually revocable API tokens. See internal/apitoken for why this
// is a second, hashed store rather than a second password. Every route here
// needs an existing session or token, so the first token is issued from the
// logged-in web UI, the same as setting the password.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerTokens(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/tokens", "every named API token issued for this instance, never the secret itself",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.APITokens.List())
		})

	reg.Add(http.MethodPost, "/api/tokens",
		"issue a new named token; the secret is in this one response and nowhere else, ever again",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Name string `json:"name"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			tok, secret, err := a.APITokens.Create(body.Name)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, apitoken.ErrEmptyName) || errors.Is(err, apitoken.ErrTooMany) {
					status = http.StatusBadRequest
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSONStatus(w, http.StatusCreated, newTokenResponse{Token: tok, Secret: secret})
		})

	reg.Add(http.MethodDelete, "/api/tokens/{id}",
		"revoke one token by id; every other token and the shared password are untouched",
		func(w http.ResponseWriter, r *http.Request) {
			err := a.APITokens.Revoke(r.PathValue("id"))
			switch {
			case err == nil:
				w.WriteHeader(http.StatusNoContent)
			case errors.Is(err, apitoken.ErrNotFound):
				http.Error(w, err.Error(), http.StatusNotFound)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		})
}

// newTokenResponse is Create's one-time answer: the metadata shape
// GET /api/tokens lists, plus the secret this instance cannot show again once
// this response is sent.
type newTokenResponse struct {
	apitoken.Token
	Secret string `json:"secret"`
}
