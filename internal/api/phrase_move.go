package api

// A new phrase for the group, outside a fortress. An account that could show
// the phrase takes it along when it is blocked, removed or loses its role, so
// the group moves to a new one by itself. The instances get it sealed for
// each of them alone, and the phones and browsers fetch the keys under it on
// the old phrase, the way they follow a move into a fortress. The group on
// the old phrase stays open beside the new one until every member followed,
// 30 days at most, and takes nobody new.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// kitMovedPath is where a phone or browser on the old phrase asks for the
// keys the group runs on now, as it asks kit/fortress's Moved.
const kitMovedPath = "/kit/fortress/v1/moved"

// rotatePhrase gives the group, or the fortress this instance is in, a new
// phrase, so the one an account could show opens nothing new.
func rotatePhrase(ctx context.Context, a *app.App) error {
	if fortressSecret(a) != nil {
		_, _, err := changeFortressPhrase(ctx, a)
		return err
	}
	if groupSecret(a) == nil {
		return nil
	}
	secret, _, err := phrase.New()
	if err != nil {
		return err
	}
	return regroup(a, func() error { return keepNewPhrase(a, secret, time.Now().Round(0)) })
}

// keepNewPhrase keeps secret as the group's phrase from changed on, with the
// one it replaces and what the group kept on that one beside it.
func keepNewPhrase(a *app.App, secret []byte, changed time.Time) error {
	current := groupSecret(a)
	if current == nil || bytes.Equal(current, secret) {
		return nil
	}
	kept, err := a.Accounts.Get(federation.GroupAccount)
	if err != nil {
		return err
	}
	for account, v := range map[string]string{
		federation.GroupPreviousAccount: kept,
		federation.SeedPreviousAccount:  hex.EncodeToString(current),
		federation.SeedChangedAccount:   strconv.FormatInt(changed.UnixNano(), 10),
	} {
		if err := a.Accounts.Set(account, v); err != nil {
			return err
		}
	}
	return a.Accounts.Set(federation.SeedAccount, hex.EncodeToString(secret))
}

// takeNewPhrase keeps the new phrase another instance handed over and opens
// the group on it, once the call that brought it has been answered.
func takeNewPhrase(a *app.App) func(group.NewPhrase) error {
	return func(p group.NewPhrase) error {
		if err := keepNewPhrase(a, p.Secret, p.Changed); err != nil {
			return err
		}
		go applyRelay(a)
		return nil
	}
}

// previousPhrase is the secret the group moved away from and when, nil once
// every member followed or previousKept has passed.
func previousPhrase(a *app.App) ([]byte, time.Time) {
	previous := storedSecret(a.Accounts, federation.SeedPreviousAccount)
	if previous == nil {
		return nil, time.Time{}
	}
	raw, _ := a.Accounts.Get(federation.SeedChangedAccount)
	nanos, err := strconv.ParseInt(raw, 10, 64)
	if changed := time.Unix(0, nanos); err == nil && time.Since(changed) < previousKept {
		return previous, changed
	}
	forgetPreviousPhrase(a)
	return nil, time.Time{}
}

func forgetPreviousPhrase(a *app.App) {
	for _, account := range []string{federation.SeedPreviousAccount, federation.SeedChangedAccount, federation.GroupPreviousAccount} {
		if err := a.Accounts.Set(account, ""); err != nil {
			log.Printf("connect: forget the phrase before the change: %v", err)
		}
	}
}

// movedGroup is the group on its new phrase, with the group on the one
// before open beside it for the members that have not followed yet.
type movedGroup struct {
	*group.Group
	a       *app.App
	changed time.Time
	direct  http.Handler
	stop    chan struct{}
	wg      sync.WaitGroup

	mu     sync.Mutex
	former *formerGroup
}

// movedOf is the group of this instance while it moves to a new phrase, nil
// otherwise.
func movedOf(a *app.App) *movedGroup {
	g, _ := a.Federation.View().(*movedGroup)
	return g
}

func (g *movedGroup) Pairing() *group.Group { return g.Group }

func (g *movedGroup) Close() error {
	close(g.stop)
	g.wg.Wait()
	var errs []error
	if f := g.takeFormer(); f != nil {
		errs = append(errs, f.Close())
	}
	return errors.Join(append(errs, g.Group.Close())...)
}

func (g *movedGroup) formerGroup() *formerGroup {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.former
}

func (g *movedGroup) takeFormer() *formerGroup {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.former
	g.former = nil
	return f
}

// beside opens the group on the phrase before next to g while there is one.
func beside(a *app.App, g *group.Group) federation.GroupView {
	previous, changed := previousPhrase(a)
	if previous == nil {
		return g
	}
	former, err := openPreviousGroup(a, previous, changed)
	if err != nil {
		log.Printf("connect: open the group on the phrase before the change: %v", err)
		return g
	}
	m := &movedGroup{Group: g, a: a, changed: changed, former: former, stop: make(chan struct{})}
	m.direct = group.Direct(g, former.Group)
	m.wg.Add(1)
	go m.follow()
	return m
}

// openPreviousGroup opens the group on previous, which takes nobody new
// since changed and answers its members only with the way to the new phrase.
func openPreviousGroup(a *app.App, previous []byte, changed time.Time) (*formerGroup, error) {
	identity := memberKey(a)
	if identity == nil {
		return nil, errors.New("no member key")
	}
	var l *lan.Service
	if fortressLAN && a.Discovery() != nil {
		l = lan.New(federation.LAN)
		l.Start()
	}
	mode, url := relayOf(a)
	g, err := group.Open(group.Config{
		Domain:              federation.Domain,
		Signing:             federation.Signing,
		Secret:              previous,
		Identity:            *identity,
		Self:                groupSelf(a),
		Relay:               mode,
		RelayURL:            url,
		LAN:                 l,
		Store:               accountStore{a.Accounts, federation.GroupPreviousAccount},
		Kinds:               []member.Kind{member.KindMobile, member.KindExtension},
		Ports:               federation.LAN.Ports,
		MaxAnswerBytes:      maxAnswer,
		MaxRelayAnswerBytes: maxRelayAnswer,
	}, movedAnswer(a))
	if err != nil {
		if l != nil {
			_ = l.Close()
		}
		return nil, err
	}
	g.TakeNoNew(changed)
	return &formerGroup{Group: g, lan: l}, nil
}

// movedAnswer answers a phone or browser on the phrase before with the keys
// of the new one, as long as it is still a member, and every other call with
// the news that the phrase changed.
func movedAnswer(a *app.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := group.CallerOf(r.Context())
		device := ok && (c.Kind == member.KindMobile || c.Kind == member.KindExtension)
		secret := groupSecret(a)
		if !device || r.Method != http.MethodGet || secret == nil || !a.Federation.StillMember(c.ID) {
			writeRefusal(w, http.StatusGone, "phraseChanged", "the group moved to a new phrase", nil)
			return
		}
		switch r.URL.Path {
		case kitMovedPath:
			set := federation.Domain.Keys(secret)
			writeJSON(w, map[string]any{"group": set.Group, "frame": set.Frame, "peerAuth": set.PeerAuth})
		case movedPath:
			writeJSON(w, map[string]string{
				"groupKey":    federation.Domain.GroupKey(secret),
				"frameKey":    hex.EncodeToString(federation.Domain.FrameKey(secret)),
				"peerAuthKey": hex.EncodeToString(federation.Domain.PeerAuthKey(secret)),
			})
		default:
			writeRefusal(w, http.StatusGone, "phraseChanged", "the group moved to a new phrase", nil)
		}
	})
}

func (g *movedGroup) follow() {
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

// moveOn hands the new phrase to the instances on the old one in reach, and
// closes the group on the old phrase once every member it let in followed or
// previousKept has passed. It reports whether there is nobody left to wait
// for.
func (g *movedGroup) moveOn() bool {
	former := g.formerGroup()
	if former == nil {
		return true
	}
	met := map[string]bool{}
	for _, m := range g.Members() {
		met[m.ID] = !m.Met.IsZero()
	}
	var left []group.Member
	for _, m := range former.Members() {
		if m.State == group.StateApproved && !met[m.ID] {
			left = append(left, m)
		}
	}
	if len(left) == 0 || time.Since(g.changed) >= previousKept {
		if len(left) > 0 {
			log.Printf("connect: the group on the phrase before the change closed with %d members that never followed", len(left))
		}
		if f := g.takeFormer(); f != nil {
			if err := f.Close(); err != nil {
				log.Printf("connect: close the group on the phrase before the change: %v", err)
			}
		}
		forgetPreviousPhrase(g.a)
		return true
	}
	secret := groupSecret(g.a)
	if secret == nil {
		return false
	}
	p := former.NewPhrase(secret, g.changed)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, m := range left {
		if m.Kind != member.KindInstance || !m.Online() {
			continue
		}
		if err := former.HandNewPhrase(ctx, m.ID, p); err != nil {
			log.Printf("connect: hand the new phrase to %s: %v", m.ID, err)
		}
	}
	return false
}
