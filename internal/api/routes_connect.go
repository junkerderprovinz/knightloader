package api

// The connection phrase: generating one, showing it again, and joining a
// group another instance already started. There is no account: the project
// relay's address is compiled in (relay.DefaultRelayURL), an instance on its
// own relay names that relay in the QR code beside the phrase, and holding the
// secret is the whole authorization.
//
// The secret is stored and the phrase handed out; the relay only ever sees
// relay.DeriveKey of the secret, so neither its operator nor its memory can
// give back what a person would type.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// ConnectInfo is what GET /api/connect answers with. It never carries the
// phrase, which needs the password (see the reveal route).
type ConnectInfo struct {
	// Active is whether this instance has a connection secret at all.
	Active bool `json:"active"`
	// Connected is whether the relay socket is up right now; a stored secret
	// with an unreachable relay is configured but not working.
	Connected bool `json:"connected"`
	// PasswordSet is whether a login password is set. Without one anybody who
	// opens this web interface can show the phrase and so reach every member,
	// which the page says.
	PasswordSet bool `json:"passwordSet"`
	// RelayURL is the relay this instance dials: the compiled-in default or an
	// override.
	RelayURL string `json:"relayUrl"`
	// SelfHosted is whether RelayURL is an override.
	SelfHosted bool `json:"selfHosted"`
	// RelayMode is "project", "own" or "off", as in relayConfig. An instance
	// in "off" mode is not self-hosting anything, which SelfHosted alone
	// cannot say.
	RelayMode string `json:"relayMode"`
	// ProjectRelayURL is the compiled-in default in every mode, so the page
	// can say where the project relay is before anybody switches to it.
	ProjectRelayURL string `json:"projectRelayUrl"`
	// Name is what the other instances of the group see this one as, and
	// Address where they open it (groupAddress), "" when there is none.
	Name    string `json:"name"`
	Address string `json:"address"`
	// Members is the other instances of the group reachable now, and Apps
	// every phone that joined it with the phrase.
	Members []groupMember `json:"members"`
	Apps    []groupApp    `json:"apps"`
	// JoinedAgo is how many seconds ago this instance generated or entered
	// its phrase, and MemberSeen whether another instance has shown up since.
	// Together they tell a group waiting for its second instance from one
	// that never forms.
	JoinedAgo  int64 `json:"joinedAgo"`
	MemberSeen bool  `json:"memberSeen"`
	// Pending is every device whose key is pinned here and waits for
	// approval, and Awaiting whether another instance said lately that this
	// one waits for approval there.
	Pending  []pendingDevice `json:"pending"`
	Awaiting bool            `json:"awaiting"`
}

type pendingDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is "instance", "mobile" or "extension", as the device signs.
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
}

type groupMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Direct is true for a member on this network, Relay for one on the same
	// relay; a member can be both.
	Direct bool `json:"direct"`
	Relay  bool `json:"relay"`
}

type groupApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Deployment is "mobile" or "extension".
	Deployment string `json:"deployment"`
	Connected  bool   `json:"connected"`
	// LastSeen is Unix seconds.
	LastSeen int64 `json:"lastSeen"`
}

func registerConnect(reg *Registry, a *app.App) {
	// A stored secret without a joining time counts as joined at start, so
	// the page waits a minute before it says nobody came.
	if secret, _ := a.Accounts.Get(relay.SeedAccountService); secret != "" {
		if st, err := a.Federation.Group(false, time.Now()); err == nil && st.JoinedAt.IsZero() {
			if err := a.Federation.SetJoinedBeforeKeys(time.Now()); err != nil {
				log.Printf("connect: could not record when this instance joined its group: %v", err)
			}
		}
	}

	reg.Add(http.MethodGet, "/api/connect",
		"whether this instance has a connection phrase, who else is in its group, whether its relay socket is up, and which relay it uses",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/activate",
		"generate a new connection phrase for this instance and start dialling the relay - answers with the phrase, the only time it is returned without the password",
		func(w http.ResponseWriter, r *http.Request) {
			// Replacing an existing secret would orphan every instance joined
			// to the old phrase; leaving is an explicit DELETE.
			if existing, err := a.Accounts.Get(relay.SeedAccountService); err == nil && existing != "" {
				writeRefusal(w, http.StatusConflict, "phraseExists",
					"this instance already has a connection phrase; remove it first to start a new group", nil)
				return
			}
			secret, phrase, err := seedphrase.New()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := enterGroup(a, secret, false); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"phrase": phrase, "qr": phraseQR(a, phrase), "info": connectInfo(a)})
		})

	reg.Add(http.MethodPost, "/api/connect/join",
		"join the group a phrase belongs to - the other half of activate, for every instance after the first",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Phrase string `json:"phrase"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			secret, err := seedphrase.Decode(body.Phrase)
			if err != nil {
				// The reason and its details rather than a sentence, so the
				// browser can explain the mistyped word in the user's language.
				var de *seedphrase.DecodeError
				if errors.As(err, &de) {
					w.WriteHeader(http.StatusBadRequest)
					writeJSON(w, map[string]any{
						"error":    de.Error(),
						"reason":   de.Reason,
						"word":     de.Word,
						"position": de.Position,
						"count":    de.Count,
					})
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := enterGroup(a, secret, true); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/reveal",
		"show this instance's connection phrase again - refused while no login password is set, and with one it has to be entered again, because the phrase reaches every instance in the group",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			// The body is optional; without a password there is nothing to
			// enter again.
			_ = decodeBody(r, &body)
			if !passwordFirst(a, w, "showing the twelve words again, which open every instance in the group") {
				return
			}

			// A session is not enough: it may have been left open on an
			// unattended screen, and the phrase unlocks every instance in the
			// group. The check shares the login's throttle, or whoever sits at
			// that screen could guess the password here at full speed.
			if a.Auth.Enabled() {
				if !reg.passwordGate.try(r) {
					http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
					return
				}
				if !a.Auth.Check(body.Password) {
					writeRefusal(w, http.StatusForbidden, "passwordWrong", "the password is required to show the phrase again", nil)
					return
				}
				reg.passwordGate.pass(r)
			}
			secretHex, err := a.Accounts.Get(relay.SeedAccountService)
			if err != nil || secretHex == "" {
				http.Error(w, "this instance has no connection phrase", http.StatusNotFound)
				return
			}
			secret, err := hex.DecodeString(secretHex)
			if err != nil {
				http.Error(w, "the stored secret is unreadable", http.StatusInternalServerError)
				return
			}
			phrase, err := seedphrase.Encode(secret)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The QR comes along, since the phrase is usually needed again to
			// set up a phone.
			writeJSON(w, map[string]any{"phrase": phrase, "qr": phraseQR(a, phrase)})
		})

	// Only this instance's owner removes a phone here, never a member of the
	// group, which holds the same words as the phone it would remove. The
	// phone learns it from this instance's next answer and leaves the whole
	// group.
	reg.Add(http.MethodDelete, "/api/connect/apps/{id}",
		"take a phone or browser extension out of the group on this instance - it is turned away here, drops every connection to the group when it next calls, and comes back as a new one when the phrase is entered again",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.Federation.RemoveApp(r.PathValue("id"), time.Now()); err != nil {
				if errors.Is(err, federation.ErrIsInstance) {
					writeRefusal(w, http.StatusConflict, "memberIsInstance",
						"that id belongs to an instance of the group; take it out as an instance", nil)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/connect/devices/{id}/approve",
		"let a device that entered the words act in the group, on every instance of it - its key is refused until then",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			var passed deviceDecision
			if fromRelayGroup(r) {
				if !fromApprovedInstance(r) {
					refuseDecision(w)
					return
				}
				if !decodeJSON(w, r, &passed) {
					return
				}
			}
			key, kind, err := a.Federation.Approve(id, passed.Key, passed.Kind)
			switch {
			case errors.Is(err, federation.ErrNoSuchPending):
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			case errors.Is(err, federation.ErrGone):
				http.Error(w, err.Error(), http.StatusGone)
				return
			case errors.Is(err, federation.ErrKeyMismatch):
				http.Error(w, err.Error(), http.StatusConflict)
				return
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !fromRelayGroup(r) {
				go passDecisionOn(a, id, "approve", deviceDecision{Key: key, Kind: kind})
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/connect/devices/{id}/refuse",
		"turn away a device that entered the words, on every instance of the group - its key is revoked",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if fromRelayGroup(r) && !fromApprovedInstance(r) {
				refuseDecision(w)
				return
			}
			if err := a.Federation.Refuse(id, time.Now()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !fromRelayGroup(r) {
				go passDecisionOn(a, id, "refuse", deviceDecision{})
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodDelete, "/api/connect",
		"leave the group: forget this instance's connection secret and stop dialling the relay",
		func(w http.ResponseWriter, r *http.Request) {
			// Idempotent: setting an empty secret deletes it, whether or not
			// one was stored.
			if err := enterGroup(a, nil, false); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// enterGroup stores secret as this instance's group, or leaves the group with
// nil, and redials the relay. Either way the group starts over with nobody
// seen in it. entered is set when the words came from a group that already
// exists, whose instances this one then meets as members already there.
func enterGroup(a *app.App, secret []byte, entered bool) error {
	stored := ""
	joined := time.Time{}
	if secret != nil {
		stored = hex.EncodeToString(secret)
		joined = time.Now()
	}
	if err := a.Accounts.Set(relay.SeedAccountService, stored); err != nil {
		return err
	}
	// The member key belongs to one group, so a group entered again is
	// entered under a new key, which the members pin as a new device.
	if err := a.Accounts.Set(relay.MemberAccountService, ""); err != nil {
		return err
	}
	// No relay mode is written: an unset mode already resolves to the
	// project relay, and writing one would stop RelayModeOf from inferring
	// "own" from a typed address.
	applyRelay(a)
	return a.Federation.SetJoined(joined, entered && secret != nil)
}

// phraseQR is the QR code for the app, naming the relay this instance dials
// when that is its own (relay.PairingCode).
func phraseQR(a *app.App, phrase string) *QRMatrix {
	cfg := a.Settings.Get()
	own := ""
	if cfg.RelayModeOf() == settings.RelayModeOwn {
		own = cfg.RelayURL
	}
	return renderQR(relay.PairingCode(phrase, own))
}

func connectInfo(a *app.App) ConnectInfo {
	secretHex, _ := a.Accounts.Get(relay.SeedAccountService)
	// Which relay this instance points at, answerable before anything is
	// activated; relayTarget instead reports what to dial right now.
	cfg := a.Settings.Get()
	mode := cfg.RelayModeOf()
	url := cfg.RelayURL
	selfHosted := mode == settings.RelayModeOwn
	if !selfHosted {
		url = relay.DefaultRelayURL
	}
	if mode == settings.RelayModeOff {
		url = ""
	}

	now := time.Now()
	peers := a.Federation.Members()
	members := make([]groupMember, 0, len(peers))
	for _, p := range peers {
		name := p.DisplayName
		if name == "" {
			name = p.Name
		}
		members = append(members, groupMember{ID: p.RelayID, Name: name, Direct: p.Direct(), Relay: p.ViaRelay()})
	}
	known, err := a.Federation.Apps(now)
	if err != nil {
		log.Printf("connect: could not record the phones of the group: %v", err)
	}
	apps := make([]groupApp, 0, len(known))
	appThere := false
	for _, p := range known {
		apps = append(apps, groupApp{ID: p.ID, Name: p.Name, Deployment: p.Deployment, Connected: p.Connected, LastSeen: p.LastSeen.Unix()})
		appThere = appThere || p.Connected
	}
	st, err := a.Federation.Group(len(members) > 0 || appThere, now)
	if err != nil {
		log.Printf("connect: could not record the first member of the group: %v", err)
	}
	var joinedAgo int64
	if !st.JoinedAt.IsZero() {
		joinedAgo = max(int64(now.Sub(st.JoinedAt).Seconds()), 0)
	}

	names := map[string]string{}
	for _, m := range members {
		names[m.ID] = m.Name
	}
	for _, p := range apps {
		names[p.ID] = p.Name
	}
	pending := []pendingDevice{}
	for _, p := range a.Federation.PendingMembers() {
		pending = append(pending, pendingDevice{ID: p.ID, Name: names[p.ID], Kind: p.Kind, Fingerprint: p.Fingerprint})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })

	return ConnectInfo{
		Active:          secretHex != "",
		Connected:       a.Federation.RelayConnected(),
		PasswordSet:     a.Auth.Enabled(),
		RelayURL:        url,
		SelfHosted:      selfHosted,
		RelayMode:       mode,
		ProjectRelayURL: relay.DefaultRelayURL,
		Name:            instanceDisplayName(a),
		Address:         groupAddress(a),
		Members:         members,
		Apps:            apps,
		JoinedAgo:       joinedAgo,
		MemberSeen:      !st.MemberSeenAt.IsZero(),
		Pending:         pending,
		Awaiting:        a.Federation.AwaitingApproval(now),
	}
}

// deviceDecision is what an approval carries when it is passed on: the key
// approved and the kind it signs as, so an instance the device has not called
// yet pins it as approved.
type deviceDecision struct {
	Key  []byte `json:"key,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// fromApprovedInstance reports whether a call from the group came from an
// instance signing with an approved key. Only such a call may decide who else
// is let in; an unsigned one, or one from a phone, may not.
func fromApprovedInstance(r *http.Request) bool {
	c, ok := federation.CallerOf(r.Context())
	return ok && c.Kind == relay.KindInstance
}

func refuseDecision(w http.ResponseWriter) {
	writeRefusal(w, http.StatusForbidden, "decisionNotFromInstance",
		"only an approved instance of the group passes an approval or a refusal on", nil)
}

// passDecisionOn tells the other instances of the group that device id was
// approved or refused here. One that is away misses it, and the device waits
// there until it is approved there too.
func passDecisionOn(a *app.App, id, verb string, d deviceDecision) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body, err := json.Marshal(d)
	if err != nil {
		return
	}
	path := "/api/connect/devices/" + url.PathEscape(id) + "/" + verb
	for _, m := range a.Federation.Members() {
		if m.RelayID == id {
			continue
		}
		if _, status, err := a.Federation.Proxy(ctx, m.Name, http.MethodPost, path, body); err != nil || status >= 300 {
			log.Printf("connect: could not pass the %s of %s on to %s: status %d, %v", verb, id, m.Name, status, err)
		}
	}
}
