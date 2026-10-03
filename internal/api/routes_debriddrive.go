package api

// The debrid drive at /dav/: what is on the debrid accounts as a read-only
// WebDAV share, for an rclone mount a media server streams from
// (internal/debriddrive).
//
// It sits outside /api/ because a WebDAV client treats the address it is
// given as the top of the share, and rclone, Windows and the players that
// speak WebDAV all show that path to the person who set them up. Like the
// download-client doors, it checks its own credential on every request: an
// API token that can read, sent as a Bearer token, which is what rclone's
// bearer_token option sends, or as the password of Basic authentication,
// which every other client can send. The login password is not taken. It
// would sit in a config file on another machine, and it would get past the
// second factor.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// drivePrefix is where the drive is mounted, below the base path.
const drivePrefix = "/dav"

func registerDebridDrive(reg *Registry, a *app.App) {
	serve := func(w http.ResponseWriter, r *http.Request) { serveDrive(a, w, r) }
	reg.AddOpen(http.MethodGet, drivePrefix+"/{path...}",
		"read a file of the debrid drive, with Range for a part of it; off unless \"Debrid drive\" is switched on "+
			"(Accounts page or Modules page), and the credential is an API token of this instance that can read, "+
			"as a Bearer token or as the Basic password", serve)
	reg.AddOpen("PROPFIND", drivePrefix+"/{path...}",
		"list a folder of the debrid drive as WebDAV does, one level at a time; same switch, same credential", serve)
	reg.AddOpen(http.MethodOptions, drivePrefix+"/{path...}",
		"tell a WebDAV client what the debrid drive answers to; same switch, same credential", serve)
	// Without it a write would fall through to the interface and answer 200
	// with its index page, which a client takes for success.
	reg.AddOpen(AnyMethod, drivePrefix+"/{path...}",
		"refuse every other method on the debrid drive, the writes among them, with 405; same switch, same credential", serve)
}

func serveDrive(a *app.App, w http.ResponseWriter, r *http.Request) {
	// The switch comes before the credential, so a closed drive does not
	// reveal whether a token would have worked. The wording matches the /api/
	// catch-all.
	if !a.Settings.Get().DebridDrive.Enabled {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	var tok apitoken.Token
	ok := false
	if secret := tokenSecret(r); secret != "" {
		tok, ok = a.APITokens.Check(secret)
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="KnightLoader debrid drive", charset="UTF-8"`)
		http.Error(w, "the debrid drive needs an API token of this instance that can read", http.StatusUnauthorized)
		return
	}
	if !tok.Has(apitoken.ScopeRead) {
		refuseScope(w, apitoken.ScopeRead)
		return
	}
	// The base path is off the request by now, and webdav names every entry
	// by the path it was asked under, which the client compares with its own.
	prefix := drivePrefix
	if base := requestBasePath(r); base != "" {
		prefix = base + drivePrefix
		r = r.Clone(r.Context())
		r.URL.Path = base + r.URL.Path
		r.URL.RawPath = ""
	}
	a.DebridDrive.Serve(w, r, prefix)
}
