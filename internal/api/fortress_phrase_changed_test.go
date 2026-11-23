package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
)

func TestAMemberThatNeverSaidItsAppShowsByItsDevice(t *testing.T) {
	t.Parallel()
	seen := time.Now().Add(-time.Hour)
	views := memberViews([]kitfortress.Member{{
		ID: "instance-cellar", Kind: member.KindInstance, Device: "Cellar", Deployment: "plugin",
		State: group.StateApproved, LastSeen: seen, Actions: []string{kitfortress.OpenAction},
	}})
	if len(views) != 1 {
		t.Fatalf("the fortress lists %+v, want the member on Cellar", views)
	}
	v := views[0]
	if v.ID != "instance-cellar" || v.App != "" || v.Device != "Cellar" || v.Online || v.LastSeen != seen.Unix() {
		t.Errorf("the member reads %+v, want it away on Cellar with no app", v)
	}
	if v.Open {
		t.Error("a member whose app is unknown offers its pages")
	}
}

type leftEntry struct {
	App    string `json:"app"`
	Device string `json:"device"`
}

func TestAPhraseChangeNamesEveryAppOnADeviceStillOnTheOldPhrase(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	arrow, fa := kitMember(t, relayURL, secret, "arrowloop", "Tower")
	bomb, fb := kitMember(t, relayURL, secret, "bombvault", "Tower")
	waitFor(t, "KnightLoader to know both apps on Tower", func() bool {
		g := fortressOf(a)
		return g != nil && knows(g.f, arrow) && knows(g.f, bomb)
	})
	for _, f := range []*kitfortress.Fortress{fa, fb} {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "both apps to be away", func() bool {
		g := fortressOf(a)
		return g != nil && !slices.ContainsFunc(g.f.Members(), kitfortress.Member.Online)
	})

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/phrase", map[string]string{})
	var changed struct {
		Left []leftEntry `json:"left"`
	}
	if err := json.Unmarshal(raw, &changed); code != http.StatusOK || err != nil {
		t.Fatalf("the change answered %d %s", code, raw)
	}
	want := []leftEntry{{App: "ArrowLoop", Device: "Tower"}, {App: "BombVault", Device: "Tower"}}
	if !slices.Equal(changed.Left, want) {
		t.Errorf("the change left %+v on the old phrase, want %+v", changed.Left, want)
	}
}

// The fortress lists a member as soon as it hears from it, before it learns
// the member's name and app, while the pairing already knows it by name.
func TestAPairedInstanceTheFortressHasNotNamedYetIsLeftUnderItsName(t *testing.T) {
	t.Parallel()
	const id = "instance-attic"
	left := leftOnOldPhrase(nil,
		[]kitfortress.Member{{ID: id, Kind: member.KindInstance, State: group.StateApproved, LastSeen: time.Now()}},
		[]group.Member{{ID: id, Kind: member.KindInstance, Name: "Attic", State: group.StateApproved}},
		nil)
	if want := []leftMember{{App: "KnightLoader", Device: "Attic"}}; !slices.Equal(left, want) {
		t.Errorf("the change left %+v on the old phrase, want %+v", left, want)
	}
}

// keptSecret is the keeper of an app that changes the phrase in a test.
type keptSecret struct {
	mu     sync.Mutex
	secret []byte
}

func (k *keptSecret) Keep(secret []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.secret = bytes.Clone(secret)
	return nil
}

func (k *keptSecret) kept() []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return bytes.Clone(k.secret)
}

// changerOpen opens the instance of another app that changes the phrase,
// on secret with previous before it, under the same key and store each time.
func changerOpen(t *testing.T, relayURL string, secret, previous []byte, store *memoryStore, keeper *keptSecret) (string, *kitfortress.Fortress) {
	t.Helper()
	seed := sha256.Sum256([]byte("changer"))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	f, err := kitfortress.Open(kitfortress.Config{
		App:      kitfortress.App{ID: "bombvault", Name: "BombVault", Version: "1.0.0"},
		Secret:   secret,
		Previous: previous,
		Identity: id,
		Self:     group.Self{Name: "Atlas", Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		Store:    store,
		Keeper:   keeper,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return id.ID(), f
}

type phraseChangedView struct {
	Joined        bool `json:"joined"`
	PhraseChanged bool `json:"phraseChanged"`
}

func phraseChangedOn(t *testing.T, base string) phraseChangedView {
	t.Helper()
	var v phraseChangedView
	if code := getJSON(t, base+"/api/fortress", &v); code != http.StatusOK {
		t.Fatalf("GET /api/fortress answered %d", code)
	}
	return v
}

func TestAnInstanceWhoseContactsMovedAsksForTheNewWordsAndStaysTheSameMember(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	old, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	store, keeper := &memoryStore{}, &keptSecret{}
	changer, fc := changerOpen(t, relayURL, old, nil, store, keeper)
	dir := t.TempDir()
	if err := kitfortress.Join(dir, old, kitfortress.Membership{App: "watcher", Device: "Cellar", Relay: relayURL}); err != nil {
		t.Fatal(err)
	}
	p := openPlugin(t, dir)
	waitFor(t, "everybody to know everybody", func() bool {
		g := fortressOf(a)
		return g != nil && knows(g.f, changer) && pluginMeets(p, a) &&
			slices.ContainsFunc(fc.Members(), func(m kitfortress.Member) bool { return m.App.ID == "watcher" }) &&
			slices.ContainsFunc(g.f.Members(), func(m kitfortress.Member) bool { return m.App.ID == "watcher" })
	})
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	// KnightLoader is away for the change, and the plugin follows it later.
	if code, cfg := putRelayConfig(t, srv.URL, `{"mode":"off"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, cfg)
	}
	waitFor(t, "KnightLoader to be away", func() bool {
		return !slices.ContainsFunc(fc.Members(), func(m kitfortress.Member) bool { return m.App.ID == appID && m.Online() })
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := fc.ChangePhrase(ctx); err != nil {
		t.Fatal(err)
	}
	next := keeper.kept()
	if err := fc.Close(); err != nil {
		t.Fatal(err)
	}
	_, moved := changerOpen(t, relayURL, next, old, store, keeper)
	p = openPlugin(t, dir)
	followed := false
	for !followed && ctx.Err() == nil {
		followed, _ = kitfortress.Follow(ctx, dir, p)
		time.Sleep(50 * time.Millisecond)
	}
	if !followed {
		t.Fatal("the plugin never followed the change")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := moved.Close(); err != nil {
		t.Fatal(err)
	}
	p = openPlugin(t, dir)

	if code, cfg := putRelayConfig(t, srv.URL, `{"relayUrl":"`+relayURL+`","mode":"own"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, cfg)
	}
	waitFor(t, "KnightLoader to hear that the phrase changed", func() bool { return phraseChangedOn(t, srv.URL).PhraseChanged })

	nextWords, err := phrase.Encode(next)
	if err != nil {
		t.Fatal(err)
	}
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": nextWords}); code != http.StatusOK {
		t.Fatalf("entering the new words answered %d: %s", code, raw)
	}
	if !bytes.Equal(fortressSecret(a), next) {
		t.Error("KnightLoader does not keep the new phrase")
	}
	waitFor(t, "the plugin to meet KnightLoader under the new phrase", func() bool { return pluginMeets(p, a) })
	if v := phraseChangedOn(t, srv.URL); !v.Joined || v.PhraseChanged {
		t.Errorf("after the new words the fortress reads %+v", v)
	}
}
