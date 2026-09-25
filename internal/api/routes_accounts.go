package api

// Accounts: the service catalogue, which accounts are configured, and what a
// live check said. The list is state, never secrets - a stored credential is
// never sent back over this API, only whether one is set and where it came
// from.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// credentialBody is the wire shape of a secret coming in. Which fields matter
// follows from the service's catalogue Kind; like accounts.Credential, the
// handler does not enforce it.
type credentialBody struct {
	Service  string `json:"service"`
	Account  string `json:"account"`
	APIKey   string `json:"apiKey"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (b credentialBody) credential() accounts.Credential {
	return accounts.Credential{APIKey: b.APIKey, Username: b.Username, Password: b.Password}
}

func registerAccounts(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/accounts", "every configured account - never the credential itself",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.AccountStates())
		})

	// The catalogue is what the "new account" picker searches: every service
	// KnightLoader knows how to store a credential for, configured or not.
	reg.Add(http.MethodGet, "/api/accounts/catalogue", "every service KnightLoader can store a credential for",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, accounts.Catalogue)
		})

	// The "new account" dialogue calls this before Save, so a typo shows up
	// before it is persisted rather than on the first download. Whether to
	// save anyway is the frontend's choice; this only reports what it found.
	reg.Add(http.MethodPost, "/api/accounts/verify", "check a credential against its service without storing it",
		func(w http.ResponseWriter, r *http.Request) {
			var body credentialBody
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which service is this credential for?", http.StatusBadRequest)
				return
			}
			ok, hosts, detail := a.VerifyCredential(body.Service, body.credential())
			writeJSON(w, struct {
				OK     bool   `json:"ok"`
				Hosts  int    `json:"hosts"`
				Detail string `json:"detail"`
			}{ok, hosts, detail})
		})

	// Re-checks an already-stored account - the per-row "Refresh".
	reg.Add(http.MethodPost, "/api/accounts/test", "ask the service whether a stored account's credential still works",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Service string `json:"service"`
				Account string `json:"account"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which service is this account for?", http.StatusBadRequest)
				return
			}
			writeJSON(w, a.TestAccount(body.Service, body.Account))
		})

	// A zero credential (every field empty) clears the account, the same
	// convention accounts.Store.Set uses for a bare secret.
	reg.Add(http.MethodPost, "/api/accounts", "store or clear one account's credential",
		func(w http.ResponseWriter, r *http.Request) {
			var body credentialBody
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which service is this credential for?", http.StatusBadRequest)
				return
			}
			if err := a.SetAccountCredential(body.Service, body.Account, body.credential()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	// Kept apart from the credential endpoint so that renaming an account
	// cannot clear its credential through an empty secret field.
	reg.Add(http.MethodPost, "/api/accounts/label", "rename one account's display label",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Service string `json:"service"`
				Account string `json:"account"`
				Label   string `json:"label"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which account is this label for?", http.StatusBadRequest)
				return
			}
			a.SetAccountLabel(body.Service, body.Account, body.Label)
			w.WriteHeader(http.StatusNoContent)
		})

	// Switches whether one account participates in routing - gates
	// rewireBackends exactly as a missing credential does, for an account
	// stored here or supplied by the container alike.
	reg.Add(http.MethodPost, "/api/accounts/enabled", "switch whether one account participates in routing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Service string `json:"service"`
				Account string `json:"account"`
				Enabled bool   `json:"enabled"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which account is this for?", http.StatusBadRequest)
				return
			}
			a.SetAccountEnabled(body.Service, body.Account, body.Enabled)
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/accounts/import", "switch whether what is added to one debrid account elsewhere is imported",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Service string `json:"service"`
				Account string `json:"account"`
				Import  bool   `json:"import"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Service == "" {
				http.Error(w, "which account is this for?", http.StatusBadRequest)
				return
			}
			a.SetAccountImport(body.Service, body.Account, body.Import)
			w.WriteHeader(http.StatusNoContent)
		})
}
