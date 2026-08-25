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
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/relay"
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
func applyRelay(a *app.App) {
	relayURL := a.Settings.Get().RelayURL
	key, err := a.Accounts.Get(relay.AccountService)
	if err != nil {
		log.Printf("relay: the stored key could not be read, connecting without one: %v", err)
	}
	serve := a.SelfServeHandler()
	if relayURL == "" || key == "" || serve == nil {
		a.Federation.SetRelay(nil)
		return
	}

	cfg := a.Settings.Get()
	c, err := relay.NewClient(relay.ClientOptions{
		URL: relayURL,
		Key: key,
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
// does, including the one limitation that comes with it: like
// federation.Manager's own direct-HTTP transport, which has never attached a
// credential of its own to an outgoing peer call, a relay-proxied request
// carries no session cookie or API token either - only the relay key that
// got it here at all. An instance with a password set already refuses an
// unauthenticated direct-HTTP peer call today; this is the same fact over
// the second transport, not a new one.
func relayProxyHandler(serve http.Handler) relay.ProxyHandler {
	return func(ctx context.Context, req relay.ProxyRequest) (int, []byte) {
		var body io.Reader
		if len(req.Body) > 0 {
			body = bytes.NewReader(req.Body)
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.Path, body)
		if err != nil {
			return http.StatusBadRequest, []byte(err.Error())
		}
		if len(req.Body) > 0 {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		// Set, never appended to: the frame is the only source of this header,
		// so a caller cannot stack a second value onto one the relay path
		// might otherwise have added. An empty field leaves the request
		// unauthenticated, which is what every relay call was before the
		// field existed and still is for instance-to-instance traffic.
		if req.Authorization != "" {
			httpReq.Header.Set("Authorization", req.Authorization)
		}
		rec := newRelayRecorder()
		serve.ServeHTTP(rec, httpReq)
		return rec.status, rec.body.Bytes()
	}
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
