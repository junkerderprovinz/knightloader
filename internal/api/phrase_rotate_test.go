package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
	"github.com/junkerderprovinz/parleyport/kit/signin"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// An account that could show the phrase takes it along when it goes. The
// group moves to a new phrase by itself: the phone it let in before keeps
// working with the keys it is handed, and the old phrase lets nobody new in.
func TestAnAccountThatEndsTakesNoWorkingPhraseAlong(t *testing.T) {
	for _, what := range []signin.Ending{signin.EndBlocked, signin.EndRemoved, signin.EndRole, signin.EndRevoked} {
		t.Run(string(what), func(t *testing.T) {
			t.Parallel()
			srv, a := testServer(t)
			t.Cleanup(srv.Close)
			relayURL := ownRelay(t, a, srv.URL)
			old := phraseGroup(t, a)
			applyRelay(a)
			approve(t, a, "phone", member.KindMobile)
			phone := idAs("phone", member.KindMobile)
			instance := groupID(a)

			if err := signInEnded(a)(context.Background(), "newcomer", what); err != nil {
				t.Fatal(err)
			}
			secret := groupSecret(a)
			if secret == nil || bytes.Equal(secret, old) {
				t.Fatal("the group kept the phrase the account could show")
			}
			if groupID(a) != instance {
				t.Error("the instance goes by another id under the new phrase, so its members lose it")
			}
			waitFor(t, "the group on the old phrase on the relay", func() bool {
				g := movedOf(a)
				return g != nil && g.formerGroup() != nil && g.formerGroup().RelayConnected()
			})

			code, raw := relayCall(t, relayURL, old, phone, instance, keyOf("phone"),
				relay.ProxyCall{Method: http.MethodGet, Path: kitMovedPath, From: phone, Kind: member.KindMobile})
			var moved struct {
				Group    string `json:"group"`
				Frame    []byte `json:"frame"`
				PeerAuth []byte `json:"peerAuth"`
			}
			_ = json.Unmarshal(raw, &moved)
			want := federation.Domain.Keys(secret)
			if code != http.StatusOK || moved.Group != want.Group || !bytes.Equal(moved.Frame, want.Frame) || !bytes.Equal(moved.PeerAuth, want.PeerAuth) {
				t.Fatalf("the phone on the old phrase was answered %d %s, want the new keys", code, raw)
			}
			waitFor(t, "the instance on the relay under the new phrase", a.Federation.RelayConnected)
			code, raw = relayCall(t, relayURL, secret, phone, instance, keyOf("phone"),
				relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: phone, Kind: member.KindMobile})
			if code != http.StatusOK {
				t.Errorf("the phone on the new keys was answered %d %s", code, raw)
			}

			stranger := idAs("phone-of-the-blocked", member.KindMobile)
			code, raw = relayCall(t, relayURL, old, stranger, instance, keyOf("phone-of-the-blocked"),
				relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: stranger, Kind: member.KindMobile})
			if code != http.StatusForbidden || !bytes.Contains(raw, []byte(group.ClosedUntilNewPhrase)) {
				t.Errorf("a newcomer on the old phrase was answered %d %s, want it turned away", code, raw)
			}
			if _, waits := memberOfGroup(a, stranger); waits {
				t.Error("a newcomer on the old phrase waits for approval")
			}
		})
	}
}

// A restart opens the group on the old phrase again for the members that
// have not followed yet.
func TestTheOldPhraseWaitsForThePhoneAcrossARestart(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	relayURL := ownRelay(t, a, srv.URL)
	old := phraseGroup(t, a)
	applyRelay(a)
	approve(t, a, "phone", member.KindMobile)
	phone := idAs("phone", member.KindMobile)
	if err := signInEnded(a)(context.Background(), "newcomer", signin.EndBlocked); err != nil {
		t.Fatal(err)
	}
	applyRelay(a)
	waitFor(t, "the group on the old phrase on the relay", func() bool {
		g := movedOf(a)
		return g != nil && g.formerGroup() != nil && g.formerGroup().RelayConnected()
	})
	code, raw := relayCall(t, relayURL, old, phone, groupID(a), keyOf("phone"),
		relay.ProxyCall{Method: http.MethodGet, Path: kitMovedPath, From: phone, Kind: member.KindMobile})
	if code != http.StatusOK {
		t.Errorf("after a restart the phone on the old phrase was answered %d %s", code, raw)
	}
}

// Another instance of the group is handed the new phrase sealed for it, and
// both stay members of each other under it.
func TestThePairedInstanceFollowsTheNewPhrase(t *testing.T) {
	t.Parallel()
	_, a, _, b, _, old := joinedTwo(t)
	waitFor(t, "a to reach b", func() bool { m, _ := memberOfGroup(a, memberKey(b).ID()); return m.Online() })

	if err := signInEnded(a)(context.Background(), "newcomer", signin.EndBlocked); err != nil {
		t.Fatal(err)
	}
	secret := groupSecret(a)
	if bytes.Equal(secret, old) {
		t.Fatal("the group kept the old phrase")
	}
	waitFor(t, "b to take the new phrase", func() bool { return bytes.Equal(groupSecret(b), secret) })
	waitFor(t, "b to reach a under the new phrase", func() bool {
		g := b.Federation.View()
		if g == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code, _, err := g.Call(ctx, groupID(a), http.MethodGet, "/api/tasks", nil)
		return err == nil && code == http.StatusOK
	})
	if m, _ := memberOfGroup(a, memberKey(b).ID()); m.State != group.StateApproved {
		t.Errorf("a lists b as %q under the new phrase", m.State)
	}
}

// In a fortress the fortress phrase changes, the way Change phrase on the
// Pairing page does it.
func TestAnAccountThatEndsChangesTheFortressPhrase(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	arrowLoopIn(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}

	if err := signInEnded(a)(context.Background(), "newcomer", signin.EndBlocked); err != nil {
		t.Fatal(err)
	}
	if now := fortressSecret(a); now == nil || bytes.Equal(now, secret) {
		t.Fatal("the fortress kept the phrase the account could show")
	}
	if v := fortressNow(t, srv.URL); !v.Joined {
		t.Errorf("after the change the fortress reads %+v", v)
	}
}

// memberOfGroup is id as a's group lists it, and whether it is listed.
func memberOfGroup(a *app.App, id string) (group.Member, bool) {
	for _, m := range a.Federation.GroupMembers() {
		if m.ID == id {
			return m, true
		}
	}
	return group.Member{}, false
}
