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
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/web"
)

// Handler builds the full HTTP handler.
func Handler(a *app.App) http.Handler {
	reg := newRegistry()
	registerAll(reg, a)

	mux := http.NewServeMux()
	reg.attach(mux, spaHandler())
	h := sameOrigin(guard(a, reg, mux))

	// Stored before applyRelay, which is the one thing downstream that reads
	// it back: a relay-proxied call has to be answered exactly the way this
	// same handler would answer a browser or an API token, not by some
	// second, hand-built stack that can drift from the real one. Called here,
	// unconditionally, rather than left for whoever embeds this App to
	// remember - both cmd/knightloader/main.go and desktop/main.go already
	// call Handler exactly once, so a relay address saved in an earlier run
	// reconnects on its own the moment either binary boots, with no
	// container-only or desktop-only line to keep in sync between them.
	a.SetSelfServeHandler(h)
	applyRelay(a)
	// Same reasoning as the two lines above: wired here, unconditionally, so a
	// peer credential saved in an earlier run is in effect the moment either
	// binary boots, with nothing for a caller to remember. See peertokens.go.
	a.Federation.SetPeerTokens(peerTokens{a: a})
	return h
}

// relayGroupKey marks a request as having arrived over this instance's own
// relay socket, from a sibling that presented the same group key.
//
// A context value, deliberately, and not a header: a header can be written by
// whoever composed the frame, so a peer could claim to be a group member by
// setting one. This is attached by relayProxyHandler on THIS side, after the
// relay client has already accepted the frame, so nothing on the wire can
// forge it.
type relayGroupKeyType struct{}

var relayGroupKey relayGroupKeyType

// fromRelayGroup reports whether this request came in over the relay from a
// sibling holding the same connection phrase.
func fromRelayGroup(r *http.Request) bool {
	v, _ := r.Context().Value(relayGroupKey).(bool)
	return v
}

// authenticated reports whether the request carries a valid session or a
// valid API token, or whether no password is set at all.
//
// A token is checked whether or not a cookie was also sent, rather than only
// when the cookie is absent, because that is what makes it possible to test
// one route with `curl -H Authorization` without first fighting the browser
// session out of the way.
func authenticated(a *app.App, r *http.Request) bool {
	if !a.Auth.Enabled() {
		return true
	}
	// A sibling on the relay has already proved it holds the group key, which
	// is derived from the connection phrase - and the phrase is stated
	// everywhere as reaching every instance in the group. Requiring a second,
	// separately-exchanged credential on top would protect nothing: anyone who
	// can present the group key can join the group and be handed one. This is
	// what lets a password-protected instance be usable from its own siblings
	// instead of answering 401 to all of them, which is what it did while the
	// phrase and the credential were two unrelated things.
	//
	// The blast radius is bounded on the way in, not here: relayProxyHandler
	// admits only the task and link routes, the same set the outbound side is
	// willing to forward. A sibling cannot read this instance's accounts,
	// change its password or ask for its phrase.
	if fromRelayGroup(r) {
		return true
	}
	if c, err := r.Cookie(auth.CookieName); err == nil && a.Auth.Valid(c.Value) {
		return true
	}
	if tok := bearerToken(r); tok != "" {
		if _, ok := a.APITokens.Check(tok); ok {
			return true
		}
	}
	return false
}

// bearerToken reads the RFC 6750 Authorization header, the way a script, a
// browser extension background page or a phone app authenticates: none of
// those hold the session cookie a browser tab does, and none of them should
// have to. That is the entire reason a named token exists as well as the
// shared password. Case-insensitive on the scheme, exactly as the RFC and
// net/http's own header canonicalisation already are.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
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
	// Same reasoning for activity: without this, a client that reconnects
	// mid-burst has no way to learn the true current counters and can only
	// ever repeat whatever its last "activity" broadcast said - permanently,
	// if that burst has since ended and nothing new of that kind starts.
	a.Hub.SendTo(c, "activitySnapshot", a.ActivitySnapshot())
	for {
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		handleWSControl(a, c, data)
	}
}

// wsControl is the one message shape a client sends up this socket: which
// broadcast kinds it wants from here on. See internal/hub.Subscribe's own
// doc comment for what "type":"subscribe" vs "unsubscribe" and a Kinds of
// "*" each mean. Everything this app pushes down the same socket is a
// different, un-typed shape ({"type","data"}, see hub.Send), so the two
// directions never collide on one Go type.
type wsControl struct {
	Type  string   `json:"type"`
	Kinds []string `json:"kinds"`
}

// handleWSControl reads one client frame. An unparseable or unrecognised one
// is silently ignored rather than closing the socket: the read loop's job is
// to notice a dead connection, not to police malformed JSON from a client
// that is otherwise working fine, and a client that only ever listens (every
// version of the UI before this feature existed) never sends anything here
// at all.
func handleWSControl(a *app.App, c hub.Conn, data []byte) {
	var msg wsControl
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	switch msg.Type {
	case "subscribe":
		a.Hub.Subscribe(c, msg.Kinds)
	case "unsubscribe":
		a.Hub.Unsubscribe(c, msg.Kinds)
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
	// Go's own mime package has no built-in mapping for .webmanifest (checked
	// against its source, not assumed - mime.TypeByExtension(".webmanifest")
	// returns ""), so without this http.FileServer falls through to content
	// sniffing, which reads a manifest's leading "{" as plain text and serves
	// it as text/plain rather than the type the PWA install prompt and
	// several browsers' own manifest parsers expect. Registered once, here
	// rather than in an init(), because this file is a library package that
	// might be imported without ever calling Handler - an init() would
	// mutate the process-wide mime table as a side effect of importing this
	// package, not of using it.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

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
	if err := json.NewDecoder(body(r)).Decode(v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

// body is r.Body, or an empty one when there is none.
//
// The server always gives a handler a non-nil Body, so this looks like belt and
// braces and is not: the relay builds its own *http.Request from a frame, and
// http.NewRequest with no payload leaves Body nil. Every route that decodes a
// body would therefore panic on a bodyless relay call - json.Decoder reads
// straight through the nil interface - and the relay calls ServeHTTP directly,
// with no server around it to recover. The first POST ever added to the relay
// allowlist found it immediately.
//
// Fixed here rather than at that one route on purpose. The hole is in the
// helper every route shares, and patching the caller that happened to reach it
// leaves the same crash armed behind every route added later.
func body(r *http.Request) io.Reader {
	if r.Body == nil {
		return strings.NewReader("")
	}
	return r.Body
}

// decodeBody is decodeJSON for the routes where an absent body is a valid
// request meaning "all of them". The error is deliberately available rather
// than acted on: those handlers treat an unreadable body the same as an empty
// one, which is what makes a bare POST work.
func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(body(r)).Decode(v)
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
