package api

// The self-hosted relay's own configuration: the address this instance dials
// out to, and whether the key that authorises it there is stored.
//
// The two halves live in two different stores on purpose, and this file is
// the only thing that knows both. The address is public identity and sits in
// settings.json beside KnownDomains (internal/settings/settings_relay.go);
// the key IS the authorization check the relay makes, so it is sealed in
// internal/accounts under relay.AccountService, the same place a TorBox or
// debrid key goes. That asymmetry is also why the relay does not simply ride
// along on PUT /api/settings: GET /api/settings hands the whole document
// back, and a secret must never be reachable through a route that does that.
//
// Nothing here ever answers with the key. Like GET /api/accounts and
// GET /api/tokens before it, this route reports state - whether something is
// configured - and never the credential itself.

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
)

// relayConfig is what both GET and PUT /api/relay/config answer with: the
// address, and the one bit of the key anybody is entitled to know.
type relayConfig struct {
	RelayURL string `json:"relayUrl"`
	KeySet   bool   `json:"keySet"`
	// Connected is whether the socket to that relay is actually up right now,
	// not whether a URL and a key happen to be stored.
	//
	// Without it the Access page could only infer "live" from the config being
	// filled in, which is true for a typo'd address, a key the relay rejects
	// and a relay that is simply down - all three then looked identical to a
	// working relay with nobody else connected to it. relay.Client.Connected()
	// existed for exactly this and had no callers at all.
	Connected bool `json:"connected"`
	// Serve is whether this instance is itself running the relay, under
	// /relay/connect on its own address.
	Serve bool `json:"serve"`
	// ServeClients is how many instances are connected to it right now, this
	// one included if it dials its own relay. Zero while Serve is false.
	ServeClients int `json:"serveClients"`
}

func registerRelay(reg *Registry, a *app.App) {
	// The relay's own socket, on this instance's address. Open, because the
	// relay key in the first frame IS the credential and there is no other:
	// every instance dialling in is a different machine with no session here,
	// which is the whole point. relay.Server admits only the key this instance
	// stores, so an open route is not an open relay.
	//
	// One Server for the life of the process, with the switch read per
	// connection. Building it on demand would drop every connected sibling
	// each time an unrelated setting was saved.
	srv := relay.New()
	srv.Admit = func(key string) bool {
		if !a.Settings.Get().RelayServe {
			return false
		}
		stored, err := a.Accounts.Get(relay.AccountService)
		if err != nil || stored == "" {
			return false
		}
		// Constant time, because this is a bearer credential and the caller
		// controls the guess. The relay's own minimum key length keeps the
		// comparison from being a useful oracle about length alone.
		return subtle.ConstantTimeCompare([]byte(key), []byte(stored)) == 1
	}
	reg.AddOpen(http.MethodGet, "/relay/connect",
		"the relay socket, when this instance is serving one - authorised by the relay key in the first frame, never by a session",
		func(w http.ResponseWriter, r *http.Request) {
			// Answered as a route that is not there, rather than as one that
			// refuses: with the switch off this instance is not a relay, and
			// saying "no such endpoint" is the same answer any KnightLoader
			// that never had the feature gives. A client that gets it can
			// treat every version alike.
			if !a.Settings.Get().RelayServe {
				http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
				return
			}
			srv.ServeHTTP(w, r)
		})

	reg.Add(http.MethodGet, "/api/relay/config",
		"the relay this instance dials out to, and whether a key is stored for it - never the key itself",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, relayConfigOf(a, srv))
		})

	reg.Add(http.MethodPut, "/api/relay/config",
		"set the relay address, and the relay key when one is sent; saving either reconnects the relay client",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				RelayURL string `json:"relayUrl"`
				// Key is a pointer because three requests have to be told
				// apart and only two of them carry a string. Absent means
				// "leave the stored key alone", which is the ordinary save of
				// an edited address from a form that was never shown the key
				// and so has nothing to send back; "" means "clear it", the
				// only way a stored secret can ever be removed on purpose
				// (accounts.Store.Set has read an empty secret as delete
				// since it existed); anything else replaces it. A plain
				// string could express two of those, and the one it would
				// have to give up is the common one.
				Key *string `json:"key"`
				// Serve is a pointer for the same reason Key is: a form that
				// only edited the address must not carry the switch back to
				// whatever it happened to be when that form was drawn.
				Serve *bool `json:"serve"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// SetPartial rather than a read-modify-write of the whole
			// document, for the reason PATCH /api/settings' own comment
			// spells out: this route must not carry every other setting a
			// concurrent editor is changing back to whatever this caller last
			// saw. Sanitising the address - trim, no trailing slash - is
			// sanitizeRelay's job on the way to disk, not this handler's.
			patch, err := json.Marshal(body.RelayURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fields := map[string]json.RawMessage{"relayUrl": patch}
			if body.Serve != nil {
				serve, err := json.Marshal(*body.Serve)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				fields["relayServe"] = serve
			}
			if _, err := a.Settings.SetPartial(fields); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if body.Key != nil {
				if err := a.Accounts.Set(relay.AccountService, *body.Key); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			applyRelay(a)
			writeJSON(w, relayConfigOf(a, srv))
		})
}

// relayConfigOf reads the configuration back out of the two stores that hold
// it, so GET and PUT can never describe it differently - PUT answers with
// what is now stored, not with what it was sent. A credential that will not
// decrypt reads as "no key set", the same reading app.accountRow already
// gives an unreadable one: there is nothing usable there either way, and the
// difference belongs in a log, not in a boolean the settings page draws a
// checkmark from.
func relayConfigOf(a *app.App, srv *relay.Server) relayConfig {
	key, err := a.Accounts.Get(relay.AccountService)
	cfg := a.Settings.Get()
	// The count is reported only while the switch is on. A relay just switched
	// off keeps whatever sockets were already open until each drops on its own,
	// and a number that outlived the switch would read as the feature still
	// running rather than as it finishing.
	clients := 0
	if cfg.RelayServe {
		clients = srv.Len()
	}
	return relayConfig{
		RelayURL:     cfg.RelayURL,
		KeySet:       err == nil && key != "",
		Connected:    a.Federation.RelayConnected(),
		Serve:        cfg.RelayServe,
		ServeClients: clients,
	}
}

// applyRelay (re)builds the relay client from whatever is currently stored
// and installs it on a.Federation, replacing (and so closing - see
// federation.Manager.SetRelay) whatever was there before. Called from two
// places: once at boot, as internal/api.Handler's own last step, so a
// configuration saved in an earlier run reconnects without anyone touching
// the settings page again; and once per PUT /api/relay/config, so pressing
// Save connects rather than only writing a file somebody has to restart the
// process to have read.
//
// Clearing the address or the key - or a client that fails to construct, or
// a.SelfServeHandler() not being ready yet, which only happens if this is
// somehow called before Handler finishes wiring itself - all take the same
// path: SetRelay(nil). A failure to CONNECT is never logged as more than
// that either, and never returned to a PUT caller: the save itself succeeded,
// and a relay that happens to be unreachable right now must not read as
// "your settings were rejected" - the whole premise of making the relay
// optional and self-hosted is that its outages never touch this instance's
// own operation, so a client left to keep retrying in the background is
// exactly the right outcome, not an error surfaced to whoever just saved an
// address.
// relayTarget answers the three questions applyRelay needs: which relay to
// dial, with which key, and under which key its frames are sealed.
//
// The seed phrase comes first. Once somebody has activated remote access,
// the secret their phrase decodes to is the whole configuration - the
// address is relay.DefaultRelayURL unless they deliberately pointed this
// instance at their own relay, and the key is derived, never stored or sent
// as the secret itself (see relay.DeriveKey).
//
// The frame key is derived from that same secret under a DIFFERENT domain
// (relay.DeriveFrameKey), which is what lets the relay hold one and never
// compute the other. It is the whole basis of the claim the connection card
// makes about a relay not being able to read what passes through it.
//
// The hand-entered relay key remains as the second path, unchanged, for a
// self-hosted relay somebody set up before the phrase existed or prefers to
// keep configuring by hand. Neither path knows about the other: an instance
// has a seed or it does not. That path has no secret to derive from, so its
// frame key comes from the relay key itself - a weaker guarantee, spelled
// out in full at relay.FrameKeyFromRelayKey, and one no UI text claims.
func relayTarget(a *app.App) (url, key string, frameKey []byte) {
	override := a.Settings.Get().RelayURL

	if secretHex, err := a.Accounts.Get(relay.SeedAccountService); err == nil && secretHex != "" {
		secret, err := hex.DecodeString(secretHex)
		if err != nil || len(secret) != seedphrase.SecretLen {
			// Sealed but unusable. Loud, because the instance will now sit
			// there looking configured while reaching nothing, and the fix
			// (re-enter the phrase) is not one anybody guesses from silence.
			log.Printf("relay: the stored connection secret is malformed, remote access is off until the phrase is entered again")
			return "", "", nil
		}
		if override != "" {
			return override, relay.DeriveKey(secret), relay.DeriveFrameKey(secret)
		}
		return relay.DefaultRelayURL, relay.DeriveKey(secret), relay.DeriveFrameKey(secret)
	}

	manual, err := a.Accounts.Get(relay.AccountService)
	if err != nil {
		log.Printf("relay: the stored key could not be read, connecting without one: %v", err)
	}
	if manual == "" {
		return override, "", nil
	}
	return override, manual, relay.FrameKeyFromRelayKey(manual)
}

func applyRelay(a *app.App) {
	relayURL, key, frameKey := relayTarget(a)
	serve := a.SelfServeHandler()
	if relayURL == "" || key == "" || serve == nil {
		a.Federation.SetRelay(nil)
		return
	}

	cfg := a.Settings.Get()
	c, err := relay.NewClient(relay.ClientOptions{
		URL:      relayURL,
		Key:      key,
		FrameKey: frameKey,
		Self: relay.Announce{
			InstanceID: cfg.InstanceID,
			Name:       instanceDisplayName(a),
			Deployment: buildinfo.Deployment,
		},
		Serve: relayProxyHandler(serve),
	})
	if err != nil {
		log.Printf("relay: could not configure the client: %v", err)
		a.Federation.SetRelay(nil)
		return
	}
	c.Start()
	a.Federation.SetRelay(c)
}

// relayProxyHandler turns one inbound relay call into a normal request
// against serve - this instance's own fully-wired HTTP handler, the same one
// a browser tab or an API token reaches. A relay-visible sibling therefore
// sees exactly the same routes and behaviour a direct HTTP peer already
// does.
//
// # Why a sibling is authenticated, and what that costs
//
// Anything arriving here came off a socket the relay only joins to other
// connections presenting the SAME group key - the key derived from the
// connection phrase. So the sender has already proved group membership
// before this function runs, and the request is marked as such.
//
// That mark is what makes a password-protected instance usable from its own
// siblings. It used to answer 401 to all of them, because the phrase and the
// credential were unrelated things and only a separate pairing exchange
// closed the gap. It is not a loosening: whoever holds the phrase can join
// the group anyway, and every screen that hands one out says so.
//
// What it does mean is that the surface reachable this way has to be the
// narrow one, which is enforced HERE rather than left to each route. The set
// is exactly what the outbound half is willing to forward (see the
// /api/instances/{name}/{rest...} route): tasks, links and the queue. A
// sibling cannot read this instance's accounts, change its password, mint an
// API token or ask for the phrase back.
func relayProxyHandler(serve http.Handler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		if !relayForwardable(call.Method, call.Path) {
			// Refused before the handler sees it, so a route added later is
			// not silently exposed to peers by existing: this list is an
			// allowlist and a new route is outside it until somebody says
			// otherwise.
			return http.StatusForbidden, []byte("route not proxied")
		}
		var body io.Reader
		if len(call.Body) > 0 {
			body = bytes.NewReader(call.Body)
		}
		httpReq, err := http.NewRequestWithContext(ctx, call.Method, call.Path, body)
		if err != nil {
			return http.StatusBadRequest, []byte(err.Error())
		}
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), relayGroupKey, true))
		if len(call.Body) > 0 {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		// Set, never appended to: the frame is the only source of this header,
		// so a caller cannot stack a second value onto one the relay path
		// might otherwise have added. An empty field leaves the request
		// unauthenticated, which is what every relay call was before the
		// field existed and still is for instance-to-instance traffic.
		if call.Authorization != "" {
			httpReq.Header.Set("Authorization", call.Authorization)
		}
		rec := newRelayRecorder()
		serve.ServeHTTP(rec, httpReq)
		return rec.status, rec.body.Bytes()
	}
}

// relayForwardable is the one list of what a group sibling may reach on this
// instance.
//
// It is deliberately NOT the same list as the outbound web-proxy filter on
// /api/instances/{name}/{rest...}. That one is what a BROWSER may ask this
// instance to relay onward, and it is narrower. This one is what any group
// member may ask of us, and the phone app is a group member too: it needs to
// know whether it reached something alive, which instances are in the group,
// and what the instance looks like, none of which a browser ever asks a peer
// for. The narrow list is a subset of this one, which is the direction that
// is safe.
//
// The task, link and queue routes carry any method - the queue travels with
// the task list because it is that list's master switch, and showing a
// sibling's downloads while being unable to stop them is a half-connected
// instance. Everything else here is GET only: a sibling may look, never
// change. Settings, accounts, tokens, scripts and the phrase are outside the
// list entirely.
func relayForwardable(method, path string) bool {
	// Query strings are part of a task listing's own vocabulary (filters,
	// paging); the decision here is about the route, not its arguments.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	const prefix = "/api/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]

	if rest == "links" || rest == "tasks" || rest == "queue" ||
		strings.HasPrefix(rest, "tasks/") || strings.HasPrefix(rest, "queue/") {
		return true
	}
	// Read-only, and each for a reason the phone app would otherwise have to
	// do without: "did I reach something, and does it want a password",
	// "which instances are in this group", and the seven cosmetic fields that
	// let the app wear the instance's own accent. /api/appearance exists
	// precisely so this last one is not a licence to read /api/settings.
	if method == http.MethodGet {
		return rest == "auth" || rest == "instances" || rest == "appearance"
	}
	return false
}

// relayRecorder buffers one handler's response in memory - the smallest
// http.ResponseWriter this needs. net/http/httptest.ResponseRecorder would
// do the same job, but it is a testing helper (see its own package doc), not
// something a real, non-test request/response cycle should depend on.
type relayRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newRelayRecorder() *relayRecorder {
	return &relayRecorder{header: http.Header{}, status: http.StatusOK}
}

func (r *relayRecorder) Header() http.Header         { return r.header }
func (r *relayRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *relayRecorder) WriteHeader(status int)      { r.status = status }
