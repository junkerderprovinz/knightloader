package api

// What each route lets an API token do. A browser session, a sibling on the
// relay and an instance with no password hold every right; only a token is
// narrowed, so that the key typed into Sonarr can add and read without being
// able to read the accounts or change the password.
//
// The table names every guarded route but forwardPattern, and a test fails
// for a route missing from it, so a new route cannot be reached with a token
// until somebody has decided which right it needs. Until then it needs admin.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// forwardPattern hands a call on to a peer. It needs no right of its own: the
// peer takes the call on this instance's full token, so the handler holds a
// token to what the forwarded call would need here (Registry.scopeOfCall).
const forwardPattern = "/api/instances/{name}/{rest...}"

// folderRoutes are the routes whose body can pick the folder downloads go to.
// Where files land on the host is configuration, so a body that picks one
// needs admin, whatever the route needs otherwise.
var folderRoutes = map[string]bool{
	"POST /api/links":         true,
	"POST /api/tasks/options": true,
}

// routeScopes is the right each guarded route needs, keyed by its pattern.
// Read shows what the instance is doing and has done, add puts new work in,
// control acts on the work that is there, and admin is everything that shows
// or changes configuration and secrets.
var routeScopes = map[string]apitoken.Scope{
	"GET /api/accounts":           apitoken.ScopeAdmin,
	"POST /api/accounts":          apitoken.ScopeAdmin,
	"GET /api/accounts/catalogue": apitoken.ScopeAdmin,
	"POST /api/accounts/enabled":  apitoken.ScopeAdmin,
	"POST /api/accounts/import":   apitoken.ScopeAdmin,
	"POST /api/accounts/label":    apitoken.ScopeAdmin,
	"POST /api/accounts/test":     apitoken.ScopeAdmin,
	"POST /api/accounts/verify":   apitoken.ScopeAdmin,

	"POST /api/activity/{kind}/abort": apitoken.ScopeControl,

	// A client reads the look to match it, and the phone app edits the
	// palette. The seven fields are cosmetic, less than control already does
	// to the queue.
	"GET /api/appearance":  apitoken.ScopeRead,
	"POST /api/appearance": apitoken.ScopeControl,

	"POST /api/auth/2fa/begin":               apitoken.ScopeAdmin,
	"POST /api/auth/2fa/confirm":             apitoken.ScopeAdmin,
	"POST /api/auth/2fa/disable":             apitoken.ScopeAdmin,
	"POST /api/auth/passkey/register/begin":  apitoken.ScopeAdmin,
	"POST /api/auth/passkey/register/finish": apitoken.ScopeAdmin,
	"DELETE /api/auth/passkeys/{id}":         apitoken.ScopeAdmin,
	"PATCH /api/auth/passkeys/{id}":          apitoken.ScopeAdmin,
	"PUT /api/auth/password":                 apitoken.ScopeAdmin,

	"GET /api/browser-extension.xpi":     apitoken.ScopeRead,
	"GET /api/browser-extension.zip":     apitoken.ScopeRead,
	"GET /api/browser-extension/version": apitoken.ScopeRead,

	"GET /api/captcha":              apitoken.ScopeRead,
	"POST /api/captcha/refresh":     apitoken.ScopeControl,
	"POST /api/captcha/{id}/answer": apitoken.ScopeControl,
	"POST /api/captcha/{id}/skip":   apitoken.ScopeControl,
	"GET /api/captcha/{id}/widget":  apitoken.ScopeRead,

	"GET /api/cleanup/{class}":             apitoken.ScopeRead,
	"POST /api/cleanup/{class}":            apitoken.ScopeControl,
	"DELETE /api/collector/filtered":       apitoken.ScopeControl,
	"GET /api/collector/filtered":          apitoken.ScopeRead,
	"POST /api/collector/filtered/restore": apitoken.ScopeControl,
	"DELETE /api/collector/skipped":        apitoken.ScopeControl,
	"GET /api/collector/skipped":           apitoken.ScopeRead,

	"DELETE /api/connect":          apitoken.ScopeAdmin,
	"GET /api/connect":             apitoken.ScopeAdmin,
	"POST /api/connect/activate":   apitoken.ScopeAdmin,
	"POST /api/connect/join":       apitoken.ScopeAdmin,
	"POST /api/connect/reveal":     apitoken.ScopeAdmin,
	"POST /api/connections/import": apitoken.ScopeAdmin,
	"POST /api/connections/test":   apitoken.ScopeAdmin,

	"POST /api/containers": apitoken.ScopeAdd,

	// The log names hosts, folders and addresses, and the bundle carries the
	// settings.
	"GET /api/diagnostics":               apitoken.ScopeAdmin,
	"GET /api/diagnostics/log":           apitoken.ScopeAdmin,
	"GET /api/diagnostics/logfile":       apitoken.ScopeAdmin,
	"GET /api/diagnostics/logfile/{gen}": apitoken.ScopeAdmin,
	"POST /api/diagnostics/startup":      apitoken.ScopeAdmin,
	"GET /api/diagnostics/task/{id}":     apitoken.ScopeAdmin,
	"GET /api/discovery":                 apitoken.ScopeAdmin,
	"GET /api/diskspace":                 apitoken.ScopeRead,

	// An event program is code this instance runs, as a script is.
	"GET /api/eventprograms":              apitoken.ScopeAdmin,
	"GET /api/eventprograms/placeholders": apitoken.ScopeAdmin,

	"GET /api/eventtargets":              apitoken.ScopeAdmin,
	"GET /api/eventtargets/placeholders": apitoken.ScopeAdmin,
	"POST /api/eventtargets/test":        apitoken.ScopeAdmin,

	"GET /api/extract":                    apitoken.ScopeRead,
	"POST /api/extract/start":             apitoken.ScopeControl,
	"POST /api/extract/{id}/abort":        apitoken.ScopeControl,
	"GET /api/features":                   apitoken.ScopeAdmin,
	"PUT /api/features/{id}":              apitoken.ScopeAdmin,
	"GET /api/feeds":                      apitoken.ScopeAdmin,
	"POST /api/feeds/test":                apitoken.ScopeAdmin,
	"GET /api/fileowner":                  apitoken.ScopeAdmin,
	"POST /api/fileowner/check":           apitoken.ScopeAdmin,
	"GET /api/folders":                    apitoken.ScopeAdmin,
	"POST /api/folders":                   apitoken.ScopeAdmin,
	"GET /api/health/detail":              apitoken.ScopeRead,
	"GET /api/help":                       apitoken.ScopeRead,
	"DELETE /api/history":                 apitoken.ScopeControl,
	"GET /api/history":                    apitoken.ScopeRead,
	"GET /api/hosterauth/hosts":           apitoken.ScopeAdmin,
	"GET /api/hosterauth/logins":          apitoken.ScopeAdmin,
	"POST /api/hosterauth/logins":         apitoken.ScopeAdmin,
	"POST /api/hosterauth/logins/enabled": apitoken.ScopeAdmin,
	"POST /api/hosterauth/logins/remove":  apitoken.ScopeAdmin,
	"GET /api/hosters/icon":               apitoken.ScopeRead,
	"GET /api/hostheaders":                apitoken.ScopeAdmin,
	"POST /api/hostheaders":               apitoken.ScopeAdmin,
	"DELETE /api/hostheaders/{id}":        apitoken.ScopeAdmin,

	// Calling off a countdown is a queue verb; checking or running the command
	// shows and executes configuration.
	"GET /api/idle-action":         apitoken.ScopeRead,
	"GET /api/idle-action/actions": apitoken.ScopeAdmin,
	"POST /api/idle-action/cancel": apitoken.ScopeControl,
	"POST /api/idle-action/check":  apitoken.ScopeAdmin,
	"POST /api/idle-action/run":    apitoken.ScopeAdmin,

	// Listing the peers is what a client needs to show their downloads.
	"GET /api/instances":           apitoken.ScopeRead,
	"POST /api/instances":          apitoken.ScopeAdmin,
	"DELETE /api/instances/{name}": apitoken.ScopeAdmin,

	"POST /api/links": apitoken.ScopeAdd,

	"GET /api/mediahooks":               apitoken.ScopeAdmin,
	"POST /api/mediahooks":              apitoken.ScopeAdmin,
	"DELETE /api/mediahooks/{id}":       apitoken.ScopeAdmin,
	"POST /api/mediahooks/{id}/test":    apitoken.ScopeAdmin,
	"GET /api/mediatools":               apitoken.ScopeAdmin,
	"GET /api/mediatools/ytdlp/latest":  apitoken.ScopeAdmin,
	"POST /api/mediatools/ytdlp/revert": apitoken.ScopeAdmin,
	"POST /api/mediatools/ytdlp/update": apitoken.ScopeAdmin,
	"GET /api/metrics":                  apitoken.ScopeRead,
	"GET /api/options":                  apitoken.ScopeAdmin,

	"GET /api/queue":            apitoken.ScopeRead,
	"POST /api/queue":           apitoken.ScopeControl,
	"GET /api/queue/counters":   apitoken.ScopeRead,
	"POST /api/queue/enabled":   apitoken.ScopeControl,
	"POST /api/queue/force":     apitoken.ScopeControl,
	"POST /api/queue/move":      apitoken.ScopeControl,
	"GET /api/queue/priorities": apitoken.ScopeRead,
	"POST /api/queue/priority":  apitoken.ScopeControl,
	"GET /api/queue/stop":       apitoken.ScopeRead,
	"POST /api/queue/stop":      apitoken.ScopeControl,

	"GET /api/reconnect":           apitoken.ScopeAdmin,
	"POST /api/reconnect":          apitoken.ScopeAdmin,
	"POST /api/reconnect/import":   apitoken.ScopeAdmin,
	"GET /api/reconnect/router":    apitoken.ScopeAdmin,
	"GET /api/relay/config":        apitoken.ScopeAdmin,
	"PUT /api/relay/config":        apitoken.ScopeAdmin,
	"GET /api/remote-access":       apitoken.ScopeRead,
	"GET /api/resolvers/jd":        apitoken.ScopeAdmin,
	"GET /api/resolvers/priority":  apitoken.ScopeAdmin,
	"POST /api/resolvers/priority": apitoken.ScopeAdmin,
	"GET /api/rules/grammar":       apitoken.ScopeAdmin,
	"POST /api/rules/preview":      apitoken.ScopeAdmin,

	// The timetable is configuration; setting it aside for a while is a queue
	// verb, like halting the queue.
	"GET /api/schedule":            apitoken.ScopeAdmin,
	"PUT /api/schedule":            apitoken.ScopeAdmin,
	"DELETE /api/schedule/suspend": apitoken.ScopeControl,
	"PUT /api/schedule/suspend":    apitoken.ScopeControl,

	// A script is code this instance runs, running one included.
	"GET /api/scripts":           apitoken.ScopeAdmin,
	"POST /api/scripts":          apitoken.ScopeAdmin,
	"GET /api/scripts/triggers":  apitoken.ScopeAdmin,
	"DELETE /api/scripts/{id}":   apitoken.ScopeAdmin,
	"PUT /api/scripts/{id}":      apitoken.ScopeAdmin,
	"POST /api/scripts/{id}/run": apitoken.ScopeAdmin,

	"GET /api/selftest":         apitoken.ScopeAdmin,
	"POST /api/selftest":        apitoken.ScopeAdmin,
	"GET /api/selftest/request": apitoken.ScopeAdmin,

	"GET /api/settings":          apitoken.ScopeAdmin,
	"PATCH /api/settings":        apitoken.ScopeAdmin,
	"PUT /api/settings":          apitoken.ScopeAdmin,
	"GET /api/settings/defaults": apitoken.ScopeAdmin,
	"GET /api/settings/export":   apitoken.ScopeAdmin,
	"POST /api/settings/import":  apitoken.ScopeAdmin,

	"GET /api/stats/speed":        apitoken.ScopeRead,
	"GET /api/stats/volume":       apitoken.ScopeRead,
	"GET /api/stats/volume/usage": apitoken.ScopeRead,

	"GET /api/system/backup":          apitoken.ScopeAdmin,
	"GET /api/system/deployment":      apitoken.ScopeAdmin,
	"GET /api/system/maintenance":     apitoken.ScopeAdmin,
	"POST /api/system/maintenance":    apitoken.ScopeAdmin,
	"POST /api/system/quit":           apitoken.ScopeAdmin,
	"POST /api/system/restart":        apitoken.ScopeAdmin,
	"POST /api/system/restore":        apitoken.ScopeAdmin,
	"GET /api/system/update-check":    apitoken.ScopeAdmin,
	"POST /api/system/update-install": apitoken.ScopeAdmin,

	// Asking which backends could take a selection changes nothing, POST or
	// not. Streaming a finished file is reading what was downloaded.
	"GET /api/tasks":                 apitoken.ScopeRead,
	"POST /api/tasks/backends":       apitoken.ScopeRead,
	"POST /api/tasks/delete":         apitoken.ScopeControl,
	"POST /api/tasks/enabled":        apitoken.ScopeControl,
	"POST /api/tasks/force":          apitoken.ScopeControl,
	"POST /api/tasks/hold":           apitoken.ScopeControl,
	"POST /api/tasks/move":           apitoken.ScopeControl,
	"POST /api/tasks/options":        apitoken.ScopeControl,
	"POST /api/tasks/package":        apitoken.ScopeControl,
	"POST /api/tasks/package/rename": apitoken.ScopeControl,
	"POST /api/tasks/pause":          apitoken.ScopeControl,
	"POST /api/tasks/priority":       apitoken.ScopeControl,
	"POST /api/tasks/recheck":        apitoken.ScopeControl,
	"POST /api/tasks/reorder":        apitoken.ScopeControl,
	"POST /api/tasks/restart":        apitoken.ScopeControl,
	"POST /api/tasks/resume":         apitoken.ScopeControl,
	"POST /api/tasks/start":          apitoken.ScopeControl,
	"POST /api/tasks/undo-delete":    apitoken.ScopeControl,
	"DELETE /api/tasks/{id}":         apitoken.ScopeControl,
	"GET /api/tasks/{id}/file":       apitoken.ScopeRead,
	"POST /api/tasks/{id}/pause":     apitoken.ScopeControl,
	"POST /api/tasks/{id}/resume":    apitoken.ScopeControl,

	"GET /api/tokens":         apitoken.ScopeAdmin,
	"POST /api/tokens":        apitoken.ScopeAdmin,
	"DELETE /api/tokens/{id}": apitoken.ScopeAdmin,

	// Parsing a .torrent is the first half of adding one.
	"POST /api/torrents":         apitoken.ScopeAdd,
	"POST /api/torrents/parse":   apitoken.ScopeAdd,
	"POST /api/torrents/portmap": apitoken.ScopeAdmin,
	// How the tracker list named in the settings was fetched, which is part
	// of the Torrents settings page.
	"GET /api/torrents/trackers": apitoken.ScopeAdmin,

	// The interface state also holds the SABnzbd bridge's grabs.
	"GET /api/uistate": apitoken.ScopeRead,
	"PUT /api/uistate": apitoken.ScopeAdmin,
	"GET /api/ws":      apitoken.ScopeRead,

	"GET /api/ytdlp/cookies":         apitoken.ScopeAdmin,
	"POST /api/ytdlp/cookies":        apitoken.ScopeAdmin,
	"POST /api/ytdlp/cookies/remove": apitoken.ScopeAdmin,
	"GET /api/ytdlp/formats":         apitoken.ScopeAdmin,
	"GET /api/ytdlp/preset":          apitoken.ScopeAdmin,
	"POST /api/ytdlp/preset":         apitoken.ScopeAdmin,
}

// sabnzbdScopes is the right each operation of the SABnzbd bridge needs. The
// bridge is one open route that checks its own ?apikey=, so the operation is
// read from the mode, and "delete" stands for mode=queue or mode=history with
// name=delete. The bridge answers an operation missing here as not
// implemented, so one added to its switch alone is served to nobody.
var sabnzbdScopes = map[string]apitoken.Scope{
	"version":    apitoken.ScopeRead,
	"get_config": apitoken.ScopeRead,
	"queue":      apitoken.ScopeRead,
	"history":    apitoken.ScopeRead,
	"addfile":    apitoken.ScopeAdd,
	"addurl":     apitoken.ScopeAdd,
	"delete":     apitoken.ScopeControl,
}

// qbittorrentScopes is sabnzbdScopes for the qBittorrent bridge, keyed by the
// call below /api/v2/. The login and the logout concern only the caller's own
// session and need no right. A call missing here is answered as one
// qBittorrent does not have.
var qbittorrentScopes = map[string]apitoken.Scope{
	"app/version":         apitoken.ScopeRead,
	"app/webapiVersion":   apitoken.ScopeRead,
	"app/buildInfo":       apitoken.ScopeRead,
	"app/preferences":     apitoken.ScopeRead,
	"torrents/info":       apitoken.ScopeRead,
	"torrents/properties": apitoken.ScopeRead,
	"torrents/files":      apitoken.ScopeRead,
	"torrents/categories": apitoken.ScopeRead,
	"sync/maindata":       apitoken.ScopeRead,
	"transfer/info":       apitoken.ScopeRead,

	// Sonarr makes its category when it tests the connection, and sets the
	// share limits right after an add when the indexer asks for them. A
	// category made here has a name and no folder, and the limits are only
	// reported back, both as torrents/add takes them.
	"torrents/add":            apitoken.ScopeAdd,
	"torrents/createCategory": apitoken.ScopeAdd,
	"torrents/setShareLimits": apitoken.ScopeAdd,

	"torrents/delete":        apitoken.ScopeControl,
	"torrents/pause":         apitoken.ScopeControl,
	"torrents/stop":          apitoken.ScopeControl,
	"torrents/resume":        apitoken.ScopeControl,
	"torrents/start":         apitoken.ScopeControl,
	"torrents/topPrio":       apitoken.ScopeControl,
	"torrents/setForceStart": apitoken.ScopeControl,
	"torrents/setCategory":   apitoken.ScopeControl,
}

// qbittorrentAddMay are the control calls a token with add may still make, in
// the part Sonarr's own round needs. After every add it resumes the torrent,
// which takes a stopped add out of the collector and leaves the rest alone,
// and after an import it deletes the torrent, which is then only forgotten.
// Resuming a paused download and deleting an unfinished one stay refused.
var qbittorrentAddMay = map[string]bool{
	"torrents/resume": true,
	"torrents/start":  true,
	"torrents/delete": true,
}

// scopeFor is the scope the table gives a route pattern, or admin for one the
// table does not name.
func scopeFor(pattern string) apitoken.Scope {
	if s, ok := routeScopes[pattern]; ok {
		return s
	}
	return apitoken.ScopeAdmin
}

// callScope is the scope a call to pattern with body needs.
func callScope(pattern string, body []byte) apitoken.Scope {
	if folderRoutes[pattern] && picksFolder(body) {
		return apitoken.ScopeAdmin
	}
	return scopeFor(pattern)
}

// picksFolder reports whether body names a destination folder. It decodes the
// way decodeJSON does, which reads the first value and ignores what follows,
// so no body reads one way here and another in the handler.
func picksFolder(body []byte) bool {
	var b struct {
		Dir *string `json:"dir"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&b); err != nil {
		return false
	}
	return b.Dir != nil && strings.TrimSpace(*b.Dir) != ""
}

// tokenKeyType marks a request that the guard let in on an API token. It is a
// context value set after the token was checked, never a header a client could
// send.
type tokenKeyType struct{}

var tokenKey tokenKeyType

// tokenOf returns the token a request was let in with, if it was a token.
func tokenOf(r *http.Request) (apitoken.Token, bool) {
	tok, ok := r.Context().Value(tokenKey).(apitoken.Token)
	return tok, ok
}

func withToken(r *http.Request, tok apitoken.Token) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), tokenKey, tok))
}

// requireScope refuses a request let in on a token that lacks what the call to
// pattern needs. Anything else the guard let in holds every right.
func requireScope(pattern string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := tokenOf(r)
		if !ok {
			next(w, r)
			return
		}
		var b []byte
		if folderRoutes[pattern] {
			var err error
			if b, err = io.ReadAll(body(r)); err != nil {
				http.Error(w, "could not read the request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		if need := callScope(pattern, b); !tok.Has(need) {
			refuseScope(w, need)
			return
		}
		next(w, r)
	}
}

// someTokenHolds reports whether one of tokens carries every scope in scopes,
// which is what a module row asks before telling the owner to make one.
func someTokenHolds(tokens []apitoken.Token, scopes ...apitoken.Scope) bool {
	return slices.ContainsFunc(tokens, func(t apitoken.Token) bool {
		for _, s := range scopes {
			if !t.Has(s) {
				return false
			}
		}
		return true
	})
}

// permits is requireScope for an open route that looks at its caller itself.
func permits(a *app.App, r *http.Request, s apitoken.Scope) bool {
	tok, ok := caller(a, r)
	return ok && (tok == nil || tok.Has(s))
}

// refuseScope answers 403 naming the right the token is missing, so whoever
// set the token up knows which one to add.
func refuseScope(w http.ResponseWriter, s apitoken.Scope) {
	writeRefusal(w, http.StatusForbidden, "tokenScope", scopeRefusal(s), map[string]string{"scope": string(s)})
}

func scopeRefusal(s apitoken.Scope) string {
	return fmt.Sprintf("this API token does not have the %q right", s)
}
