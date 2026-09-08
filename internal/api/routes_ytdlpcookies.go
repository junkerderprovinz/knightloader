package api

// The yt-dlp cookie jars over HTTP: which sites have one, putting one there,
// taking one away. Names in both directions and never the jar itself, which is
// the same rule internal/resolver/ytdlp/cookies.go states for the store this
// wraps - a cookies.txt is a live session, so the only leg it ever travels is
// the paste that stores it.
//
// It exists because the switch shipped ahead of the door. The Resolvers page
// already offers "use stored sign-in cookies" and says so in its own comment
// ("this build registers no route that puts one there"), so the feature was
// switchable and had nothing to read. The single existing way in was the
// generic POST /api/accounts with service "ytdlpcookies", which no interface
// offers and the account catalogue does not list; worse, its account field is
// the host, so getting that field wrong files a jar under a key
// CookieStore.Hosts never lists and CookieStore.Text never looks up, with no
// error anywhere.

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

func registerYtdlpCookies(reg *Registry, a *app.App) {
	// One CookieStore over the app's OWN *accounts.Store, never a second store
	// opened on the same directory: two accounts.Store instances over one
	// accounts.json each keep their own snapshot of the whole file, so the
	// second to write erases what the first had just saved (see NewCookieStore's
	// doc comment, which documents that trap for exactly this reason). Wrapping
	// the same pointer costs nothing - a CookieStore holds that pointer and
	// nothing else.
	//
	// Nothing below re-arms the backend after a write, and that is not an
	// omission: rewireBackends hands yt-dlp a closure over this same store
	// (yb.Cookies = ytdlp.NewCookieStore(a.Accounts).Text, app_accounts.go), so
	// a jar saved between two downloads is read on the next spawn without a
	// restart and without a rewire.
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
				// A pointer, because absent and empty have to mean different
				// things here and only one of them may be acted on. An empty
				// text is the deliberate clear, which is CookieStore.Set's own
				// established contract. An ABSENT text is a caller that sent no
				// jar at all - and this is the one store the page it came from
				// can never read back, so a form re-saved without the textarea
				// refilled would silently delete a working session, and the
				// download that then fails looks like the site changing its
				// mind rather than like this endpoint. Refused below rather
				// than read as "no opinion, leave the stored one alone",
				// because a POST that answers 200 and stores nothing is the
				// worse of the two lies.
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
				// The host and the operation, never a word about the text: this
				// sentence reaches a person through the page and a copy of it
				// can reach a public bug report, which is the rule cookies.go
				// holds every one of its own error paths to.
				http.Error(w, "host "+host+": the cookies.txt could not be sealed into the credential store ("+
					err.Error()+"). Check that KnightLoader's data directory is writable, then save it again.",
					http.StatusInternalServerError)
				return
			}
			writeJSON(w, cookieJarHosts(jars))
		})

	// Its own route rather than a DELETE with the host in the path, matching
	// POST /api/hosterauth/logins/remove next door: a host is a dotted name a
	// person typed, not an opaque id, and putting it in a path segment invites
	// exactly the encoding questions that the body already answers.
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
			// A host with nothing stored is a 404 and not a cheerful 204, the
			// same answer DELETE /api/tokens/{id} gives an unknown id and for a
			// sharper version of the same reason: somebody removing a jar is
			// removing their own logged-in session from this machine, and a
			// success on a name that stored nothing would leave them believing a
			// live session is gone while it is still sealed under the name they
			// meant to type.
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

// cookieHostHelp is the one sentence both writes answer a host they cannot use
// with. Shared rather than written twice, so the advice cannot drift into two
// versions that describe different fields.
const cookieHostHelp = "host: which site is this cookies.txt for? Send the address of the page you " +
	"exported it from (https://www.youtube.com/watch...) or the site's own name (youtube.com). " +
	"A path, a port or a query string is not a name any lookup will ever match."

// cookieJarHosts is the listing all three routes answer with: the hosts that
// have a jar, and nothing else about them.
//
// Every write answers it too, re-read from the store rather than echoed from
// the request, for the reason POST /api/resolvers/priority states next door:
// what was stored is not necessarily what was sent (the host is normalised on
// the way in), so a page redrawing from what it SENT would show a row the
// downloader will never match.
//
// A bare list of names is the whole shape ON PURPOSE. No size, no age, no
// first line, no preview: a cookies.txt is a live session, and every field a
// richer row would grow is another place a piece of one can end up in the
// diagnostics bundle (routes_diagnostics.go) that people attach to public bug
// reports.
//
// Never nil. encoding/json writes a nil slice as null, and CookieStore.Hosts
// answers nil for an install that has stored nothing yet, so a page mapping
// over the answer would throw on a fresh instance rather than draw an empty
// table.
func cookieJarHosts(jars *ytdlp.CookieStore) []string {
	hosts := jars.Hosts()
	if hosts == nil {
		return []string{}
	}
	return hosts
}

// cookieJarHost is the key a jar has to be filed under to ever be read again,
// worked out from whatever a person typed into one field.
//
// It applies hostOf's own expression (internal/app/app.go) rather than a
// second rule invented here, because hostOf is what every lookup has already
// applied by the time this store is asked: a task's Host is hostOf(t.URL),
// HosterPresetFor is reached with that, and CookieStore.Text walks
// cookieHostChain, which lower-cases and strips "www." before it looks
// anything up. So a jar filed as "www.youtube.com" is a jar that is stored,
// listed on the page, and never once read - the feature failing in the one way
// that is indistinguishable from the site winning.
//
// The two branches are hostOf's two branches. An address parses and hands back
// its Hostname; a bare site name does not, because url.Parse reads
// "youtube.com" as a path and answers an empty Hostname with no error at all.
// hostOf returns its input untouched in that second case, since its input is
// always a URL and a non-URL there is a scheduling bucket rather than a host;
// here the second case is the ordinary one (it is what the field asks for), so
// the same lower-case and "www." strip is applied to it. That is idempotent
// with what CookieStore.Set does to the same string on the way in, which is
// what lets the caller's key, the stored key and the listed key all be the one
// string.
//
// "" is the answer for anything that cannot be a host, and the caller turns
// that into the 400 above. accounts.Store validates nothing by design, so
// "youtube.com/watch?v=x" - what a person pasting an address without its
// scheme leaves behind - would otherwise be sealed, listed as though it were a
// site, and matched by nothing: this refusal is the only moment anybody can be
// told.
func cookieJarHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	}
	host := strings.TrimPrefix(strings.ToLower(raw), "www.")
	// Only on this branch. What u.Hostname() answered above is by construction
	// what a lookup produces, IPv6 colons included; what a person typed is not,
	// and a name still carrying a path, a port, a query or a space is a paste
	// that lost its scheme rather than a site.
	if strings.ContainsAny(host, "/?#@: \t") {
		return ""
	}
	return host
}
