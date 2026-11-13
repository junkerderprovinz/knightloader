package api

// Sites signed into through the browser (internal/sitelogin): the accounts
// page's tiles, its Sign in, a pasted cookies.txt, and the session the
// extension hands over. What travels back says who is signed in, never the
// session.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// handoverSite is what the extension learns of a site: where to read the
// session and whether this instance is waiting for it. How a stored session
// stands is the owner's to read.
type handoverSite struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	LoginURL string   `json:"loginUrl"`
	Domains  []string `json:"domains"`
	Waiting  bool     `json:"waiting"`
}

func registerSiteLogins(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/sitelogins",
		"the sites signed into through the browser and who each session signs in as - never the session",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.SiteLogins())
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/begin",
		"wait for the session the extension hands over once the user signs in to the site in the browser",
		func(w http.ResponseWriter, r *http.Request) {
			st, err := a.BeginSiteLogin(r.PathValue("id"))
			writeSiteLogin(w, st, err)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/check",
		"ask the site again who the stored session signs in as",
		func(w http.ResponseWriter, r *http.Request) {
			st, err := a.CheckSiteLogin(r.PathValue("id"))
			writeSiteLogin(w, st, err)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/cookies",
		"store a pasted cookies.txt as the site's session",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Text *string `json:"text"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Signing out is its own route, so a form sent without its text
			// never ends a working session.
			if body.Text == nil || strings.TrimSpace(*body.Text) == "" {
				http.Error(w, "text: there is no cookies.txt in this request. Paste the file exported from a "+
					"browser signed in to the site; to sign out, use the remove route.", http.StatusBadRequest)
				return
			}
			st, err := a.StoreSiteSession(r.PathValue("id"), *body.Text)
			writeSiteLogin(w, st, err)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/remove",
		"sign out of a site by forgetting its stored session",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if err := a.RemoveSiteLogin(id); err != nil {
				writeSiteLoginError(w, err)
				return
			}
			st, err := a.CheckSiteLogin(id)
			writeSiteLogin(w, st, err)
		})

	reg.Add(http.MethodGet, "/api/sitelogins/handover",
		"the sites the browser extension can hand a session over for, and which this instance waits for",
		func(w http.ResponseWriter, r *http.Request) {
			states := a.SiteLogins()
			out := make([]handoverSite, 0, len(states))
			for _, s := range states {
				out = append(out, handoverSite{s.ID, s.Name, s.Host, s.LoginURL, s.Domains, s.Waiting})
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/handover",
		"store the session the browser extension read from the browser for one site",
		func(w http.ResponseWriter, r *http.Request) {
			// The group's phones and instances hold the words too, and none of
			// them signs in anywhere on the user's behalf.
			if fromRelayGroup(r) {
				if c, ok := federation.CallerOf(r.Context()); !ok || c.Kind != relay.KindExtension {
					writeRefusal(w, http.StatusForbidden, "handoverNotFromExtension",
						"only the browser extension hands a site's session over", nil)
					return
				}
			}
			var body struct {
				Cookies []sitelogin.BrowserCookie `json:"cookies"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			st, err := a.HandOverSiteSession(r.PathValue("id"), body.Cookies)
			writeSiteLogin(w, st, err)
		})
}

func writeSiteLogin(w http.ResponseWriter, st app.SiteLoginState, err error) {
	if err != nil {
		writeSiteLoginError(w, err)
		return
	}
	writeJSON(w, st)
}

// writeSiteLoginError answers in words that carry nothing of a session.
func writeSiteLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrNoSuchSite):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, app.ErrNoSiteSession):
		http.Error(w, err.Error()+": sign in to the site in the browser first, then hand it over again.",
			http.StatusUnprocessableEntity)
	default:
		http.Error(w, "the session could not be sealed into the credential store. Check that "+
			"KnightLoader's data directory is writable, then try again.", http.StatusInternalServerError)
	}
}
