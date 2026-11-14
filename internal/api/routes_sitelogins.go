package api

// Sites signed into through the browser (internal/sitelogin): the accounts
// page's tiles, its Sign in, and the session the extension hands over. What
// travels back says who is signed in, never the session.
//
// GET /api/sitelogins lists every site, and begin, check, remove and the
// extension's handover answer with one, in this shape:
//
//	{"id": "patreon", "name": "Patreon", "host": "patreon.com",
//	 "loginUrl": "https://www.patreon.com/login", "domains": ["patreon.com"],
//	 "use": "always",                  // or "onNeed": only after a sign-in failure
//	 "state": "signedIn",              // signedOut, expired or unchecked
//	 "account": "Jane Doe",            // where the site names it
//	 "expires": "2027-01-01T00:00:00Z", // where the cookies say
//	 "waiting": false,                 // from begin until a session turns up
//	 "source": "browser",              // "extension", or "" without a session
//	 "browser": "Firefox (default-release)", // with source "browser" only
//	 "unreadable": ["Chrome"],         // browsers whose cookies could not be read
//	 "readsBrowsers": true}            // false in a container, which reads none
//
// GET /api/sitelogins/handover lists for the extension each site's id, name,
// host, loginUrl, domains, waiting and signIn, the names of the cookies only a
// signed-in session carries. A handover with an empty cookies list signs the
// extension's session out; a set without the site's cookies is refused (422).
//
// A session read from a browser is not stored, so remove forgets only the
// extension's; one in a browser ends with signing out there.

import (
	"errors"
	"net/http"

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
	// SignIn are the cookies only a signed-in session carries, which the
	// extension watches.
	SignIn  []string `json:"signIn"`
	Waiting bool     `json:"waiting"`
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
		"read the browsers and ask the site again who the session signs in as",
		func(w http.ResponseWriter, r *http.Request) {
			st, err := a.CheckSiteLogin(r.PathValue("id"))
			writeSiteLogin(w, st, err)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/remove",
		"forget the session the extension handed over for a site",
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
				out = append(out, handoverSite{
					ID: s.ID, Name: s.Name, Host: s.Host, LoginURL: s.LoginURL,
					Domains: s.Domains, SignIn: s.SignInCookies(), Waiting: s.Waiting,
				})
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/sitelogins/{id}/handover",
		"store the session the browser extension read from the browser for one site; no cookies at all is the browser signed out and forgets it",
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
