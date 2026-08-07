// Package api exposes the app over HTTP: a small REST surface plus a WebSocket
// stream under /api, and the embedded SPA everywhere else.
//
// This file is the router and the two pieces of middleware every request passes
// through. The endpoints themselves live in routes_*.go, one file per subsystem,
// and every one of them registers through the table in routes.go — see the note
// there for why nothing may attach a handler to the mux by hand.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/web"
)

// Handler builds the full HTTP handler.
func Handler(a *app.App) http.Handler {
	reg := newRegistry()
	registerSystem(reg, a)
	registerTasks(reg, a)
	registerBulk(reg, a)
	registerQueue(reg, a)
	registerLinks(reg, a)
	registerContainers(reg, a)
	registerSettings(reg, a)
	registerAccounts(reg, a)
	registerSchedule(reg, a)
	registerReconnect(reg, a)
	registerUIState(reg, a)
	registerFederation(reg, a)

	mux := http.NewServeMux()
	reg.attach(mux, spaHandler())
	return sameOrigin(guard(a, reg, mux))
}

// authenticated reports whether the request carries a valid session, or whether
// no password is set at all.
func authenticated(a *app.App, r *http.Request) bool {
	if !a.Auth.Enabled() {
		return true
	}
	c, err := r.Cookie(auth.CookieName)
	return err == nil && a.Auth.Valid(c.Value)
}

func setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL / time.Second),
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// guard refuses API calls without a session once a password is set. Which routes
// are exempt is read from the registration table rather than from a list of
// paths kept here: a second list is a list that disagrees with the first one
// eventually, and the disagreement is either a locked login page or an open API.
func guard(a *app.App, reg *Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reg.open(r.URL.Path) || authenticated(a, r) {
			next.ServeHTTP(w, r)
			return
		}
		// Setting the FIRST password needs no session; there is nothing to
		// protect yet and no way to get one.
		if r.URL.Path == "/api/auth/password" && !a.Auth.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// sameOrigin keeps other websites from driving this instance through the
// visitor's browser. The UI is served from the same origin as the API, so no
// cross-origin access is ever needed.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originMatchesHost compares an Origin header against the host being addressed.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func serveWS(a *app.App, w http.ResponseWriter, r *http.Request) {
	// No InsecureSkipVerify: the library then requires Origin to match Host,
	// which is what stops another site from opening this socket.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	a.Hub.Add(c)
	defer func() {
		a.Hub.Remove(c)
		c.CloseNow()
	}()
	// Queued through the hub, not written straight to the socket: the writer
	// goroutine started with Add above, so a task event that arrives in between
	// could otherwise reach the client first and be overwritten by the older
	// snapshot.
	a.Hub.SendTo(c, "snapshot", a.Tasks())
	for {
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

// spaHandler serves the embedded build, falling back to index.html for
// client-side routes.
//
// The bundle file names deliberately carry no content hash, so that the dist
// committed to the repository does not churn on every build. That leaves the
// cache with nothing to go on: embedded files have no modification time, so
// net/http sends no Last-Modified either, and a browser is then free to keep a
// stale app.js for as long as it likes — which after a redeploy means an old
// UI talking to a new API, or a blank page.
//
// The fix is an ETag over the file's own bytes plus no-cache, which does not
// mean "do not cache" but "revalidate before use". A reload then costs one
// conditional request that almost always answers 304.
func spaHandler() http.Handler {
	sub, _ := fs.Sub(web.Dist, "dist")
	etags := buildETags(sub)
	fileServer := http.FileServer(http.FS(sub))

	serve := func(w http.ResponseWriter, r *http.Request, name string) {
		if tag, ok := etags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			serve(w, r, "index.html")
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			serve(w, r, p)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		serve(w, r2, "index.html")
	})
}

// buildETags hashes every embedded file once at startup. The build is immutable
// for the life of the process, so this is computed once rather than per request.
func buildETags(sub fs.FS) map[string]string {
	tags := map[string]string{}
	_ = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(sub, path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		tags[path] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	return tags
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONStatus is writeJSON with a status other than 200. The header has to
// be set before WriteHeader or it is silently dropped, which is how a 202 ends
// up being served as text/plain.
func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON reads a JSON body and answers the caller itself when it cannot,
// so that a handler's happy path is not three lines of the same error handling.
// It reports whether the body was usable.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

// decodeBody is decodeJSON for the routes where an absent body is a valid
// request meaning "all of them". The error is deliberately available rather
// than acted on: those handlers treat an unreadable body the same as an empty
// one, which is what makes a bare POST work.
func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// requireIDs refuses a request that names no tasks, for the routes where "none"
// cannot sensibly mean "all": moving nothing to the top of the queue is a
// mistake somewhere in the client, and answering 204 to it hides that.
func requireIDs(w http.ResponseWriter, ids []string) bool {
	if len(ids) == 0 {
		http.Error(w, "this needs at least one task id", http.StatusBadRequest)
		return false
	}
	return true
}
