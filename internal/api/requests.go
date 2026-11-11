package api

// What a request that changes something has to look like. A form on another
// website can post to any address the visitor's browser reaches, without a
// preflight and without a cookie, but only as a urlencoded, multipart or
// text/plain body. Everything else, a fetch with application/json included,
// needs a preflight this server never answers. Requiring JSON therefore
// closes the form-post route whatever the Origin and Sec-Fetch-Site headers
// say, which matters most on an instance without a password.

import (
	"mime"
	"net/http"

	"github.com/junkerderprovinz/parleyport/kit/group"
)

// otherBodies are the routes that take a body other than JSON, each for a
// client that cannot send one. They keep the cross-site refusal in sameOrigin
// and a credential of their own; the reason says which.
var otherBodies = map[string]string{
	"POST /api/system/restore":       "a backup archive, uploaded as multipart from the web interface",
	"POST /api/system/restore/check": "the same archive, checked before a restore is confirmed",
	"POST /api/containers":           "a link container, uploaded as multipart from the web interface",
	"POST /api/torrents/parse":       "a .torrent, uploaded as multipart from the web interface",
	"POST /api/jdimport/read":        "a JDownloader folder, uploaded as multipart from the web interface",
	"POST " + qbitPrefix + "{call...}": "Sonarr and Radarr post forms to the qBittorrent bridge, " +
		"which checks its own session or API token on every call",
	"POST " + sabnzbdPath: "Sonarr and Radarr upload an .nzb as multipart to the SABnzbd bridge, " +
		"which checks the API token in ?apikey= on every call",
	"PROPFIND " + drivePrefix + "/{path...}": "a WebDAV client sends XML, and the drive checks an API token on every call",
	drivePrefix + "/{path...}":               "the drive refuses every other method itself, whatever the body",
}

// alsoTakes are the routes that take one other type of body besides JSON.
// Another site cannot send a type outside the three a form posts without a
// preflight either, so such a route stays as closed to it as one taking JSON.
var alsoTakes = map[string]string{
	// kit/group sends an upload, a backup an app of the fortress hands back,
	// as a stream of this type.
	"POST " + group.DirectPath: "application/x-kit-upload",
}

// changesState reports whether method can change something. GET, HEAD and
// OPTIONS cannot on any route here, and another site can read none of their
// answers.
func changesState(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// requireJSON answers 415 to a request with a body that is not declared as
// JSON, unless the route pattern is one of otherBodies or the body is of the
// type alsoTakes names for it. A request without a body has nothing to
// declare: plenty of routes act on a bare POST.
func requireJSON(pattern string, next http.HandlerFunc) http.HandlerFunc {
	if _, ok := otherBodies[pattern]; ok {
		return next
	}
	other := alsoTakes[pattern]
	return func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if changesState(r.Method) && r.ContentLength != 0 && !isJSON(ct) && (other == "" || !isType(ct, other)) {
			http.Error(w, "this endpoint takes application/json; set the Content-Type header", http.StatusUnsupportedMediaType)
			return
		}
		next(w, r)
	}
}

// isJSON reports whether a Content-Type header names JSON, with or without a
// charset.
func isJSON(contentType string) bool { return isType(contentType, "application/json") }

func isType(contentType, want string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == want
}
