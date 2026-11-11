package api

// The fortress: every app of a household under one phrase. KnightLoader joins
// it with the twelve words, and its own pairing moves along: the instances,
// phones and browsers it was paired with stay paired, now under the fortress
// phrase. The group on the old phrase stays open until every member of it
// followed or 30 days have passed, handing the fortress phrase to the
// instances in reach and the keys of the pairing to the phones and browsers
// that ask. Whoever had not followed by then is listed as left behind.

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/fortress"
)

const appID = "knightloader"

// fortressLAN finds the members on this network as well as through the
// relay. The tests leave it off, so that none of them joins the multicast
// group.
var fortressLAN = true

// followEvery is how often the members still on the old phrase are offered
// the fortress, and the kit asked whether the old group closes.
var followEvery = time.Minute

// movedPath is where a phone or a browser on the old phrase asks for the
// keys of the pairing in the fortress. Newer ones ask kit/fortress's Moved,
// which the group on the old phrase serves beside it (openPhraseGroup).
const movedPath = "/api/group/peer/moved"

var (
	errInFortress = errors.New("KnightLoader is in a fortress already")
	errNoFortress = errors.New("KnightLoader is in no fortress")
)

// fortressSecret is the secret of the fortress this instance is in, nil
// outside one.
func fortressSecret(a *app.App) []byte {
	return storedSecret(a.Accounts, federation.FortressAccount)
}

func storedSecret(acc *accounts.Store, account string) []byte {
	v, err := acc.Get(account)
	if err != nil || v == "" {
		return nil
	}
	secret, err := hex.DecodeString(v)
	if err != nil || len(secret) != phrase.SecretLen {
		return nil
	}
	return secret
}

// fortressGroup is the pairing a fortress opened, as the federation reaches
// the members in it. It closes the fortress, and the group on the old
// phrase while there is one, with it.
type fortressGroup struct {
	a      *app.App
	f      *kitfortress.Fortress
	lan    *lan.Service
	secret []byte
	// direct answers the direct calls of the fortress, the pairing and the
	// group on the old phrase, which all arrive at group.DirectPath.
	direct http.Handler

	mu     sync.Mutex
	former *formerGroup
	stop   chan struct{}
	wg     sync.WaitGroup
}

// formerGroup is the group on the old phrase beside the fortress. It
// announces itself on a network service of its own, since closing a group
// takes back what it announced and the pairing announces on a.Discovery().
type formerGroup struct {
	*group.Group
	lan *lan.Service
}

func (f *formerGroup) Close() error {
	err := f.Group.Close()
	if f.lan != nil {
		err = errors.Join(err, f.lan.Close())
	}
	return err
}

func (g *fortressGroup) Pairing() *group.Group { return g.f.Pairing() }

func (g *fortressGroup) Members() []group.Member { return g.f.Pairing().Members() }

func (g *fortressGroup) Call(ctx context.Context, id, method, path string, body []byte) (int, []byte, error) {
	return g.f.Pairing().Call(ctx, id, method, path, body)
}

func (g *fortressGroup) RelayConnected() bool { return g.f.Pairing().RelayConnected() }

func (g *fortressGroup) Close() error {
	close(g.stop)
	g.wg.Wait()
	g.a.SetFortress(nil)
	var errs []error
	if f := g.formerGroup(); f != nil {
		errs = append(errs, f.Close())
	}
	errs = append(errs, g.f.Close())
	if g.lan != nil {
		errs = append(errs, g.lan.Close())
	}
	return errors.Join(errs...)
}

func (g *fortressGroup) formerGroup() *formerGroup {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.former
}

// left names the approved members of the group on the old phrase that have
// not followed it into the fortress yet.
func (g *fortressGroup) left() []string {
	former := g.formerGroup()
	if former == nil {
		return nil
	}
	var out []string
	for _, m := range g.f.Left(former.Group) {
		out = append(out, cmp.Or(m.Name, m.ID))
	}
	slices.Sort(out)
	return out
}

// moveOn hands the fortress phrase to the instances on the old phrase in
// reach, and ends the old group once the kit settles it: every member
// followed, or 30 days have passed. It reports whether there is nobody left
// to wait for.
func (g *fortressGroup) moveOn() bool {
	former := g.formerGroup()
	if former == nil {
		return true
	}
	if g.f.Settle(former.Group) {
		g.mu.Lock()
		g.former = nil
		g.mu.Unlock()
		if err := former.Close(); err != nil {
			log.Printf("fortress: close the group on the old phrase: %v", err)
		}
		endFormerGroup(g.a, len(g.f.Behind()))
		g.a.Hub.Broadcast("group", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kitfortress.Move(ctx, former.Group, g.secret)
	return false
}

func (g *fortressGroup) follow() {
	defer g.wg.Done()
	t := time.NewTicker(followEvery)
	defer t.Stop()
	for {
		if g.moveOn() {
			return
		}
		select {
		case <-g.stop:
			return
		case <-t.C:
		}
	}
}

// endFormerGroup forgets the old phrase and what its group kept, so nobody
// comes in with it any more. behind is how many members never followed.
func endFormerGroup(a *app.App, behind int) {
	for _, account := range []string{federation.SeedAccount, federation.GroupAccount} {
		if err := a.Accounts.Set(account, ""); err != nil {
			log.Printf("fortress: forget the old phrase: %v", err)
			return
		}
	}
	if behind > 0 {
		log.Printf("fortress: the old group closed with %d members that never followed, the old phrase is gone", behind)
		return
	}
	log.Printf("fortress: every member of the old group is in the fortress, the old phrase is gone")
}

// openFortress opens the fortress of secret with the pairing in it, and the
// group on the old phrase beside it while this instance still has one.
func openFortress(a *app.App, secret []byte) (federation.GroupView, error) {
	serve := a.SelfServeHandler()
	identity := memberKey(a)
	if serve == nil || identity == nil {
		return nil, nil
	}
	mode, url := relayOf(a)
	pairing := pairingConfig(a, serve, pairingStore{a.Accounts})
	pairing.Previous = groupSecret(a)
	cfg := kitfortress.Config{
		App:                 kitfortress.App{ID: appID, Name: "KnightLoader", Version: buildinfo.Version},
		Secret:              secret,
		Previous:            fortressPrevious(a),
		Identity:            *identity,
		Self:                fortressSelf(a),
		Relay:               mode,
		RelayURL:            url,
		Store:               accountStore{a.Accounts, federation.FortressGroupAccount},
		Keeper:              fortressKeeper{a},
		Pairing:             &kitfortress.Pairing{Group: pairing, Serve: memberHandler(a, serve)},
		Status:              fortress.Source{Events: a.FortressLog, Read: a.FortressSnapshot},
		Uses:                afterdownload.Uses,
		Export:              a.FortressExport,
		Import:              a.FortressImport,
		MaxAnswerBytes:      maxAnswer,
		MaxRelayAnswerBytes: maxRelayAnswer,
		OnRemoved: func() {
			log.Printf("fortress: another member took this instance out of the fortress")
			go func() {
				if err := regroup(a, func() error { return forgetFortress(a) }); err != nil {
					log.Printf("fortress: forget the fortress: %v", err)
				}
			}()
		},
	}
	var l *lan.Service
	if fortressLAN {
		l = lan.New(lan.Config{Group: kitfortress.Multicast})
		l.Start()
		cfg.LAN = l
	}
	f, err := kitfortress.Open(cfg)
	if err != nil {
		if l != nil {
			_ = l.Close()
		}
		return nil, err
	}
	g := &fortressGroup{a: a, f: f, lan: l, secret: bytes.Clone(secret), stop: make(chan struct{})}
	var former *group.Group
	if groupSecret(a) != nil {
		g.former = openFormerGroup(a)
		if g.former != nil {
			former = g.former.Group
			g.wg.Add(1)
			go g.follow()
		}
	}
	g.direct = f.DirectHandler(former)
	a.SetFortress(f)
	return g, nil
}

// openFormerGroup opens the group on the old phrase beside the fortress, nil
// where there is none or it does not open.
func openFormerGroup(a *app.App) *formerGroup {
	var l *lan.Service
	if fortressLAN && a.Discovery() != nil {
		l = lan.New(federation.LAN)
		l.Start()
	}
	g, err := openPhraseGroup(a, l)
	if g == nil {
		if err != nil {
			log.Printf("fortress: open the group on the old phrase: %v", err)
		}
		if l != nil {
			_ = l.Close()
		}
		return nil
	}
	return &formerGroup{Group: g, lan: l}
}

// fortressOf is the fortress this instance is in now, nil outside one.
func fortressOf(a *app.App) *fortressGroup {
	g, _ := a.Federation.View().(*fortressGroup)
	return g
}

// accountStore keeps what kit/group writes in one credential-store entry.
type accountStore struct {
	acc     *accounts.Store
	account string
}

func (s accountStore) Load() ([]byte, error) {
	v, err := s.acc.Get(s.account)
	return []byte(v), err
}

func (s accountStore) Save(b []byte) error { return s.acc.Set(s.account, string(b)) }

// pairingStore keeps the pairing in the fortress. Until it holds anything it
// reads the group's state on the old phrase, which the kit moves over.
type pairingStore struct{ acc *accounts.Store }

func (p pairingStore) Load() ([]byte, error) {
	if v, err := p.acc.Get(federation.FortressPairingAccount); err != nil || v != "" {
		return []byte(v), err
	}
	return accountStore{p.acc, federation.GroupAccount}.Load()
}

func (p pairingStore) Save(b []byte) error {
	return p.acc.Set(federation.FortressPairingAccount, string(b))
}

// changing marks the instances changing the phrase themselves, whose keeper
// then leaves opening the fortress again to the change.
var changing sync.Map

// previousKept is how long the phrase before a change still opens the
// fortress, for the members that hold only its keys to follow the change.
const previousKept = 30 * 24 * time.Hour

// fortressPrevious is the secret before the last phrase change, nil once
// previousKept has passed, when it is forgotten.
func fortressPrevious(a *app.App) []byte {
	previous := storedSecret(a.Accounts, federation.FortressPreviousAccount)
	if previous == nil {
		return nil
	}
	raw, _ := a.Accounts.Get(federation.FortressPreviousSinceAccount)
	since, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// Without a time the days start now, rather than the members that
		// hold only keys losing the way to the new phrase.
		if err := a.Accounts.Set(federation.FortressPreviousSinceAccount, strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
			log.Printf("fortress: keep when the phrase changed: %v", err)
		}
		return previous
	}
	if time.Since(time.Unix(since, 0)) < previousKept {
		return previous
	}
	for _, account := range []string{federation.FortressPreviousAccount, federation.FortressPreviousSinceAccount} {
		if err := a.Accounts.Set(account, ""); err != nil {
			log.Printf("fortress: forget the phrase before the change: %v", err)
		}
	}
	log.Printf("fortress: the phrase before the change opens nothing any more")
	return nil
}

// fortressKeeper takes a fortress phrase: one a member of the old group hands
// over as the group moves into a fortress, or a new one when the phrase
// changes.
type fortressKeeper struct{ a *app.App }

func (k fortressKeeper) Keep(secret []byte) error {
	current := fortressSecret(k.a)
	if bytes.Equal(current, secret) {
		return nil
	}
	if current != nil {
		if err := k.a.Accounts.Set(federation.FortressPreviousAccount, hex.EncodeToString(current)); err != nil {
			return err
		}
		if err := k.a.Accounts.Set(federation.FortressPreviousSinceAccount, strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
			return err
		}
	}
	if err := k.a.Accounts.Set(federation.FortressAccount, hex.EncodeToString(secret)); err != nil {
		return err
	}
	if _, ok := changing.Load(k.a); !ok {
		// The member that hands it over waits for this answer.
		go applyRelay(k.a)
	}
	return nil
}

// moveKeeper takes the fortress phrase an instance of the group hands over as
// the group moves into a fortress, and follows it there. It turns away the
// phrase of another fortress than the one this instance is in.
type moveKeeper struct{ a *app.App }

func (k moveKeeper) Keep(secret []byte) error {
	current := fortressSecret(k.a)
	if current != nil {
		if bytes.Equal(current, secret) {
			return nil
		}
		return errInFortress
	}
	if err := k.a.Accounts.Set(federation.FortressAccount, hex.EncodeToString(secret)); err != nil {
		return err
	}
	// The instance that hands it over waits for this answer.
	go applyRelay(k.a)
	return nil
}

// joinFortress joins the fortress of secret. The pairing moves along with
// the same member key, which the members already know.
func joinFortress(a *app.App, secret []byte) error {
	if fortressSecret(a) != nil {
		return errInFortress
	}
	return regroup(a, func() error {
		return a.Accounts.Set(federation.FortressAccount, hex.EncodeToString(secret))
	})
}

// forgetFortress drops what this instance keeps of its fortress. The pairing
// ends with it, since it lived under the fortress phrase.
func forgetFortress(a *app.App) error {
	for _, account := range []string{
		federation.FortressAccount, federation.FortressPreviousAccount, federation.FortressPreviousSinceAccount,
		federation.FortressGroupAccount, federation.FortressPairingAccount,
	} {
		if err := a.Accounts.Set(account, ""); err != nil {
			return err
		}
	}
	return nil
}

func leaveFortress(a *app.App) error {
	if fortressSecret(a) == nil {
		return errNoFortress
	}
	return regroup(a, func() error { return forgetFortress(a) })
}

// changeFortressPhrase gives the fortress a new phrase and opens it on that.
// It returns the new words and the devices out of reach, which keep the old
// ones.
func changeFortressPhrase(ctx context.Context, a *app.App) (string, []string, error) {
	g := fortressOf(a)
	if g == nil {
		return "", nil, errNoFortress
	}
	changing.Store(a, true)
	took, err := g.f.ChangePhrase(ctx)
	changing.Delete(a)
	if err != nil {
		return "", nil, err
	}
	left := []string{}
	noted := map[string]bool{}
	note := func(id, name string) {
		if !slices.Contains(took, id) && !noted[id] && !slices.Contains(left, name) {
			noted[id] = true
			left = append(left, name)
		}
	}
	for _, m := range g.f.Members() {
		if m.Kind == member.KindInstance {
			note(m.ID, m.Device)
		}
	}
	// Every instance the pairing approved counts, whether or not it has met
	// the pairing in the fortress yet: one still moving in holds the phrase
	// from before. Those left behind are listed apart.
	behind := map[string]bool{}
	for _, m := range g.f.Behind() {
		behind[m.ID] = true
	}
	for _, m := range g.f.Pairing().Members() {
		if m.Kind == member.KindInstance && m.State == group.StateApproved && !behind[m.ID] {
			note(m.ID, cmp.Or(m.Name, m.ID))
		}
	}
	slices.Sort(left)
	if err := regroup(a, nil); err != nil {
		return "", nil, err
	}
	words, err := phrase.Encode(fortressSecret(a))
	return words, left, err
}

// fortressMemberView is another instance of the fortress that said which app
// it runs.
type fortressMemberView struct {
	ID         string `json:"id"`
	App        string `json:"app"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Device     string `json:"device"`
	Deployment string `json:"deployment"`
	Kind       string `json:"kind"`
	Online     bool   `json:"online"`
	// Relay means it is reached through the relay alone.
	Relay    bool  `json:"relay"`
	LastSeen int64 `json:"lastSeen,omitempty"`
}

type fortressPermissionView struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Action string `json:"action"`
	On     bool   `json:"on"`
}

// fortressView is what GET /api/fortress answers. It never carries the
// phrase, which has its own route behind the password.
type fortressView struct {
	Joined bool `json:"joined"`
	// Phrase says whether the phrase can be shown and changed here.
	Phrase bool   `json:"phrase"`
	Device string `json:"device"`
	// DefaultDevice is the name Device falls back to when none is set.
	DefaultDevice string                   `json:"defaultDevice"`
	PasswordSet   bool                     `json:"passwordSet"`
	Members       []fortressMemberView     `json:"members"`
	Permissions   []fortressPermissionView `json:"permissions"`
	// PairedWith names the devices of this instance's pairing, which moves
	// into the fortress on joining and ends on leaving.
	PairedWith []string `json:"pairedWith"`
	// Left names the members of the pairing that have not followed it into
	// the fortress yet.
	Left []string `json:"left"`
	// Behind is every device of the pairing that had not followed it into the
	// fortress when the group on the old phrase closed.
	Behind []behindDevice `json:"behind"`
}

// behindDevice carries no address: the owner finds the device by its name.
type behindDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is "instance", "mobile" or "extension".
	Kind       string `json:"kind"`
	Deployment string `json:"deployment"`
}

// behindOf lists the devices left behind by the move into the fortress, empty
// outside one.
func behindOf(a *app.App) []behindDevice {
	out := []behindDevice{}
	if g := fortressOf(a); g != nil {
		for _, m := range g.f.Behind() {
			out = append(out, behindDevice{ID: m.ID, Name: m.Name, Kind: string(m.Kind), Deployment: m.Deployment})
		}
	}
	return out
}

func fortressViewOf(a *app.App) fortressView {
	v := fortressView{
		Joined:        fortressSecret(a) != nil,
		Phrase:        fortressSecret(a) != nil,
		Device:        fortressDevice(a),
		DefaultDevice: defaultFortressDevice(a),
		PasswordSet:   a.Auth.Enabled(),
		Members:       []fortressMemberView{},
		Permissions:   []fortressPermissionView{},
		PairedWith:    []string{},
		Left:          []string{},
		Behind:        behindOf(a),
	}
	behind := map[string]bool{}
	for _, d := range v.Behind {
		behind[d.ID] = true
	}
	if p := a.Federation.Group(); p != nil {
		for _, m := range p.Members() {
			if m.State == group.StateApproved && !behind[m.ID] {
				v.PairedWith = append(v.PairedWith, cmp.Or(m.Name, m.ID))
			}
		}
		slices.Sort(v.PairedWith)
	}
	g := fortressOf(a)
	if g == nil {
		return v
	}
	if left := g.left(); left != nil {
		v.Left = left
	}
	for _, m := range g.f.Members() {
		if m.State != group.StateApproved || m.App.ID == "" {
			continue
		}
		mv := fortressMemberView{
			ID: m.ID, App: m.App.ID, Name: m.App.Name, Version: m.App.Version,
			Device: m.Device, Deployment: m.Deployment, Kind: string(m.Kind),
			Online: m.Online(), Relay: m.Relay && !m.Direct,
		}
		if !m.LastSeen.IsZero() {
			mv.LastSeen = m.LastSeen.Unix()
		}
		v.Members = append(v.Members, mv)
	}
	for _, p := range g.f.Permissions() {
		if p.From == appID || p.To == appID {
			v.Permissions = append(v.Permissions, fortressPermissionView{From: p.From, To: p.To, Action: p.Action, On: p.On})
		}
	}
	return v
}

// unraidFlash is where the Unraid template mounts the flash, read-only and at
// the same place as in BombVault.
var unraidFlash = "/host/boot"

// maxDeviceName bounds the name in the fortress, as the kit bounds an app's.
const maxDeviceName = 64

// fortressDevice is the name the other members know this server by. The other
// apps find KnightLoader on their own server by it, so it is the server's
// name unless the owner gives another.
func fortressDevice(a *app.App) string {
	if name := a.Settings.Get().FortressDevice; name != "" {
		return name
	}
	return defaultFortressDevice(a)
}

// defaultFortressDevice is the name of the Unraid server, else the name this
// instance goes by in its group.
func defaultFortressDevice(a *app.App) string {
	if name := unraidServerName(unraidFlash); name != "" {
		return name
	}
	return instanceDisplayName(a)
}

// unraidServerName reads the server's name from config/ident.cfg on the
// flash, empty where there is none.
func unraidServerName(flash string) string {
	raw, err := os.ReadFile(filepath.Join(flash, "config", "ident.cfg"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "NAME="); ok {
			return strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"`))
		}
	}
	return ""
}

// fortressSelf is what the other members see of this instance, under the
// name of the server.
func fortressSelf(a *app.App) group.Self {
	s := groupSelf(a)
	s.Name = fortressDevice(a)
	return s
}

// refuseFortress answers a failed fortress change and reports whether err
// was one.
func refuseFortress(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errInFortress):
		writeRefusal(w, http.StatusConflict, "fortress.inFortress", err.Error(), nil)
	case errors.Is(err, errNoFortress):
		writeRefusal(w, http.StatusNotFound, "fortress.noFortress", err.Error(), nil)
	case errors.Is(err, auth.ErrStale):
		http.Error(w, err.Error(), http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return true
}

// passwordAgain asks for the login password again, where one is set, before
// the phrase shows: a session may be open on a screen nobody watches.
func passwordAgain(reg *Registry, a *app.App, w http.ResponseWriter, r *http.Request, password string) bool {
	if !a.Auth.Enabled() {
		return true
	}
	if !reg.passwordGate.try(r) {
		http.Error(w, "too many attempts, wait a moment", http.StatusTooManyRequests)
		return false
	}
	if !a.Auth.Check(password) {
		writeRefusal(w, http.StatusForbidden, "passwordWrong", "the password is required to show the phrase", nil)
		return false
	}
	reg.passwordGate.pass(r)
	return true
}

func registerFortressMembership(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/fortress",
		"whether this instance is in a fortress, the other apps there, the permissions between them and KnightLoader, and the devices its pairing moves along; never the phrase",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, fortressViewOf(a))
		})

	reg.Add(http.MethodPost, "/api/fortress/join",
		"join the fortress of a phrase; the pairing of this instance moves along, its instances, phones and browsers stay paired",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Phrase string `json:"phrase"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			secret, err := phrase.Decode(body.Phrase)
			if err != nil {
				var de *phrase.DecodeError
				if errors.As(err, &de) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					writeJSON(w, map[string]any{
						"error": de.Error(), "code": "fortress.phrase", "reason": de.Reason,
						"word": de.Word, "position": de.Position, "count": de.Count,
					})
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if refuseFortress(w, a.Auth.Mint(stillAdmitted(a, r), func() error { return joinFortress(a, secret) })) {
				return
			}
			writeJSON(w, fortressViewOf(a))
		})

	reg.Add(http.MethodPost, "/api/fortress/phrase/show",
		"show the fortress phrase again, with its QR code; with a login password set it has to be entered again, because the phrase opens every app of the fortress",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			_ = decodeBody(r, &body)
			if !passwordAgain(reg, a, w, r, body.Password) {
				return
			}
			secret := fortressSecret(a)
			if secret == nil {
				refuseFortress(w, errNoFortress)
				return
			}
			words, err := phrase.Encode(secret)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"phrase": words, "qr": phraseQR(a, words)})
		})

	reg.Add(http.MethodPost, "/api/fortress/phrase",
		"give the fortress a new phrase, for every instance in reach; answers with it and the devices out of reach, which enter it once; with a login password set it has to be entered again",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Password string `json:"password"`
			}
			_ = decodeBody(r, &body)
			if !passwordAgain(reg, a, w, r, body.Password) {
				return
			}
			words, left, err := changeFortressPhrase(r.Context(), a)
			if refuseFortress(w, err) {
				return
			}
			writeJSON(w, map[string]any{"phrase": words, "qr": phraseQR(a, words), "left": left})
		})

	reg.Add(http.MethodPut, "/api/fortress/device",
		"name this server for the other apps of the fortress; an empty name takes the one KnightLoader finds",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Device string `json:"device"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			name := strings.TrimSpace(body.Device)
			if utf8.RuneCountInString(name) > maxDeviceName || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				writeRefusal(w, http.StatusBadRequest, "fortress.deviceName", "the name must be one line of at most 64 characters", nil)
				return
			}
			if a.Settings.Get().FortressDevice != name {
				raw, _ := json.Marshal(name)
				if _, err := a.Settings.SetPartial(map[string]json.RawMessage{"fortressDevice": raw}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				// The fortress tells the others its name when it opens.
				if fortressSecret(a) != nil {
					applyRelay(a)
				}
			}
			writeJSON(w, fortressViewOf(a))
		})

	reg.Add(http.MethodPut, "/api/fortress/permissions",
		"switch whether one app of the fortress may run an action at another, here and at every member in reach",
		func(w http.ResponseWriter, r *http.Request) {
			var body fortressPermissionView
			if !decodeJSON(w, r, &body) {
				return
			}
			g := fortressOf(a)
			if g == nil {
				refuseFortress(w, errNoFortress)
				return
			}
			if err := g.f.SetPermission(body.From, body.To, body.Action, body.On); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, fortressViewOf(a))
		})

	reg.Add(http.MethodDelete, "/api/fortress",
		"leave the fortress; the pairing that lived under its phrase ends with it",
		func(w http.ResponseWriter, r *http.Request) {
			if refuseFortress(w, a.Auth.Mint(stillAdmitted(a, r), func() error { return leaveFortress(a) })) {
				return
			}
			writeJSON(w, fortressViewOf(a))
		})

	reg.Add(http.MethodGet, movedPath,
		"the keys of the pairing in the fortress, for a phone or browser of the group on the old phrase to follow it there without pairing again",
		func(w http.ResponseWriter, r *http.Request) {
			c, ok := memberOf(r)
			if !ok || (c.Kind != member.KindMobile && c.Kind != member.KindExtension) {
				writeRefusal(w, http.StatusForbidden, "movedForDevices", "only a phone or browser of the group asks where it moved", nil)
				return
			}
			secret := fortressSecret(a)
			if secret == nil {
				writeRefusal(w, http.StatusNotFound, "notMoved", "the group has not moved into a fortress", nil)
				return
			}
			writeJSON(w, map[string]string{
				"groupKey":    federation.Domain.GroupKey(secret),
				"frameKey":    hex.EncodeToString(federation.Domain.FrameKey(secret)),
				"peerAuthKey": hex.EncodeToString(federation.Domain.PeerAuthKey(secret)),
			})
		})
}
