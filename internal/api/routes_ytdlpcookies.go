package api

// The yt-dlp cookie jars over HTTP: which sites have one, storing one and
// removing one. Only host names travel back; a cookies.txt is a live session,
// so the paste that stores it is the only way it crosses the wire (see
// internal/resolver/ytdlp/cookies.go).

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

func registerYtdlpCookies(reg *Registry, a *app.App) {
	// Over the app's own *accounts.Store; a second store on the same file would
	// keep its own snapshot and overwrite the other's writes (see
	// NewCookieStore). yt-dlp reads through the same store on every spawn, so
	// a saved jar needs no rewire.
	jars := ytdlp.NewCookieStore(a.Accounts)

	reg.Add(http.MethodGet, "/api/ytdlp/cookies",
		"which sites have a stored sign-in cookies.txt - the host names only, never the jar",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, cookieJarHosts(jars))
		})

	reg.Add(http.MethodPost, "/api/ytdlp/cookies",
		"store or replace one site's sign-in cookies.txt; an empty text clears it",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host string `json:"host"`
				// Text is a pointer because empty clears the jar (as in
				// CookieStore.Set) while absent is refused: the page can never
				// read a jar back, so a form re-saved without it must not
				// delete a working session.
				Text *string `json:"text"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			host := cookieJarHost(body.Host)
			if host == "" {
				http.Error(w, cookieHostHelp, http.StatusBadRequest)
				return
			}
			if body.Text == nil {
				http.Error(w, "text: there is no cookies.txt in this request. A stored jar is never sent "+
					"back to the page, so leaving the field out cannot mean \"keep the one you have\": send the "+
					"exported file's contents, or an empty text to clear this site's jar.", http.StatusBadRequest)
				return
			}
			if err := jars.Set(host, *body.Text); err != nil {
				// Names the host but nothing of the text, which could end up in
				// a public bug report.
				http.Error(w, "host "+host+": the cookies.txt could not be sealed into the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable, then save it again.",
					http.StatusInternalServerError)
				return
			}
			writeJSON(w, cookieJarHosts(jars))
		})

	// A POST with the host in the body rather than a DELETE with it in the
	// path, like POST /api/hosterauth/logins/remove, since a typed host raises
	// encoding questions in a path segment.
	reg.Add(http.MethodPost, "/api/ytdlp/cookies/remove",
		"delete one site's stored sign-in cookies.txt",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Host string `json:"host"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			host := cookieJarHost(body.Host)
			if host == "" {
				http.Error(w, cookieHostHelp, http.StatusBadRequest)
				return
			}
			// A 404 for an unknown host, so a typo cannot look like a removed
			// session.
			if !slices.Contains(jars.Hosts(), host) {
				http.Error(w, "host: nothing is stored for "+host+". The sites that have a jar are the ones "+
					"the list shows, and a jar saved for a parent domain is removed under that name "+
					"(youtube.com), not under the subdomain that used it.", http.StatusNotFound)
				return
			}
			if err := jars.Remove(host); err != nil {
				http.Error(w, "host "+host+": the cookies.txt could not be removed from the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable, then try again.",
					http.StatusInternalServerError)
				return
			}
			writeJSON(w, cookieJarHosts(jars))
		})
}

// cookieHostHelp is the answer both writes give a host they cannot use.
const cookieHostHelp = "host: which site is this cookies.txt for? Send the address of the page you " +
	"exported it from (https://www.youtube.com/watch...) or the site's own name (youtube.com). " +
	"A path, a port or a query string is not a name any lookup will ever match."

// cookieJarHosts is the listing all three routes answer with: the hosts that
// have a jar, re-read from the store so a write shows the normalised host.
// Nothing else about a jar is listed, since any extra field could carry part
// of a session. It is never nil, so a fresh install answers [].
func cookieJarHosts(jars *ytdlp.CookieStore) []string {
	hosts := jars.Hosts()
	if hosts == nil {
		return []string{}
	}
	return hosts
}

// cookieJarHost is the key a jar has to be filed under to be found again,
// worked out from what a person typed. It follows hostOf in internal/app and
// the lower-casing and "www." strip of cookieHostChain, so a jar is never
// stored under a key no lookup uses.
//
// An address yields its Hostname; a bare name like "youtube.com" parses as a
// path, so it is normalised directly. Anything that cannot be a host returns
// "", which the caller refuses, since accounts.Store would otherwise seal a
// key nothing ever matches.
func cookieJarHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	}
	host := strings.TrimPrefix(strings.ToLower(raw), "www.")
	// A typed name still carrying a path, port, query or space is a paste
	// that lost its scheme. IPv6 colons only come through the branch above.
	if strings.ContainsAny(host, "/?#@: \t") {
		return ""
	}
	return host
}
