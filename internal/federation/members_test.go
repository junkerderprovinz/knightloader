package federation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// memberKeyOf is a member key made from one byte, so a test can name a few
// distinct ones.
func memberKeyOf(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

// signedCall is a call as relay.OpenCall hands it over: signed by member in
// the name of from.
func signedCall(member ed25519.PrivateKey, from, kind string) relay.ProxyCall {
	return relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks", From: from, Kind: kind,
		Key: member.Public().(ed25519.PublicKey), Sig: []byte("checked by OpenCall"),
	}
}

// guarded is a manager in a group with a guard whose handler names the
// caller it was given.
func guarded(t *testing.T) (*Manager, relay.ProxyHandler) {
	t.Helper()
	m := newManager(t)
	if err := m.SetJoined(time.Now(), false); err != nil {
		t.Fatal(err)
	}
	return m, m.Guard(func(ctx context.Context, _ relay.ProxyCall) (int, []byte) {
		c, _ := CallerOf(ctx)
		return http.StatusOK, []byte(c.ID)
	})
}

// letIn makes member's first signed call as from, which pins its key, and
// approves it the way a person at the instance does.
func letIn(t *testing.T, m *Manager, serve relay.ProxyHandler, member ed25519.PrivateKey, from, kind string) {
	t.Helper()
	if status, body := serve(context.Background(), signedCall(member, from, kind)); status != http.StatusForbidden || codeOf(body) != "memberPending" {
		t.Fatalf("the first signed call of %s = %d %s, want 403 memberPending", from, status, body)
	}
	if _, _, err := m.Approve(from, nil, ""); err != nil {
		t.Fatal(err)
	}
}

func codeOf(body []byte) string {
	var r struct{ Code string }
	_ = json.Unmarshal(body, &r)
	return r.Code
}

func TestAMembersFirstSignedCallPinsItsKeyAndNoOtherKeySpeaksForIt(t *testing.T) {
	m, serve := guarded(t)
	phone, forger := memberKeyOf(1), memberKeyOf(2)

	letIn(t, m, serve, phone, "phone-a", relay.KindMobile)
	if status, body := serve(context.Background(), signedCall(phone, "phone-a", relay.KindMobile)); status != http.StatusOK || string(body) != "phone-a" {
		t.Fatalf("the approved call = %d %s, want it served as phone-a", status, body)
	}
	if status, _ := serve(context.Background(), signedCall(phone, "phone-a", relay.KindMobile)); status != http.StatusOK {
		t.Fatalf("the pinned key's next call = %d, want 200", status)
	}
	status, body := serve(context.Background(), signedCall(forger, "phone-a", relay.KindMobile))
	if status != http.StatusForbidden || codeOf(body) != "memberKeyChanged" {
		t.Fatalf("another key in phone-a's name = %d %s, want 403 memberKeyChanged", status, body)
	}
	status, body = serve(context.Background(), relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: "phone-a"})
	if status != http.StatusForbidden || codeOf(body) != "memberUnsigned" {
		t.Fatalf("an unsigned call in phone-a's name = %d %s, want 403 memberUnsigned", status, body)
	}
	status, body = serve(context.Background(), signedCall(phone, "phone-b", relay.KindMobile))
	if status != http.StatusForbidden || codeOf(body) != "memberKeyChanged" {
		t.Fatalf("phone-a's key in another name = %d %s, want 403", status, body)
	}

	st, _ := m.Group(false, time.Now())
	if pin := st.Keys["phone-a"]; !bytes.Equal(pin.Key, phone.Public().(ed25519.PublicKey)) || pin.Kind != relay.KindMobile {
		t.Fatalf("pinned %+v, want phone-a's own key as a phone", pin)
	}
	// The pin outlives a restart.
	again, err := Load(filepath.Dir(m.group.path))
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := again.Guard(func(context.Context, relay.ProxyCall) (int, []byte) { return http.StatusOK, nil })(
		context.Background(), signedCall(forger, "phone-a", relay.KindMobile)); status != http.StatusForbidden {
		t.Fatalf("after a restart another key in phone-a's name = %d, want 403", status)
	}
}

func TestAMemberKnownBeforeKeysKeepsItsCardAndPinsOnItsFirstSignedCall(t *testing.T) {
	_, serve := fromBeforeKeys(t)
	// Before the update: the phone called unsigned and has a card.
	if status, _ := serve(context.Background(), unsignedCall("phone-old")); status != http.StatusOK {
		t.Fatalf("an unsigned call from a member not yet on keys = %d, want 200", status)
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(4), "phone-old", relay.KindMobile)); status != http.StatusOK || string(body) != "phone-old" {
		t.Fatalf("its first signed call = %d %s, want it pinned under the id it had", status, body)
	}
	if status, body := serve(context.Background(), unsignedCall("phone-old")); codeOf(body) != "memberUnsigned" {
		t.Fatalf("an unsigned call in its name once it signs = %d %s, want memberUnsigned", status, body)
	}
}

// TestAHandEnteredRelayKeyStillServesCallsWithoutAKey: outside a phrase group
// there are no member keys, and the relay key is all a caller can show.
func TestAHandEnteredRelayKeyStillServesCallsWithoutAKey(t *testing.T) {
	m := newManager(t)
	serve := m.Guard(func(context.Context, relay.ProxyCall) (int, []byte) { return http.StatusOK, nil })
	if status, body := serve(context.Background(), unsignedCall("")); status != http.StatusOK {
		t.Errorf("an unsigned call outside a phrase group = %d %s, want it served", status, body)
	}
}

func TestTakingADeviceOutRevokesItsKeyForGood(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(5)
	letIn(t, m, serve, phone, "phone-gone", relay.KindMobile)
	if err := m.RemoveApp("phone-gone", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"phone-gone", "phone-new-id"} {
		if status, body := serve(context.Background(), signedCall(phone, from, relay.KindMobile)); status != http.StatusGone || string(body) != "removed" {
			t.Errorf("the removed key as %s = %d %s, want 410 removed", from, status, body)
		}
	}
	if status, _ := serve(context.Background(), signedCall(memberKeyOf(6), "phone-gone", relay.KindMobile)); status != http.StatusGone {
		t.Errorf("a new key under the removed id = %d, want 410", status)
	}
	if status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: "phone-gone"}); status != http.StatusGone {
		t.Errorf("an unsigned call under the removed id = %d, want 410", status)
	}
}

func TestAnInstanceIsNotTakenOutAsAPhoneAndComesBackOnlyForApproval(t *testing.T) {
	m, serve := guarded(t)
	old := memberKeyOf(7)
	letIn(t, m, serve, old, bravoID, relay.KindInstance)
	if err := m.RemoveApp(bravoID, time.Now()); err != ErrIsInstance {
		t.Fatalf("removing an instance as a phone = %v, want ErrIsInstance", err)
	}
	if err := m.RemoveMember(bravoID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, _ := serve(context.Background(), signedCall(old, bravoID, relay.KindInstance)); status != http.StatusGone {
		t.Errorf("the instance's old key = %d, want 410", status)
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(8), bravoID, relay.KindInstance)); status != http.StatusForbidden || codeOf(body) != "memberPending" {
		t.Errorf("the instance entering the phrase again = %d %s, want it waiting for approval under its id", status, body)
	}
}

// TestOneKeysCallsDoNotUseUpAnothersShare holds a member at its limit of
// calls at once and checks that another member, and callers without keys,
// are still served.
func TestOneKeysCallsDoNotUseUpAnothersShare(t *testing.T) {
	m := newManager(t)
	if err := m.SetJoined(time.Now(), false); err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	var started sync.WaitGroup
	serve := m.Guard(func(_ context.Context, call relay.ProxyCall) (int, []byte) {
		if call.Path == "/api/hold" {
			started.Done()
			<-hold
		}
		return http.StatusOK, nil
	})
	forger, real := memberKeyOf(9), memberKeyOf(10)
	for id, key := range map[string]ed25519.PrivateKey{"phone-flood": forger, "phone-real": real} {
		if _, _, err := m.Approve(id, key.Public().(ed25519.PublicKey), relay.KindMobile); err != nil {
			t.Fatal(err)
		}
	}
	busy := signedCall(forger, "phone-flood", relay.KindMobile)
	busy.Path = "/api/hold"
	var done sync.WaitGroup
	for range perMemberCalls {
		started.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			serve(context.Background(), busy)
		}()
	}
	started.Wait()
	defer func() { close(hold); done.Wait() }()

	if status, body := serve(context.Background(), signedCall(forger, "phone-flood", relay.KindMobile)); status != http.StatusTooManyRequests || codeOf(body) != "memberBusy" {
		t.Errorf("the flooding key's next call = %d %s, want 429 memberBusy", status, body)
	}
	if status, _ := serve(context.Background(), signedCall(real, "phone-real", relay.KindMobile)); status != http.StatusOK {
		t.Errorf("another member = %d, want it served while one key is at its limit", status)
	}
}

// TestOneMemberWithoutAKeyCannotTakeEveryUnsignedCall holds one phone from
// before keys at its limit and checks that the other is still served.
func TestOneMemberWithoutAKeyCannotTakeEveryUnsignedCall(t *testing.T) {
	m, _ := fromBeforeKeys(t)
	hold := make(chan struct{})
	var started sync.WaitGroup
	serve := m.Guard(func(_ context.Context, call relay.ProxyCall) (int, []byte) {
		if call.Path == "/api/hold" {
			started.Done()
			<-hold
		}
		return http.StatusOK, nil
	})
	busy := unsignedCall("phone-old")
	busy.Path = "/api/hold"
	var done sync.WaitGroup
	for range unsignedPerMember {
		started.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			serve(context.Background(), busy)
		}()
	}
	started.Wait()
	defer func() { close(hold); done.Wait() }()

	if status, body := serve(context.Background(), unsignedCall("phone-old")); status != http.StatusTooManyRequests || codeOf(body) != "memberBusy" {
		t.Errorf("the busy phone's next call = %d %s, want 429 memberBusy", status, body)
	}
	if status, body := serve(context.Background(), unsignedCall(browserID)); status != http.StatusOK {
		t.Errorf("the other device from before keys = %d %s, want it served", status, body)
	}
}

// TestOneSenderCannotUseUpTheHourForTheOthers mints keys through one relay
// connection and checks that a device on another one still gets pinned.
func TestOneSenderCannotUseUpTheHourForTheOthers(t *testing.T) {
	_, serve := guarded(t)
	minting := relay.WithSource(context.Background(), "relay\x00phone-minting")
	refused := 0
	for i := range maxNewPinsPerSource + 2 {
		_, body := serve(minting, signedCall(memberKeyOf(byte(150+i)), "phone-minted-"+string(rune('a'+i)), relay.KindMobile))
		if codeOf(body) == "groupJoinsBusy" {
			refused++
		}
	}
	if refused != 2 {
		t.Errorf("%d keys from one connection were refused, want all past its %d", refused, maxNewPinsPerSource)
	}
	other := relay.WithSource(context.Background(), "relay\x00phone-honest")
	if status, body := serve(other, signedCall(memberKeyOf(170), "phone-honest", relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("a device on another connection = %d %s, want it pinned and waiting", status, body)
	}
}

func TestMintingKeysCannotFillTheGroupForTheMembersItHas(t *testing.T) {
	m, serve := guarded(t)
	real := memberKeyOf(11)
	letIn(t, m, serve, real, "phone-real", relay.KindMobile)
	refused := 0
	for i := range maxNewPins + 4 {
		status, body := serve(context.Background(), signedCall(memberKeyOf(byte(100+i)), "phone-minted-"+strconv.Itoa(i), relay.KindMobile))
		if status == http.StatusTooManyRequests && codeOf(body) == "groupJoinsBusy" {
			refused++
		}
	}
	if refused == 0 {
		t.Error("every minted key was pinned; new pins have no limit")
	}
	if status, _ := serve(context.Background(), signedCall(real, "phone-real", relay.KindMobile)); status != http.StatusOK {
		t.Errorf("the real phone after the minted keys = %d, want it served", status)
	}

	// A group full of approved members makes room only with a key nobody
	// used for pinIdle.
	m.group.mu.Lock()
	for id, pin := range m.group.state.Keys {
		if !pin.Approved {
			delete(m.group.state.Keys, id)
		}
	}
	for i := len(m.group.state.Keys); i < maxPinned; i++ {
		m.group.state.Keys[string(rune(0x2000+i))] = PinnedKey{Key: memberKeyOf(byte(i)).Public().(ed25519.PublicKey), Used: time.Now(), Approved: true}
	}
	m.group.mu.Unlock()
	m.limits = newMemberLimits()
	if status, body := serve(context.Background(), signedCall(memberKeyOf(250), "phone-late", relay.KindMobile)); status != http.StatusTooManyRequests || codeOf(body) != "groupFull" {
		t.Fatalf("a new key in a full group = %d %s, want 429 groupFull", status, body)
	}
	idle := string(rune(0x2000 + maxPinned - 1))
	m.group.mu.Lock()
	pin := m.group.state.Keys[idle]
	pin.Used = time.Now().Add(-pinIdle - time.Hour)
	m.group.state.Keys[idle] = pin
	m.group.mu.Unlock()
	if status, body := serve(context.Background(), signedCall(memberKeyOf(250), "phone-late", relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("a new key once one pin had been idle for long = %d %s, want it pinned and waiting", status, body)
	}
	if status, _ := serve(context.Background(), signedCall(real, "phone-real", relay.KindMobile)); status != http.StatusOK {
		t.Errorf("the real phone in a full group = %d, want it served", status)
	}
}
