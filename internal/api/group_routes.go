package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// groupRoutes are the routes a member of this instance's phrase group reaches,
// by the pattern they are registered under: the phone app, the browser
// extension and the other instances, which all hold the twelve words and
// nothing else. A route that is not here answers ownLoginNeeded to such a
// call, so a new one stays out until somebody puts it here.
var groupRoutes = map[string]bool{
	// Whether a password is wanted, the look, the instances of the group and
	// the addresses this instance answers on. The addresses travel inside the
	// sealed frame, so the relay operator never learns them.
	"GET /api/auth":          true,
	"GET /api/appearance":    true,
	"POST /api/appearance":   true,
	"GET /api/instances":     true,
	"GET /api/remote-access": true,

	// The phones and browsers of the group, which the phone app and the
	// extension list beside the instances.
	"GET /api/group/apps": true,

	// Adding links. A folder outside the download folders is refused below.
	"POST /api/links": true,

	// The list and what the rows do. Showing a member's downloads without
	// being able to stop them would be half a connection.
	"GET /api/tasks":                     true,
	"GET /api/tasks/watch":               true,
	"POST /api/tasks/backends":           true,
	"POST /api/tasks/delete":             true,
	"POST /api/tasks/enabled":            true,
	"POST /api/tasks/force":              true,
	"POST /api/tasks/hold":               true,
	"POST /api/tasks/move":               true,
	"POST /api/tasks/options":            true,
	"POST /api/tasks/package":            true,
	"POST /api/tasks/package/rename":     true,
	"POST /api/tasks/pause":              true,
	"POST /api/tasks/priority":           true,
	"POST /api/tasks/recheck":            true,
	"POST /api/tasks/reorder":            true,
	"POST /api/tasks/restart":            true,
	"POST /api/tasks/resume":             true,
	"POST /api/tasks/seeding/start":      true,
	"POST /api/tasks/seeding/stop":       true,
	"POST /api/tasks/start":              true,
	"POST /api/tasks/undo-delete":        true,
	"DELETE /api/tasks/{id}":             true,
	"GET /api/tasks/{id}/file":           true,
	"POST /api/tasks/{id}/pause":         true,
	"GET /api/tasks/{id}/pieces":         true,
	"POST /api/tasks/{id}/play":          true,
	"POST /api/tasks/{id}/resume":        true,
	"GET /api/tasks/{id}/torrent-files":  true,
	"POST /api/tasks/{id}/torrent-files": true,
	"GET /api/extract":                   true,

	// The speed the phone draws its meter from, a sum over every download
	// that names none of them.
	"GET /api/stats/speed": true,

	// The queue, the master switch of that list.
	"GET /api/queue":            true,
	"POST /api/queue":           true,
	"GET /api/queue/counters":   true,
	"POST /api/queue/enabled":   true,
	"POST /api/queue/force":     true,
	"POST /api/queue/move":      true,
	"GET /api/queue/priorities": true,
	"POST /api/queue/priority":  true,
	"GET /api/queue/stop":       true,
	"POST /api/queue/stop":      true,

	// A captcha holds a download up, and the phone answers or skips it, loads
	// a widget challenge's page and says when that will not load. The web
	// interface's own widget page and its report stay off.
	"GET /api/captcha":                            true,
	"POST /api/captcha/refresh":                   true,
	"POST /api/captcha/{id}/answer":               true,
	"POST /api/captcha/{id}/skip":                 true,
	"GET /api/captcha/{id}/widget/phone":          true,
	"POST /api/captcha/{id}/unanswerable/phone":   true,
	"DELETE /api/captcha/{id}/unanswerable/phone": true,

	// The extension renews and drops its clipboard lease, and the members list
	// and stop one another's watchers.
	"GET /api/clipboard-watchers":            true,
	"PUT /api/clipboard-watchers/{id}":       true,
	"DELETE /api/clipboard-watchers/{id}":    true,
	"POST /api/clipboard-watchers/{id}/stop": true,

	// An approval, a refusal or a removal, passed on by the instance whose
	// owner made it. The handlers take it only from an instance that signs
	// with an approved key, never from a phone or an unsigned caller.
	"POST /api/connect/devices/{id}/approve": true,
	"POST /api/connect/devices/{id}/refuse":  true,
	"DELETE /api/connect/apps/{id}":          true,
	"DELETE /api/connect/members/{id}":       true,
}

// groupLimits are the allowed routes whose request can ask for more than a
// member may do. Each reports whether the call stays within it; body is the
// request body, already read.
var groupLimits = map[string]func(a *app.App, r *http.Request, body []byte) bool{
	// Where files land on the host is configuration, so a member picks only
	// among the folders the owner set up.
	"POST /api/links":         insideDownloadFolders,
	"POST /api/tasks/options": insideDownloadFolders,
	// Removing rows can be undone; erasing what was downloaded cannot.
	"POST /api/tasks/delete": func(_ *app.App, _ *http.Request, body []byte) bool {
		var b struct {
			Files bool `json:"files"`
		}
		_ = json.NewDecoder(bytes.NewReader(body)).Decode(&b)
		return !b.Files
	},
	"DELETE /api/tasks/{id}": func(_ *app.App, r *http.Request, _ []byte) bool {
		return r.URL.Query().Get("files") != "1"
	},
}

// insideDownloadFolders reports whether the folder a body names, if any, lies
// in a folder downloads already go to. It decodes the way decodeJSON does, so
// no body reads one way here and another in the handler.
func insideDownloadFolders(a *app.App, _ *http.Request, body []byte) bool {
	var b struct {
		Dir *string `json:"dir"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&b); err != nil || b.Dir == nil {
		return true
	}
	return a.InsideDownloadFolders(*b.Dir)
}

// admitGroup decides a call from a member of the group before anything else
// sees it, and reports whether the call may go on. A path no route serves, or
// one the mux would first clean, is a 404: what the gate cannot put a route to
// is never served. A route the member may not reach, or a request asking more
// of an allowed one than a member may do, answers ownLoginNeeded.
func (reg *Registry) admitGroup(a *app.App, w http.ResponseWriter, r *http.Request) bool {
	pattern := reg.patternOf(r.Method, r.URL)
	if !reg.patterns[pattern] || path.Clean(r.URL.Path) != r.URL.Path {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return false
	}
	if !groupRoutes[pattern] {
		refuseGroup(w)
		return false
	}
	limit := groupLimits[pattern]
	if limit == nil {
		return true
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, io.NopCloser(body(r)), maxJSONBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "could not read the request body", http.StatusBadRequest)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	if !limit(a, r, b) {
		refuseGroup(w)
		return false
	}
	return true
}

// patternOf is the pattern of the route that serves method and path, or ""
// for none.
func (reg *Registry) patternOf(method string, u *url.URL) string {
	reg.lookupOnce.Do(reg.buildLookup)
	_, pattern := reg.lookup.Handler(&http.Request{Method: method, URL: &url.URL{Path: u.Path, RawPath: u.RawPath}})
	return pattern
}

// refuseGroup answers a member of the group with what it may not do here: the
// owner has to sign in at this instance's own web interface.
func refuseGroup(w http.ResponseWriter) {
	writeRefusal(w, http.StatusForbidden, "ownLoginNeeded",
		"sign in at this instance's web interface to do this; a paired device or instance does not reach it", nil)
}
