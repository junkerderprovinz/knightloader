// Package api exposes the app over HTTP: a small REST surface plus a WebSocket
// stream under /api, and the embedded SPA everywhere else. Every route registers
// through the table in routes.go.
package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/web"
)

// Handler builds the full HTTP handler.
func Handler(a *app.App) http.Handler {
	reg := newRegistry()
	registerAll(reg, a)

	mux := http.NewServeMux()
	reg.attach(mux, spaHandler())
	h := withBasePath(sameOrigin(guard(a, reg, mux)))

	// A relay-proxied call is answered by this same handler, so it cannot drift
	// from what a browser or an API token gets. Wiring the relay and the peer
	// tokens here means a saved relay address and saved peer credentials take
	// effect as soon as either binary boots.
	a.SetSelfServeHandler(h)
	applyRelay(a)
	a.Federation.SetPeerTokens(peerTokens{a: a})
	return h
}

// relayGroupKey marks a request as having arrived over this instance's own
// relay socket, from a sibling that presented the same group key. It is a
// context value rather than a header because a header can be forged by whoever
// composed the frame; relayProxyHandler attaches it only after the relay client
// has accepted the frame.
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
func authenticated(a *app.App, r *http.Request) bool {
	_, ok := caller(a, r)
	return ok
}

// caller works out who is asking: ok is false for nobody, and tok is set when
// an API token is what let the request in, since only a token is narrowed by
// scopes. A token is checked even when a cookie was sent too, so one route can
// be tested with curl from a logged-in browser machine.
func caller(a *app.App, r *http.Request) (tok *apitoken.Token, ok bool) {
	if !a.Auth.Enabled() {
		return nil, true
	}
	// A sibling on the relay already holds the group key, derived from the
	// connection phrase that reaches every instance in the group, so a second
	// credential would protect nothing. relayProxyHandler limits such calls to
	// the task and link routes.
	if fromRelayGroup(r) {
		return nil, true
	}
	if c, err := r.Cookie(auth.CookieName); err == nil && a.Auth.Valid(c.Value) {
		return nil, true
	}
	if secret := bearerToken(r); secret != "" {
		if t, ok := a.APITokens.Check(secret); ok {
			return &t, true
		}
	}
	return nil, false
}

// bearerToken reads the RFC 6750 Authorization header, which is how scripts,
// extension background pages and phone apps authenticate without a session
// cookie. The scheme is matched case-insensitively, as the RFC requires.
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
		Path:     cookiePath(r),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL / time.Second),
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	paths := []string{cookiePath(r)}
	// The browser sends a cookie set at the root along with this one: one from
	// before the base path was configured, or from the LAN address of the same
	// host. Left alone, it would keep the session alive after the logout.
	if paths[0] != "/" {
		paths = append(paths, "/")
	}
	for _, p := range paths {
		http.SetCookie(w, &http.Cookie{
			Name: auth.CookieName, Value: "", Path: p, HttpOnly: true,
			Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
		})
	}
}

// cookiePath keeps the session cookie under the base path, so requests to
// other applications on the same host do not carry it. It is no boundary
// against their scripts, which share the origin and can call the API.
func cookiePath(r *http.Request) string {
	if p := requestBasePath(r); p != "" {
		return p
	}
	return "/"
}

// guard refuses API calls without a session once a password is set. The open
// routes come from the registration table, so there is no second list that
// could disagree with it. A request let in on a token carries the token on,
// for requireScope to check against the route.
func guard(a *app.App, reg *Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reg.open(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if tok, ok := caller(a, r); ok {
			if tok != nil {
				r = withToken(r, *tok)
			}
			next.ServeHTTP(w, r)
			return
		}
		// Setting the first password needs no session; there is nothing to
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
	// Queued through the hub rather than written to the socket, so a task
	// event sent after Add cannot overtake the older snapshot. The activity
	// snapshot gives a reconnecting client the current counters instead of
	// whatever its last broadcast said.
	a.Hub.SendTo(c, "snapshot", a.Tasks())
	a.Hub.SendTo(c, "activitySnapshot", a.ActivitySnapshot())
	for {
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		handleWSControl(a, c, data)
	}
}

// wsControl is what a client sends up the socket: which broadcast kinds it
// wants from here on (see hub.Subscribe), or whether its viewer can see it
// (see hub.SetVisible).
type wsControl struct {
	Type    string   `json:"type"`
	Kinds   []string `json:"kinds"`
	Visible *bool    `json:"visible"`
}

// handleWSControl applies one client frame. A frame it cannot parse is ignored
// rather than closing the socket; the read loop is there to notice a dead
// connection, not to police the client.
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
	case "visibility":
		if msg.Visible != nil {
			a.Hub.SetVisible(c, *msg.Visible)
		}
	}
}

// spaHandler serves the embedded build, falling back to index.html for
// client-side routes.
//
// The bundle file names carry no content hash, so the committed dist does not
// churn on every build, and embedded files have no modification time. An ETag
// over the file's bytes plus no-cache makes the browser revalidate instead of
// keeping a stale app.js after a redeploy.
func spaHandler() http.Handler {
	// The mime package has no mapping for .webmanifest, and content sniffing
	// would serve a manifest as text/plain. Registered here rather than in an
	// init() so importing the package alone does not change the process-wide
	// table.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	sub, _ := fs.Sub(web.Dist, "dist")
	etags := buildETags(sub)
	fileServer := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")

	serve := func(w http.ResponseWriter, r *http.Request, name string) {
		if tag, ok := etags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(sub, p); err == nil {
				serve(w, r, p)
				return
			}
		}
		serveIndex(w, r, index)
	})
}

// serveIndex answers with the page every client-side route starts from. Its
// <base> element names where the app lives, and the build writes every asset
// path relative to it, so one build works at the root and under any prefix.
func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	base := []byte("<head>\n    <base href=\"" + html.EscapeString(requestBasePath(r)) + "/\" />")
	page := bytes.Replace(index, []byte("<head>"), base, 1)
	w.Header().Set("ETag", etagOf(page))
	w.Header().Set("Cache-Control", "no-cache")
	if buildinfo.BasePath == "" {
		w.Header().Add("Vary", "X-Forwarded-Prefix")
	}
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
}

// buildETags hashes every embedded file once at startup; the build does not
// change for the life of the process.
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
		tags[path] = etagOf(b)
		return nil
	})
	return tags
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONStatus is writeJSON with a status other than 200. The header has to
// be set before WriteHeader or it is silently dropped.
func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeRefusal answers with a refusal the interface translates: code names
// the reason and params fill its sentence, while text says the same in
// English for every other client.
func writeRefusal(w http.ResponseWriter, status int, code, text string, params map[string]string) {
	out := map[string]any{"error": text, "code": code}
	if params != nil {
		out["params"] = params
	}
	writeJSONStatus(w, status, out)
}

// decodeJSON reads a JSON body and answers 400 itself when it cannot. It
// reports whether the body was usable.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(body(r)).Decode(v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

// body is r.Body, or an empty reader when there is none. The server always
// sets Body, but the relay builds its own requests with http.NewRequest, which
// leaves it nil for a bodyless call, and decoding that would panic.
func body(r *http.Request) io.Reader {
	if r.Body == nil {
		return strings.NewReader("")
	}
	return r.Body
}

// decodeBody is decodeJSON for routes where an absent body means "all of
// them". Those handlers treat an unreadable body like an empty one, which is
// what makes a bare POST work.
func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(body(r)).Decode(v)
}

// requireIDs refuses a request that names no tasks, for the routes where
// "none" cannot sensibly mean "all": an empty selection there is a client bug,
// and answering 204 would hide it.
func requireIDs(w http.ResponseWriter, ids []string) bool {
	if len(ids) == 0 {
		http.Error(w, "this needs at least one task id", http.StatusBadRequest)
		return false
	}
	return true
}
