package api

// The relay configuration: the address this instance dials out to, kept in
// settings.json, and the key that authorises it there, sealed in
// internal/accounts under relay.AccountService. The key is never answered
// with, and it does not ride on /api/settings, which hands the whole document
// back.

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
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// relayConfig is what both GET and PUT /api/relay/config answer with: the
// address, and the one bit of the key anybody is entitled to know.
type relayConfig struct {
	RelayURL string `json:"relayUrl"`
	KeySet   bool   `json:"keySet"`
	// Connected is whether the socket to that relay is up right now, which a
	// stored address and key alone cannot tell.
	Connected bool `json:"connected"`
	// Serve is whether this instance is itself running the relay, under
	// /relay/connect on its own address.
	Serve bool `json:"serve"`
	// ServeClients is how many instances are connected to it right now, this
	// one included if it dials its own relay. Zero while Serve is false.
	ServeClients int `json:"serveClients"`
	// Mode is which relay this instance uses: "project", "own" or "off".
	// settings.RelayModeOf resolves the empty value older installs store.
	Mode string `json:"mode"`
}

func registerRelay(reg *Registry, a *app.App) {
	// The relay socket is open: instances dialling in have no session here,
	// and the relay key in the first frame is the credential. One Server lives
	// for the whole process with the switch read per connection, so saving an
	// unrelated setting does not drop connected siblings.
	srv := relay.New()
	srv.Admit = func(key string) bool {
		if !a.Settings.Get().RelayServe {
			return false
		}
		stored, err := a.Accounts.Get(relay.AccountService)
		if err != nil || stored == "" {
			return false
		}
		// Constant time, because the caller controls the guess.
		return subtle.ConstantTimeCompare([]byte(key), []byte(stored)) == 1
	}
	reg.AddOpen(http.MethodGet, "/relay/connect",
		"the relay socket, when this instance is serving one - authorised by the relay key in the first frame, never by a session",
		func(w http.ResponseWriter, r *http.Request) {
			// With the switch off this instance is not a relay, so it answers
			// like a version without the feature.
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
				// Key is absent to keep the stored key, "" to clear it, and
				// anything else to replace it.
				Key *string `json:"key"`
				// Serve and Mode are pointers so a save from one control does
				// not carry the others back.
				Serve *bool   `json:"serve"`
				Mode  *string `json:"mode"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// SetPartial, so a concurrent edit to other settings survives.
			// sanitizeRelay trims the address on the way to disk.
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
			if body.Mode != nil {
				// An unknown mode would be stored and then fall through to
				// RelayModeOf's legacy inference, so it is refused.
				switch *body.Mode {
				case settings.RelayModeProject, settings.RelayModeOwn, settings.RelayModeOff:
				default:
					http.Error(w, "relay mode must be project, own or off", http.StatusBadRequest)
					return
				}
				mode, err := json.Marshal(*body.Mode)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				fields["relayMode"] = mode
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

// relayConfigOf reads the configuration back from the two stores, so PUT
// answers with what is stored. A key that will not decrypt reads as not set,
// as in app.accountRow.
func relayConfigOf(a *app.App, srv *relay.Server) relayConfig {
	key, err := a.Accounts.Get(relay.AccountService)
	cfg := a.Settings.Get()
	// Sockets opened before the switch went off linger until they drop, so the
	// count is only reported while it is on.
	clients := 0
	if cfg.RelayServe {
		clients = srv.Len()
	}
	return relayConfig{
		Mode:         cfg.RelayModeOf(),
		RelayURL:     cfg.RelayURL,
		KeySet:       err == nil && key != "",
		Connected:    a.Federation.RelayConnected(),
		Serve:        cfg.RelayServe,
		ServeClients: clients,
	}
}

// relayTarget answers which relay applyRelay dials, with which key, and under
// which key the frames are sealed.
//
// A stored connection secret from the seed phrase comes first: the relay key
// and the frame key are derived from it under different domains
// (relay.DeriveKey, relay.DeriveFrameKey), so the relay can hold one and never
// compute the other. Without a seed, a hand-entered relay key is used, and the
// frame key comes from it (relay.FrameKeyFromRelayKey), a weaker guarantee no
// UI text claims.
func relayTarget(a *app.App) (url, key string, frameKey []byte) {
	cfg := a.Settings.Get()
	// Checked before any credential is read, so switching the relay off does
	// not log warnings about the stored secret.
	if cfg.RelayModeOf() == settings.RelayModeOff {
		return "", "", nil
	}
	// Only "own" honours the stored address; in project mode the field may
	// still hold an address somebody switched away from.
	override := ""
	if cfg.RelayModeOf() == settings.RelayModeOwn {
		override = cfg.RelayURL
		// Own relay without an address dials nothing rather than falling back
		// to the project relay.
		if strings.TrimSpace(override) == "" {
			log.Printf("relay: own relay is selected but no address is set, nothing is dialled")
			return "", "", nil
		}
	}

	if secretHex, err := a.Accounts.Get(relay.SeedAccountService); err == nil && secretHex != "" {
		secret, err := hex.DecodeString(secretHex)
		if err != nil || len(secret) != seedphrase.SecretLen {
			// Logged, because the instance looks configured while reaching
			// nothing.
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

// applyRelay rebuilds the relay client from what is stored and installs it on
// a.Federation, which closes the previous one. It runs at boot and after every
// relay or name change. A relay that cannot be reached is not an error for the
// caller; the client keeps retrying in the background.
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

// relayProxyHandler turns one inbound relay call into a request against serve,
// the same handler a browser or an API token reaches.
//
// The relay only joins sockets presenting the same group key, derived from the
// connection phrase, so the sender has proved group membership and the request
// is marked authenticated. Whoever holds the phrase can join the group anyway.
// In return the reachable surface is limited here by relayForwardable, so a
// sibling cannot read accounts, change the password, mint a token or ask for
// the phrase.
func relayProxyHandler(serve http.Handler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		if !relayForwardable(call.Method, call.Path) {
			// An allowlist, so a route added later is not exposed to peers
			// until somebody adds it.
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
		// Set rather than added, so the frame is the header's only source.
		if call.Authorization != "" {
			httpReq.Header.Set("Authorization", call.Authorization)
		}
		rec := newRelayRecorder()
		// There is no net/http server here to recover a panicking handler, and
		// this runs on the goroutine reading relay frames, so a panic would take
		// the whole instance down.
		status, out := func() (s int, b []byte) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("relay: handler panicked serving %s %s: %v", call.Method, call.Path, r)
					s, b = http.StatusInternalServerError, []byte("internal error")
				}
			}()
			serve.ServeHTTP(rec, httpReq)
			return rec.status, rec.body.Bytes()
		}()
		return status, out
	}
}

// relayForwardable is the list of what a group sibling may reach on this
// instance. It is wider than the browser's outbound filter on
// /api/instances/{name}/{rest...}, because the phone app is a group member too
// and asks things a browser never asks a peer.
//
// The task, link and queue routes take any method, since showing a sibling's
// downloads without being able to stop them would be half a connection. So do
// the captcha routes: a captcha holds a download up, and the phone answers or
// skips it. The rest is read-only apart from the appearance fields. Settings,
// accounts, tokens, scripts and the phrase are not on the list.
func relayForwardable(method, path string) bool {
	// The decision is about the route, not its query arguments.
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
	if relayCaptchaRoute(method, rest) {
		return true
	}
	// Setting the seven appearance fields is less than a phrase holder can
	// already do through the queue.
	if method == http.MethodPost && rest == "appearance" {
		return true
	}
	// Reads a companion needs: whether a password is wanted, the group's
	// instances, the look, and the addresses this instance answers on. The
	// addresses travel inside the encrypted frame, so the relay operator
	// never learns them.
	if method == http.MethodGet {
		return rest == "auth" || rest == "instances" || rest == "appearance" || rest == "remote-access"
	}
	return false
}

// relayCaptchaRoute reports whether rest is one of the captcha calls the phone
// makes: the list, a refresh, and answering or skipping one challenge. Matched
// by shape rather than by prefix, so a route added under /api/captcha later is
// not forwarded until it is named here.
func relayCaptchaRoute(method, rest string) bool {
	switch {
	case method == http.MethodGet && rest == "captcha":
		return true
	case method != http.MethodPost:
		return false
	case rest == "captcha/refresh":
		return true
	}
	parts := strings.Split(rest, "/")
	return len(parts) == 3 && parts[0] == "captcha" && parts[1] != "" &&
		(parts[2] == "answer" || parts[2] == "skip")
}

// relayRecorder buffers one handler's response in memory, so production code
// does not depend on httptest.
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
