package api

// The connection phrase: generating one, showing it again, and joining a
// group another instance already started. There is no account: the relay
// address is compiled in (relay.DefaultRelayURL) and holding the secret is the
// whole authorization.
//
// The secret is stored and the phrase handed out; the relay only ever sees
// relay.DeriveKey of the secret, so neither its operator nor its memory can
// give back what a person would type.

import (
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
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
	// Name is what the other instances of the group see this one as.
	Name string `json:"name"`
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
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	// LastSeen is Unix seconds.
	LastSeen int64 `json:"lastSeen"`
}

func registerConnect(reg *Registry, a *app.App) {
	// A stored secret without a joining time counts as joined at start, so
	// the page waits a minute before it says nobody came.
	if secret, _ := a.Accounts.Get(relay.SeedAccountService); secret != "" {
		if st, err := a.Federation.Group(false, time.Now()); err == nil && st.JoinedAt.IsZero() {
			if err := a.Federation.SetJoined(time.Now()); err != nil {
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
			if err := enterGroup(a, secret); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"phrase": phrase, "qr": renderQR(phrase), "info": connectInfo(a)})
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
			if err := enterGroup(a, secret); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/reveal",
		"show this instance's connection phrase again - requires the login password to be entered again when one is set, because the phrase reaches every instance in the group",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			// The body is optional; without a password there is nothing to
			// enter again.
			_ = decodeBody(r, &body)

			// A session is not enough: it may have been left open on an
			// unattended screen, and the phrase unlocks every instance in the
			// group.
			if a.Auth.Enabled() && !a.Auth.Check(body.Password) {
				writeRefusal(w, http.StatusForbidden, "passwordWrong", "the password is required to show the phrase again", nil)
				return
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
			writeJSON(w, map[string]any{"phrase": phrase, "qr": renderQR(phrase)})
		})

	reg.Add(http.MethodDelete, "/api/connect",
		"leave the group: forget this instance's connection secret and stop dialling the relay",
		func(w http.ResponseWriter, r *http.Request) {
			// Idempotent: setting an empty secret deletes it, whether or not
			// one was stored.
			if err := enterGroup(a, nil); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// enterGroup stores secret as this instance's group, or leaves the group with
// nil, and redials the relay. Either way the group starts over with nobody
// seen in it.
func enterGroup(a *app.App, secret []byte) error {
	stored := ""
	joined := time.Time{}
	if secret != nil {
		stored = hex.EncodeToString(secret)
		joined = time.Now()
	}
	if err := a.Accounts.Set(relay.SeedAccountService, stored); err != nil {
		return err
	}
	// No relay mode is written: an unset mode already resolves to the
	// project relay, and writing one would stop RelayModeOf from inferring
	// "own" from a typed address.
	applyRelay(a)
	return a.Federation.SetJoined(joined)
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
		apps = append(apps, groupApp{ID: p.ID, Name: p.Name, Connected: p.Connected, LastSeen: p.LastSeen.Unix()})
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

	return ConnectInfo{
		Active:          secretHex != "",
		Connected:       a.Federation.RelayConnected(),
		PasswordSet:     a.Auth.Enabled(),
		RelayURL:        url,
		SelfHosted:      selfHosted,
		RelayMode:       mode,
		ProjectRelayURL: relay.DefaultRelayURL,
		Name:            instanceDisplayName(a),
		Members:         members,
		Apps:            apps,
		JoinedAgo:       joinedAgo,
		MemberSeen:      !st.MemberSeenAt.IsZero(),
	}
}
