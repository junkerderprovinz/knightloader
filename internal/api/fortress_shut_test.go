package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"

	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// kitMember opens an app of the fortress secret makes on the relay at
// relayURL, under a key of its own.
func kitMember(t *testing.T, relayURL string, secret []byte, app, device string) (string, *kitfortress.Fortress) {
	t.Helper()
	seed := sha256.Sum256(append([]byte(app+" "+device+" "), secret...))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	f, err := kitfortress.Open(kitfortress.Config{
		App:      kitfortress.App{ID: app, Name: app, Version: "1.0.0"},
		Secret:   secret,
		Identity: id,
		Self:     group.Self{Name: device, Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		Store:    &memoryStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return id.ID(), f
}

// knows reports whether f has heard from member id which app it runs.
func knows(f *kitfortress.Fortress, id string) bool {
	return slices.ContainsFunc(f.Members(), func(m kitfortress.Member) bool { return m.ID == id && m.App.ID != "" })
}

func lists(f *kitfortress.Fortress, id string) bool {
	return slices.ContainsFunc(f.Members(), func(m kitfortress.Member) bool { return m.ID == id })
}

func TestAJoinAfterARemovalIsRefusedAndLeavesThePairingWhereItWas(t *testing.T) {
	t.Parallel()
	srvA, a, _, b, relayURL, old := joinedTwo(t)
	secret, words := newWords(t)
	alpha, fa := kitMember(t, relayURL, secret, "arrowloop", "Tower")
	bravo, fb := kitMember(t, relayURL, secret, "bombvault", "Cellar")
	charlie, fc := kitMember(t, relayURL, secret, "firesquire", "Attic")
	for _, f := range []*kitfortress.Fortress{fa, fb, fc} {
		waitFor(t, "every app to see the others", func() bool { return len(f.Members()) == 2 })
	}
	waitFor(t, "alpha and bravo to know each other", func() bool { return knows(fa, bravo) && knows(fb, alpha) })
	if err := fa.Remove(charlie); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bravo to take charlie out", func() bool { return !lists(fb, charlie) })
	if err := fc.Close(); err != nil {
		t.Fatal(err)
	}

	code, raw := postJSON(t, http.MethodPost, srvA.URL+"/api/fortress/join", map[string]string{"phrase": words})
	var answer map[string]any
	_ = json.Unmarshal(raw, &answer)
	if code != http.StatusForbidden || answer["code"] != group.ClosedUntilNewPhrase || answer["ok"] != false {
		t.Fatalf("a join after a removal answered %d %s", code, raw)
	}
	if _, ok := answer["error"]; ok {
		t.Errorf("the refusal carries words of its own: %s", raw)
	}

	if v := fortressNow(t, srvA.URL); v.Joined {
		t.Errorf("after the refusal the fortress reads %+v", v)
	}
	if a.InFortress() || fortressOf(a) != nil {
		t.Error("the station still sees a fortress after the refusal")
	}
	for _, account := range []string{federation.FortressAccount, federation.FortressGroupAccount, federation.FortressPairingAccount} {
		if v, _ := a.Accounts.Get(account); v != "" {
			t.Errorf("%s keeps %q after the refusal", account, v)
		}
	}
	if !bytes.Equal(groupSecret(a), old) || a.Federation.Group() == nil {
		t.Error("the group on the old phrase is gone after the refusal")
	}
	if moveStarted(a) {
		t.Error("the group on the old phrase started its move into the fortress")
	}
	if fortressSecret(b) != nil {
		t.Error("the paired instance was handed the phrase of a fortress that turned KnightLoader away")
	}
}
