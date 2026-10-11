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
)

// otherBodies are the routes that take a body other than JSON, each for a
// client that cannot send one. They keep the cross-site refusal in sameOrigin
// and a credential of their own; the reason says which.
var otherBodies = map[string]string{
	"POST /api/system/restore": "a backup archive, uploaded as multipart from the web interface",
	"POST /api/containers":     "a link container, uploaded as multipart from the web interface",
	"POST /api/torrents/parse": "a .torrent, uploaded as multipart from the web interface",
	"POST /api/jdimport/read":  "a JDownloader folder, uploaded as multipart from the web interface",
	"POST " + qbitPrefix + "{call...}": "Sonarr and Radarr post forms to the qBittorrent bridge, " +
		"which checks its own session or API token on every call",
	"POST " + sabnzbdPath: "Sonarr and Radarr upload an .nzb as multipart to the SABnzbd bridge, " +
		"which checks the API token in ?apikey= on every call",
	"PROPFIND " + drivePrefix + "/{path...}": "a WebDAV client sends XML, and the drive checks an API token on every call",
	drivePrefix + "/{path...}":               "the drive refuses every other method itself, whatever the body",
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
// JSON, unless the route pattern is one of otherBodies. A request without a
// body has nothing to declare: plenty of routes act on a bare POST.
func requireJSON(pattern string, next http.HandlerFunc) http.HandlerFunc {
	if _, ok := otherBodies[pattern]; ok {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if changesState(r.Method) && r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) {
			http.Error(w, "this endpoint takes application/json; set the Content-Type header", http.StatusUnsupportedMediaType)
			return
		}
		next(w, r)
	}
}

// isJSON reports whether a Content-Type header names JSON, with or without a
// charset.
func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}
