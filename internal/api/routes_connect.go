package api

// The connection phrase: generating one, showing it again, and joining a
// group another instance already started. There is no account: the project
// relay's address is compiled in (relay.DefaultURL), an instance on its
// own relay names that relay in the QR code beside the phrase, and holding the
// secret is the whole authorization.
//
// The secret is stored and the phrase handed out; the relay only ever sees
// federation.Domain.GroupKey of the secret, so neither its operator nor its
// memory can give back what a person would type.

import (
	"bytes"
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
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// ConnectInfo is what GET /api/connect answers with. It never carries the
// phrase, which only the reveal route hands out.
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
	// NetworkAddress is where this instance found itself on its network
	// (networkAddress), which Address leaves out once a known domain stands
	// in for it. "" where nothing outside the machine can reach it.
	NetworkAddress string `json:"networkAddress"`
	// Members is the other instances of the group reachable now, and Apps
	// every phone that joined it with the phrase.
	Members []groupMember `json:"members"`
	Apps    []groupApp    `json:"apps"`
	// Away is every other instance approved in the group that is not
	// reachable now, as it last announced itself.
	Away []awayMember `json:"away"`
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
	// Fingerprint is this instance's own key as the others list it while it
	// waits for approval there, "" outside a group.
	Fingerprint string `json:"fingerprint"`
	// RelayKeyDropped is whether this instance was paired by a relay key
	// typed in by hand, which the update took away, and has not paired with
	// the phrase since.
	RelayKeyDropped bool `json:"relayKeyDropped"`
	// DroppedInstances names the instances that were added here by their
	// address, which the update took away, until the owner puts the note away.
	DroppedInstances []string `json:"droppedInstances"`
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
	// Deployment is what the member announced itself as, as on its card.
	Deployment string `json:"deployment"`
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
	// Approved is whether its key was let into the group. An extension
	// connects only while it makes a call, so this is its standing between
	// calls.
	Approved bool `json:"approved"`
	// LastSeen is Unix seconds.
	LastSeen int64 `json:"lastSeen"`
}

type awayMember struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Deployment string `json:"deployment"`
	Address    string `json:"address"`
	// LastSeen is Unix seconds.
	LastSeen int64 `json:"lastSeen"`
}

// groupView is the group as the Pairing card, the Instances page, the app and
// the extension list it, from one look at the members.
type groupView struct {
	members []groupMember
	apps    []groupApp
	away    []awayMember
	pending []pendingDevice
	// awaiting is whether another instance said lately that this one waits
	// for approval there.
	awaiting bool
}

// viewOf lists the members of this instance's group.
func viewOf(a *app.App) groupView {
	v := groupView{members: []groupMember{}, apps: []groupApp{}, away: []awayMember{}, pending: []pendingDevice{}}
	for _, p := range a.Federation.List() {
		name := p.DisplayName
		if name == "" {
			name = p.Name
		}
		v.members = append(v.members, groupMember{ID: p.RelayID, Name: name, Deployment: p.Deployment, Direct: p.Direct(), Relay: p.ViaRelay()})
	}
	for _, m := range a.Federation.GroupMembers() {
		v.awaiting = v.awaiting || m.Awaiting
		if m.State == group.StatePending {
			v.pending = append(v.pending, pendingDevice{ID: m.ID, Name: m.Name, Kind: string(m.Kind), Fingerprint: member.Fingerprint(m.Key)})
		}
		switch {
		case m.Kind == member.KindMobile || m.Kind == member.KindExtension:
			v.apps = append(v.apps, groupApp{ID: m.ID, Name: m.Name, Deployment: string(m.Kind), Connected: m.Online(),
				Approved: m.State == group.StateApproved, LastSeen: m.LastSeen.Unix()})
		case m.Kind == member.KindInstance && m.State == group.StateApproved && !m.Online():
			v.away = append(v.away, awayMember{ID: m.ID, Name: m.Name, Deployment: m.Deployment, Address: m.Address,
				LastSeen: m.LastSeen.Unix()})
		}
	}
	// Connected phones first, then by name, as the cards stand on the page.
	sort.SliceStable(v.apps, func(i, j int) bool { return v.apps[i].Connected && !v.apps[j].Connected })
	sort.Slice(v.pending, func(i, j int) bool { return v.pending[i].ID < v.pending[j].ID })
	return v
}

func registerConnect(reg *Registry, a *app.App) {
	// A stored secret without a joining time counts as joined at start, so
	// the page waits a minute before it says nobody came.
	if secret, _ := a.Accounts.Get(federation.SeedAccount); secret != "" {
		if st, err := a.Federation.Joined(false, time.Now()); err == nil && st.JoinedAt.IsZero() {
			if err := a.Federation.SetJoined(time.Now()); err != nil {
				log.Printf("connect: could not record when this instance joined its group: %v", err)
			}
		}
	}

	reg.Add(http.MethodGet, "/api/connect",
		"whether this instance has a connection phrase, who else is in its group, whether its relay socket is up, and which relay it uses",
		func(w http.ResponseWriter, r *http.Request) {
			// The Pairing card asks this while it is open, which is when an
			// instance on an older release shares what it decided.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				a.Federation.SyncLegacy(ctx, time.Now())
			}()
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodGet, federation.LegacyRosterPath,
		"the phones and browser extensions this instance knows, with the key of every one approved here, and the ones taken out - for instances of the group on an older release, which take what they lack",
		func(w http.ResponseWriter, r *http.Request) {
			g := a.Federation.Group()
			if g == nil || !fromApprovedInstance(a, r) {
				writeRefusal(w, http.StatusForbidden, "rosterNotForInstance",
					"only an approved instance of the group reads its roster", nil)
				return
			}
			writeJSON(w, federation.LegacyRosterOf(g.Roster()))
		})

	reg.Add(http.MethodGet, "/api/group/apps",
		"every phone and browser extension of the group: whether it is connected now, whether its key was approved, and when it was last seen; the other instances of the group this one reaches now; and every other instance of the group that is away, as it last announced itself",
		func(w http.ResponseWriter, r *http.Request) {
			v := viewOf(a)
			writeJSON(w, struct {
				Apps    []groupApp    `json:"apps"`
				Members []groupMember `json:"members"`
				Away    []awayMember  `json:"away"`
			}{v.apps, v.members, v.away})
		})

	reg.Add(http.MethodPost, "/api/connect/activate",
		"generate a new connection phrase for this instance and start dialling the relay - answers with the phrase, the only time it is returned without the password",
		func(w http.ResponseWriter, r *http.Request) {
			secret, words, err := phrase.New()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			exists := false
			err = a.Auth.Mint(stillAdmitted(a, r), func() error {
				// Replacing an existing secret would orphan every instance
				// joined to the old phrase; leaving is an explicit DELETE.
				if existing, err := a.Accounts.Get(federation.SeedAccount); err == nil && existing != "" {
					exists = true
					return nil
				}
				return enterGroup(a, secret)
			})
			if exists {
				writeRefusal(w, http.StatusConflict, "phraseExists",
					"this instance already has a connection phrase; remove it first to start a new group", nil)
				return
			}
			if refuseGroupChange(w, err) {
				return
			}
			writeJSON(w, map[string]any{"phrase": words, "qr": phraseQR(a, words), "info": connectInfo(a)})
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
			secret, err := phrase.Decode(body.Phrase)
			if err != nil {
				// The reason and its details rather than a sentence, so the
				// browser can explain the mistyped word in the user's language.
				var de *phrase.DecodeError
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
			if refuseGroupChange(w, a.Auth.Mint(stillAdmitted(a, r), func() error { return enterGroup(a, secret) })) {
				return
			}
			writeJSON(w, connectInfo(a))
		})

	reg.Add(http.MethodPost, "/api/connect/reveal",
		"show this instance's connection phrase again; with a login password set it has to be entered again, because the phrase reaches every instance in the group",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			// The body is optional; without a password there is nothing to
			// enter again, and the phrase card warns that anyone who opens
			// the page can read the words.
			_ = decodeBody(r, &body)

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
			secretHex, err := a.Accounts.Get(federation.SeedAccount)
			if err != nil || secretHex == "" {
				http.Error(w, "this instance has no connection phrase", http.StatusNotFound)
				return
			}
			secret, err := hex.DecodeString(secretHex)
			if err != nil {
				http.Error(w, "the stored secret is unreadable", http.StatusInternalServerError)
				return
			}
			words, err := phrase.Encode(secret)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The QR comes along, since the phrase is usually needed again to
			// set up a phone.
			writeJSON(w, map[string]any{"phrase": words, "qr": phraseQR(a, words)})
		})

	// A removal starts at the owner's session and reaches the other instances
	// the way an approval does. A phone holds the same words as the phone it
	// would remove, so the group takes a removal only from an approved
	// instance. One that arrives from an instance is taken here alone, since
	// the instance that passed it on tells the others itself.
	reg.Add(http.MethodDelete, "/api/connect/apps/{id}",
		"take a phone or browser extension out of the group on every instance of it - it drops every connection to the group and comes back as a new device, waiting for approval, when the phrase is entered again",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if fromGroup(r) {
				takePassedOn(w, r, a, id)
				return
			}
			if kind := federation.KindOfID(id); kind != member.KindMobile && kind != member.KindExtension {
				writeRefusal(w, http.StatusConflict, "memberIsInstance",
					"that id belongs to an instance of the group; take it out as an instance", nil)
				return
			}
			if !removeHere(w, a, id) {
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodDelete, "/api/connect/members/{id}",
		"take another instance out of the group on every instance of it - its key is revoked, it forgets the phrase, and it comes back as a new device, waiting for approval, when the phrase is entered there again",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if fromGroup(r) {
				takePassedOn(w, r, a, id)
				return
			}
			if id == groupID(a) {
				http.Error(w, "to leave the group, use DELETE /api/connect", http.StatusBadRequest)
				return
			}
			if federation.KindOfID(id) != member.KindInstance {
				writeRefusal(w, http.StatusConflict, "memberIsApp",
					"that id belongs to a phone or a browser of the group; take it out as one", nil)
				return
			}
			if !removeHere(w, a, id) {
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/connect/devices/{id}/approve",
		"let a device that entered the words act in the group, on every instance of it - its key is refused until then",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			g := a.Federation.Group()
			if fromGroup(r) {
				var passed deviceDecision
				if !fromApprovedInstance(a, r) {
					refuseDecision(w)
					return
				}
				if !decodeJSON(w, r, &passed) {
					return
				}
				if err := g.Import(group.Roster{Members: []group.Member{{ID: id, Kind: passed.Kind, Key: passed.Key, State: group.StateApproved}}}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if g == nil {
				http.Error(w, "this instance is in no group", http.StatusNotFound)
				return
			}
			to := olderInstances(a, id)
			switch err := g.Approve(id); {
			case errors.Is(err, group.ErrNoSuchMember):
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			case errors.Is(err, group.ErrRemoved):
				http.Error(w, err.Error(), http.StatusGone)
				return
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, m := range g.Roster().Members {
				if m.ID == id {
					body, _ := json.Marshal(deviceDecision{Key: m.Key, Kind: m.Kind})
					go passOn(a, to, http.MethodPost, "/api/connect/devices/"+url.PathEscape(id)+"/approve", body)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/connect/devices/{id}/refuse",
		"turn away a device that entered the words, on every instance of the group - its key is revoked",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if fromGroup(r) {
				takePassedOn(w, r, a, id)
				return
			}
			if !removeHere(w, a, id) {
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodDelete, "/api/connect/dropped-instances",
		"put away the note naming the instances that were added by their address, which pair by the phrase now",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.Federation.PutAwayDroppedInstances(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
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

// removeHere takes member id out of the group at the owner's word: kit/group
// tells the instances on this release, and passOn the older ones, the member
// itself included. It reports whether it answered nothing yet.
func removeHere(w http.ResponseWriter, a *app.App, id string) bool {
	g := a.Federation.Group()
	if g == nil {
		return true
	}
	to := olderInstances(a, "")
	if err := g.Remove(id); err != nil && !errors.Is(err, group.ErrNoSuchMember) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	path := "/api/connect/apps/" + url.PathEscape(id)
	switch federation.KindOfID(id) {
	case member.KindInstance:
		path = "/api/connect/members/" + url.PathEscape(id)
	case "":
		return true
	}
	go passOn(a, to, http.MethodDelete, path, nil)
	return true
}

// takePassedOn takes a removal an instance on an older release passed on: of
// this instance, which then leaves, or of another member, which is taken out
// here alone.
func takePassedOn(w http.ResponseWriter, r *http.Request, a *app.App, id string) {
	g := a.Federation.Group()
	if g == nil || !fromApprovedInstance(a, r) {
		refuseDecision(w)
		return
	}
	if id != groupID(a) {
		if err := g.Import(group.Roster{Removed: map[string]time.Time{id: time.Now()}}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Answered before the group closes under this request. The leaving runs
	// under Mint, and only while the instance that asked is still approved
	// here: a reset in between puts this instance in a group of its own,
	// which nobody else may make it leave.
	w.WriteHeader(http.StatusNoContent)
	go func() {
		err := a.Auth.Mint(func() bool { return fromApprovedInstance(a, r) }, func() error { return enterGroup(a, nil) })
		if err != nil {
			log.Printf("connect: did not leave the group after an instance of it took this one out: %v", err)
		}
	}()
}

// enterGroup stores secret as this instance's group, or leaves the group with
// nil, and opens the group again. Either way the group starts over with nobody
// seen in it and nobody approved.
func enterGroup(a *app.App, secret []byte) error {
	stored := ""
	joined := time.Time{}
	if secret != nil {
		stored = hex.EncodeToString(secret)
		joined = time.Now()
	}
	return regroup(a, func() error {
		if err := a.Accounts.Set(federation.SeedAccount, stored); err != nil {
			return err
		}
		// The member key belongs to one group, so a group entered again is
		// entered under a new key, which the members pin as a new device.
		if err := a.Accounts.Set(federation.MemberAccount, ""); err != nil {
			return err
		}
		if err := a.Accounts.Set(federation.GroupAccount, ""); err != nil {
			return err
		}
		// No relay mode is written: an unset mode already resolves to the
		// project relay, and writing one would stop RelayModeOf from
		// inferring "own" from a typed address.
		return a.Federation.SetJoined(joined)
	})
}

// refuseGroupChange answers a failed join or activation and reports whether
// err was one. Both run under Mint, so a session a reset ended cannot put the
// instance back into a group the reset left.
func refuseGroupChange(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, auth.ErrStale):
		http.Error(w, err.Error(), http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return true
}

// renewGroup puts an instance that is in a pairing group into a new group of
// its own with a new phrase, so every member and phone has to pair again, and
// tells them so for a while (see retired_group.go). It reports whether there
// was a group to leave.
func renewGroup(a *app.App) (bool, error) {
	if seed, _ := a.Accounts.Get(federation.SeedAccount); seed == "" {
		return false, nil
	}
	if err := retireGroup(a, time.Now()); err != nil {
		log.Printf("connect: the devices of the group left at the reset will not hear why: %v", err)
	}
	secret, _, err := phrase.New()
	if err != nil {
		return true, err
	}
	return true, enterGroup(a, secret)
}

// phraseQR is the QR code for the app, naming the relay this instance dials
// when that is its own (relay.PairingCode).
func phraseQR(a *app.App, words string) *QRMatrix {
	cfg := a.Settings.Get()
	own := ""
	if cfg.RelayModeOf() == settings.RelayModeOwn {
		own = cfg.RelayURL
	}
	return renderQR(relay.PairingCode(words, own))
}

func connectInfo(a *app.App) ConnectInfo {
	secretHex, _ := a.Accounts.Get(federation.SeedAccount)
	// Which relay this instance points at, answerable before anything is
	// activated; relayOf instead reports what the group dials right now.
	cfg := a.Settings.Get()
	mode := cfg.RelayModeOf()
	url := cfg.RelayURL
	selfHosted := mode == settings.RelayModeOwn
	if !selfHosted {
		url = relay.DefaultURL
	}
	if mode == settings.RelayModeOff {
		url = ""
	}

	now := time.Now()
	v := viewOf(a)
	appThere := false
	for _, p := range v.apps {
		appThere = appThere || p.Connected
	}
	st, err := a.Federation.Joined(len(v.members) > 0 || appThere, now)
	if err != nil {
		log.Printf("connect: could not record the first member of the group: %v", err)
	}
	var joinedAgo int64
	if !st.JoinedAt.IsZero() {
		joinedAgo = max(int64(now.Sub(st.JoinedAt).Seconds()), 0)
	}

	return ConnectInfo{
		Active:           secretHex != "",
		Connected:        a.Federation.RelayConnected(),
		PasswordSet:      a.Auth.Enabled(),
		RelayURL:         url,
		SelfHosted:       selfHosted,
		RelayMode:        mode,
		ProjectRelayURL:  relay.DefaultURL,
		Name:             instanceDisplayName(a),
		Address:          groupAddress(a),
		NetworkAddress:   networkAddress(),
		Members:          v.members,
		Apps:             v.apps,
		Away:             v.away,
		JoinedAgo:        joinedAgo,
		MemberSeen:       !st.MemberSeenAt.IsZero(),
		Pending:          v.pending,
		Awaiting:         v.awaiting,
		Fingerprint:      ownFingerprint(a),
		RelayKeyDropped:  st.RelayKeyDropped,
		DroppedInstances: append([]string{}, st.DroppedInstances...),
	}
}

// deviceDecision is what an approval carries when it is passed on: the key
// approved and the kind it signs as, so an instance the device has not called
// yet pins it as approved.
type deviceDecision struct {
	Key  []byte      `json:"key,omitempty"`
	Kind member.Kind `json:"kind,omitempty"`
}

// fromApprovedInstance reports whether a call from the group came from an
// instance this one still holds approved under the key that signed it. Only
// such a call may decide who else is in the group; one from a phone may not.
func fromApprovedInstance(a *app.App, r *http.Request) bool {
	c, ok := memberOf(r)
	if !ok || c.Kind != member.KindInstance {
		return false
	}
	for _, m := range a.Federation.GroupMembers() {
		if m.ID == c.ID {
			return m.State == group.StateApproved && bytes.Equal(m.Key, c.Key)
		}
	}
	return false
}

func refuseDecision(w http.ResponseWriter) {
	writeRefusal(w, http.StatusForbidden, "decisionNotFromInstance",
		"only an approved instance of the group passes an approval, a refusal or a removal on", nil)
}

// olderInstances lists the approved instances of the group reachable now but
// for skip, which hear of a decision only on the routes of their release.
// Instances on this release take the same call as a decision they already
// have.
func olderInstances(a *app.App, skip string) []string {
	var out []string
	for _, m := range a.Federation.GroupMembers() {
		if m.ID != skip && m.Kind == member.KindInstance && m.State == group.StateApproved && m.Online() {
			out = append(out, m.ID)
		}
	}
	return out
}

// passOn hands a decision about the group's members, made here by the owner,
// to the instances in to, signed with this instance's key.
func passOn(a *app.App, to []string, method, path string, body []byte) {
	g := a.Federation.Group()
	if g == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, id := range to {
		if status, _, err := g.Call(ctx, id, method, path, body); err != nil || status >= 300 {
			log.Printf("connect: could not pass %s %s on to %s: status %d, %v", method, path, id, status, err)
		}
	}
}

// ownFingerprint is the fingerprint of the key this instance signs its calls
// to the group with, "" outside a group.
func ownFingerprint(a *app.App) string {
	identity := memberKey(a)
	if identity == nil {
		return ""
	}
	return member.Fingerprint(identity.Public())
}
