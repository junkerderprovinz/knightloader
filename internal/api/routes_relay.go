package api

// The relay configuration: the address this instance dials out to, kept in
// settings.json, and the key that authorises it there, sealed in
// internal/accounts under relay.AccountService. The key is never answered
// with, and it does not ride on /api/settings, which hands the whole document
// back.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
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
		return a.Settings.Get().RelayServe && servesKey(a, key)
	}
	// The group left at a reset keeps its relay key for its devices to hear
	// that, and for nothing else: on it they reach this instance alone.
	srv.Confine = func(key string) string {
		g, ok := retiredOf(a, time.Now())
		if !ok || subtle.ConstantTimeCompare([]byte(key), []byte(g.Key)) != 1 {
			return ""
		}
		return idOf(a, g.member())
	}
	// A hello proves an id only to the relay it names. This one goes by the
	// name a connection reached it under, when that is a name this instance
	// answers on, and by its known domains, which stay right behind a proxy
	// that rewrites the Host header.
	srv.Names = func(r *http.Request) []string {
		var names []string
		if hostAllowed(a, r.Host) {
			names = append(names, relay.RelayHost(r.Host))
		}
		for _, d := range a.Settings.Get().KnownDomains {
			names = append(names, relay.RelayHost(d))
		}
		return names
	}
	srv.TrustedProxies = trustedProxies(a)
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

	// A member on the same network calls here instead of over the relay. The
	// route is open like the relay socket: the call is signed and sealed with
	// keys only members hold, and answers 404 outside a group.
	reg.AddOpen(http.MethodPost, federation.DirectPath,
		"a call from another member of this instance's phrase group on the same network - authorised by the group's keys, never by a session",
		func(w http.ResponseWriter, r *http.Request) {
			serve := a.SelfServeHandler()
			if serve == nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			a.Federation.ServeDirect(w, r, a.Federation.Guard(announceArrivals(a, relayProxyHandler(serve))))
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
				before, _ := a.Accounts.Get(relay.AccountService)
				if err := a.Accounts.Set(relay.AccountService, *body.Key); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if before != *body.Key && groupSecret(a) == nil {
					if err := enterManualGroup(a, *body.Key != ""); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				}
			}
			applyRelay(a)
			writeJSON(w, relayConfigOf(a, srv))
		})
}

// enterManualGroup starts the group a hand-entered relay key makes, or leaves
// it when joined is false. Its instances sign their calls and approve each
// other as in a group reached by the words, under a member key of its own.
func enterManualGroup(a *app.App, joined bool) error {
	if err := a.Accounts.Set(relay.MemberAccountService, ""); err != nil {
		return err
	}
	at := time.Time{}
	if joined {
		at = time.Now()
	}
	return a.Federation.SetJoined(at)
}

// servesKey reports whether the relay this instance serves admits key: the
// hand-entered relay key, or the one its phrase group dials with (see
// relayTarget).
func servesKey(a *app.App, key string) bool {
	var served []string
	if manual, err := a.Accounts.Get(relay.AccountService); err == nil && manual != "" {
		served = append(served, manual)
	}
	if secret := groupSecret(a); secret != nil {
		served = append(served, relay.DeriveKey(secret))
	}
	if g, ok := retiredOf(a, time.Now()); ok {
		served = append(served, g.Key)
	}
	ok := false
	for _, s := range served {
		// Constant time, because the caller controls the guess.
		if subtle.ConstantTimeCompare([]byte(key), []byte(s)) == 1 {
			ok = true
		}
	}
	return ok
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
	applyGroup(a)
	applyRetired(a)
	relayURL, key, frameKey := relayTarget(a)
	serve := a.SelfServeHandler()
	if relayURL == "" || key == "" || serve == nil {
		a.Federation.SetRelay(nil)
		return
	}

	member := memberKey(a)
	c, err := relay.NewClient(relay.ClientOptions{
		URL:      relayURL,
		Key:      key,
		FrameKey: frameKey,
		Self: relay.Announce{
			InstanceID: idOf(a, member),
			Name:       instanceDisplayName(a),
			Deployment: instanceKind(),
			Address:    groupAddress(a),
		},
		Member: member,
		Trusts: a.Federation.TrustsAnswer,
		Serve:  a.Federation.Guard(announceArrivals(a, relayProxyHandler(serve))),
	})
	if err != nil {
		log.Printf("relay: could not configure the client: %v", err)
		a.Federation.SetRelay(nil)
		return
	}
	c.Start()
	a.Federation.SetRelay(c)
}

// applyGroup hands the stored group secret to the direct transport, which
// works whichever relay is chosen, none included.
func applyGroup(a *app.App) {
	member := memberKey(a)
	a.Federation.SetGroup(groupSecret(a), idOf(a, member), member)
}

// memberMu keeps two callers from minting two member keys at once.
var memberMu sync.Mutex

// memberKey is the key this instance signs its calls to the group with, nil
// outside a group. A group reached by a hand-entered relay key signs the same
// way as one reached by the words. The key is minted here the first time it is
// needed.
func memberKey(a *app.App) ed25519.PrivateKey {
	if manual, _ := a.Accounts.Get(relay.AccountService); groupSecret(a) == nil && manual == "" {
		return nil
	}
	memberMu.Lock()
	defer memberMu.Unlock()
	if stored, err := a.Accounts.Get(relay.MemberAccountService); err == nil && stored != "" {
		if seed, err := hex.DecodeString(stored); err == nil && len(seed) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(seed)
		}
		log.Printf("relay: the stored member key is malformed, minting a new one; the group pins it as a new key")
	}
	seed, err := relay.NewMemberSeed()
	if err == nil {
		err = a.Accounts.Set(relay.MemberAccountService, hex.EncodeToString(seed))
	}
	if err != nil {
		log.Printf("relay: no member key, the group refuses this instance's calls until one can be made: %v", err)
		return nil
	}
	return ed25519.NewKeyFromSeed(seed)
}

// idOf is the id this instance goes by with member: the one the key makes
// (relay.MemberID), or its own instance id without one.
func idOf(a *app.App, member ed25519.PrivateKey) string {
	if member == nil {
		return a.Settings.Get().InstanceID
	}
	return relay.MemberID(relay.KindInstance, member.Public().(ed25519.PublicKey))
}

// groupID is the id the group knows this instance by.
func groupID(a *app.App) string { return idOf(a, memberKey(a)) }

// groupSecret is the stored connection secret of this instance's phrase
// group, nil outside a group or when the stored value is malformed.
func groupSecret(a *app.App) []byte {
	secretHex, err := a.Accounts.Get(relay.SeedAccountService)
	if err != nil || secretHex == "" {
		return nil
	}
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) != seedphrase.SecretLen {
		return nil
	}
	return secret
}

// relayProxyHandler turns one inbound relay call into a request against serve,
// the same handler a browser or an API token reaches.
//
// The relay only joins sockets presenting the same group key, derived from the
// connection phrase, so the sender has proved group membership and the request
// is marked as coming from the group. Whoever holds the phrase can join the
// group anyway. In return guard holds the call to groupRoutes, so a member
// cannot read accounts, change the password, mint a token or ask for the
// phrase. Who the sender is, and whether it was taken out, is the guard's to
// decide (federation.Manager.Guard) before this runs.
func relayProxyHandler(serve http.Handler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
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
			if rec.tooLong {
				return http.StatusBadGateway, []byte(errRelayAnswerTooLong.Error())
			}
			return rec.status, rec.body.Bytes()
		}()
		return status, out
	}
}

// relayAnswerMax bounds what one relayed call answers. No member reads more
// than 32 MiB of an answer, sealed and encoded, so a larger one, such as a
// finished file, could never arrive and would only fill this instance's memory.
const relayAnswerMax = 32 << 20

// relayRecorder buffers one handler's response in memory, so production code
// does not depend on httptest.
type relayRecorder struct {
	header  http.Header
	status  int
	body    bytes.Buffer
	tooLong bool
}

func newRelayRecorder() *relayRecorder {
	return &relayRecorder{header: http.Header{}, status: http.StatusOK}
}

// errRelayAnswerTooLong stops a handler that writes past relayAnswerMax.
var errRelayAnswerTooLong = errors.New("answer too large to pass on")

func (r *relayRecorder) Header() http.Header    { return r.header }
func (r *relayRecorder) WriteHeader(status int) { r.status = status }

func (r *relayRecorder) Write(b []byte) (int, error) {
	if r.tooLong || r.body.Len()+len(b) > relayAnswerMax {
		r.tooLong = true
		return 0, errRelayAnswerTooLong
	}
	return r.body.Write(b)
}
