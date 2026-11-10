package api

// The relay configuration: which relay this instance's group dials, kept in
// settings.json, and the group itself, opened from the phrase sealed in
// internal/accounts under federation.SeedAccount. The phrase never rides on
// /api/settings, which hands the whole document back.

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// relayConfig is what both GET and PUT /api/relay/config answer with.
type relayConfig struct {
	RelayURL string `json:"relayUrl"`
	// Connected is whether the socket to that relay is up right now, which a
	// stored address alone cannot tell.
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

// maxAnswer is the most a member answers a direct call with. The Overview of
// a busy instance lists thousands of tasks, which a relay frame cannot carry
// but the network next door can.
const maxAnswer = 32 << 20

func registerRelay(reg *Registry, a *app.App) {
	dropRelayKey(a)

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
		return idOf(a, g.identity())
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
	reg.AddOpen(http.MethodPost, group.DirectPath,
		"a call from another member of this instance's phrase group on the same network - authorised by the group's keys, never by a session",
		func(w http.ResponseWriter, r *http.Request) {
			g := a.Federation.Group()
			if g == nil {
				http.NotFound(w, r)
				return
			}
			g.DirectHandler().ServeHTTP(w, r)
		})

	reg.Add(http.MethodGet, "/api/relay/config",
		"the relay this instance's group dials, and whether it is connected",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, relayConfigOf(a, srv))
		})

	reg.Add(http.MethodPut, "/api/relay/config",
		"set the relay address, the relay mode and whether this instance serves a relay; saving reconnects the group",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				RelayURL string `json:"relayUrl"`
				// Key is the relay key older releases took by hand.
				Key *string `json:"key"`
				// Serve and Mode are pointers so a save from one control does
				// not carry the others back.
				Serve *bool   `json:"serve"`
				Mode  *string `json:"mode"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if body.Key != nil && *body.Key != "" {
				writeRefusal(w, http.StatusBadRequest, "relayKeyGone",
					"a relay key typed in by hand no longer pairs instances; pair them with the phrase instead", nil)
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
			applyRelay(a)
			writeJSON(w, relayConfigOf(a, srv))
		})
}

// dropRelayKey forgets a relay key typed in by hand, which older releases
// paired instances by without a phrase. An instance that was in such a
// group is left alone in none, and its Pairing card says why.
func dropRelayKey(a *app.App) {
	manual, err := a.Accounts.Get(federation.RelayKeyAccount)
	if err != nil || manual == "" {
		return
	}
	if err := a.Accounts.Set(federation.RelayKeyAccount, ""); err != nil {
		log.Printf("relay: could not forget the relay key typed in by hand: %v", err)
		return
	}
	if groupSecret(a) != nil {
		return
	}
	log.Printf("relay: a relay key typed in by hand no longer pairs instances; pair this one again with the phrase")
	if err := a.Accounts.Set(federation.MemberAccount, ""); err != nil {
		log.Printf("relay: could not forget the member key of the group the relay key made: %v", err)
	}
	if err := a.Federation.DropRelayKey(); err != nil {
		log.Printf("relay: could not note that the relay key typed in by hand is gone: %v", err)
	}
}

// servesKey reports whether the relay this instance serves admits key: the
// one its phrase group dials with, or the one of the group it left at a
// reset.
func servesKey(a *app.App, key string) bool {
	var served []string
	if secret := groupSecret(a); secret != nil {
		served = append(served, federation.Domain.GroupKey(secret))
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

// relayConfigOf reads the configuration back from the settings, so PUT
// answers with what is stored.
func relayConfigOf(a *app.App, srv *relay.Server) relayConfig {
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
		Connected:    a.Federation.RelayConnected(),
		Serve:        cfg.RelayServe,
		ServeClients: clients,
	}
}

// relayOf is the relay the group dials: the project's, the own one at the
// stored address, or none. Only "own" honours the stored address; in project
// mode the field may still hold an address somebody switched away from.
func relayOf(a *app.App) (group.RelayMode, string) {
	cfg := a.Settings.Get()
	switch cfg.RelayModeOf() {
	case settings.RelayModeOff:
		return group.RelayOff, ""
	case settings.RelayModeOwn:
		// Own relay without an address dials nothing rather than falling back
		// to the project relay.
		if strings.TrimSpace(cfg.RelayURL) == "" {
			log.Printf("relay: own relay is selected but no address is set, nothing is dialled")
			return group.RelayOff, ""
		}
		return group.RelayOwn, cfg.RelayURL
	}
	return group.RelayProject, relay.DefaultURL
}

// applyRelay opens this instance's group again from what is stored. It runs
// at boot and after every relay or name change. A relay that cannot be
// reached is not an error for the caller, since the group keeps trying in the
// background.
func applyRelay(a *app.App) {
	if err := regroup(a, nil); err != nil {
		log.Printf("relay: could not open the group: %v", err)
	}
}

// regroup closes the group, runs change, when there is one, and opens the
// group from what is stored then. Outside a group this instance announces
// itself on its network untagged, so the others can still offer to add it.
func regroup(a *app.App, change func() error) error {
	applyRetired(a)
	err := a.Federation.Reopen(func() (*group.Group, error) {
		var changeErr error
		if change != nil {
			changeErr = change()
		}
		g, err := openGroup(a)
		if changeErr != nil {
			return g, changeErr
		}
		return g, err
	})
	if svc := a.Discovery(); svc != nil && a.Federation.Group() == nil {
		svc.SetAccept(nil)
		svc.SetSelf(discoverySelf(a), nil)
	}
	return err
}

// openGroup opens the group the stored phrase makes, nil outside one. A group
// that has never been opened here takes over what group.json holds from
// releases before kit/group.
func openGroup(a *app.App) (*group.Group, error) {
	secret := groupSecret(a)
	serve := a.SelfServeHandler()
	if secret == nil || serve == nil {
		return nil, nil
	}
	identity := memberKey(a)
	if identity == nil {
		return nil, nil
	}
	store := groupStore{a.Accounts}
	kept, err := store.Load()
	if err != nil {
		return nil, err
	}
	mode, url := relayOf(a)
	g, err := group.Open(group.Config{
		Domain:   federation.Domain,
		Signing:  federation.Signing,
		Secret:   secret,
		Identity: *identity,
		Self:     groupSelf(a),
		Relay:    mode,
		RelayURL: url,
		LAN:      a.Discovery(),
		Store:    store,
		// The owner of an instance that was taken out leaves only the group
		// it was taken out of: a reset in between put it in a group of its
		// own, which nobody else may make it leave.
		OnRemoved: func() {
			err := a.Auth.Mint(func() bool { return subtle.ConstantTimeCompare(groupSecret(a), secret) == 1 },
				func() error { return enterGroup(a, nil) })
			if err != nil {
				log.Printf("connect: did not leave the group after an instance of it took this one out: %v", err)
			}
		},
		Kinds:          []member.Kind{member.KindMobile, member.KindExtension},
		Ports:          federation.LAN.Ports,
		MaxAnswerBytes: maxAnswer,
		OnChange: func() {
			a.Hub.Broadcast("group", nil)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			a.Federation.SyncLegacy(ctx, time.Now())
		},
	}, memberHandler(a, serve))
	if err != nil {
		return nil, err
	}
	if len(kept) == 0 {
		if old, ok := a.Federation.OldGroup(); ok {
			if err := g.Import(old); err != nil {
				log.Printf("relay: could not keep the group this instance was in before the update: %v", err)
			}
		}
	}
	return g, nil
}

// groupSelf is what the other members see of this instance.
func groupSelf(a *app.App) group.Self {
	return group.Self{
		Name:       instanceDisplayName(a),
		Deployment: instanceKind(),
		Address:    groupAddress(a),
		DirectURL:  networkAddress(),
	}
}

// groupStore keeps what kit/group knows of the group sealed in the credential
// store, beside the phrase it belongs to.
type groupStore struct{ accounts *accounts.Store }

func (s groupStore) Load() ([]byte, error) {
	v, err := s.accounts.Get(federation.GroupAccount)
	return []byte(v), err
}

func (s groupStore) Save(b []byte) error {
	return s.accounts.Set(federation.GroupAccount, string(b))
}

// memberHandler serves a member's call with serve, the same handler a browser
// or an API token reaches, marked as coming from the group, and tells the open
// pages when a member handed links over.
//
// Whoever holds the phrase can join the group, so serve holds such a call to
// groupRoutes: a member cannot read accounts, change the password, mint a
// token or ask for the phrase. Who the member is, and whether it was taken
// out, kit/group decided before this runs.
func memberHandler(a *app.App, serve http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := group.CallerOf(r.Context())
		serveMember(a, serve, c, w, r)
	})
}

// serveMember serves r, a call member c made, with serve.
func serveMember(a *app.App, serve http.Handler, c group.Caller, w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), memberCallKey{}, c))
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	serve.ServeHTTP(sw, r)
	if addsLinks(r.Method, r.URL.Path) && sw.status/100 == 2 {
		a.Hub.Broadcast("handover", handover{Peer: c.ID})
	}
}

// statusWriter remembers the status a handler answered with.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// memberMu keeps two callers from minting two member keys at once.
var memberMu sync.Mutex

// memberKey is the key this instance signs its calls to the group with, nil
// outside a group. The key is minted here the first time it is needed.
func memberKey(a *app.App) *member.Identity {
	if groupSecret(a) == nil {
		return nil
	}
	memberMu.Lock()
	defer memberMu.Unlock()
	if stored, err := a.Accounts.Get(federation.MemberAccount); err == nil && stored != "" {
		if seed, err := hex.DecodeString(stored); err == nil {
			if id, err := member.FromSeed(member.KindInstance, seed); err == nil {
				return &id
			}
		}
		log.Printf("relay: the stored member key is malformed, minting a new one; the group pins it as a new key")
	}
	id, err := member.NewIdentity(member.KindInstance)
	if err == nil {
		err = a.Accounts.Set(federation.MemberAccount, hex.EncodeToString(id.Seed()))
	}
	if err != nil {
		log.Printf("relay: no member key, the group refuses this instance's calls until one can be made: %v", err)
		return nil
	}
	return &id
}

// idOf is the id this instance goes by with identity: the one its key makes,
// or its own instance id without one.
func idOf(a *app.App, identity *member.Identity) string {
	if identity == nil {
		return a.Settings.Get().InstanceID
	}
	return identity.ID()
}

// groupID is the id the group knows this instance by.
func groupID(a *app.App) string { return idOf(a, memberKey(a)) }

// groupSecret is the stored connection secret of this instance's phrase
// group, nil outside a group or when the stored value is malformed.
func groupSecret(a *app.App) []byte {
	secretHex, err := a.Accounts.Get(federation.SeedAccount)
	if err != nil || secretHex == "" {
		return nil
	}
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) != phrase.SecretLen {
		return nil
	}
	return secret
}
