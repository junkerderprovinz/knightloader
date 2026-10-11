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
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// memberKeyOf is a member key made from one byte, so a test can name a few
// distinct ones.
func memberKeyOf(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

// idOf is the id member goes by as a device of kind.
func idOf(member ed25519.PrivateKey, kind string) string {
	return relay.MemberID(kind, member.Public().(ed25519.PublicKey))
}

var callIDs atomic.Int64

// nextCallID is a request id no other call in the test run has.
func nextCallID() string { return "call-" + strconv.FormatInt(callIDs.Add(1), 10) }

// withID is call under a request id of its own.
func withID(call relay.ProxyCall) relay.ProxyCall {
	call.ID = nextCallID()
	return call
}

// signedCall is a call as relay.OpenCall hands it over: signed by member in
// the name of from, with an id of its own and the time it was sent.
func signedCall(member ed25519.PrivateKey, from, kind string) relay.ProxyCall {
	return relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks", From: from, Kind: kind,
		Key: member.Public().(ed25519.PublicKey), Sig: []byte("checked by OpenCall"),
		ID: nextCallID(), Sent: time.Now().Unix(),
	}
}

// callAs is member's call as the device of kind it is, under the id its key
// makes.
func callAs(member ed25519.PrivateKey, kind string) relay.ProxyCall {
	return signedCall(member, idOf(member, kind), kind)
}

// guarded is a manager in a group with a guard whose handler names the
// caller it was given.
func guarded(t *testing.T) (*Manager, relay.ProxyHandler) {
	t.Helper()
	m := newManager(t)
	if err := m.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	return m, m.Guard(func(ctx context.Context, _ relay.ProxyCall) (int, []byte) {
		c, _ := CallerOf(ctx)
		return http.StatusOK, []byte(c.ID)
	})
}

// letIn makes member's first signed call as a device of kind, which pins its
// key, and approves it the way a person at the instance does. It returns the
// device's id.
func letIn(t *testing.T, m *Manager, serve relay.ProxyHandler, member ed25519.PrivateKey, kind string) string {
	t.Helper()
	id := idOf(member, kind)
	if status, body := serve(context.Background(), signedCall(member, id, kind)); status != http.StatusForbidden || codeOf(body) != "memberPending" {
		t.Fatalf("the first signed call of %s = %d %s, want 403 memberPending", id, status, body)
	}
	if _, _, err := m.Approve(id, nil, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func codeOf(body []byte) string {
	var r struct{ Code string }
	_ = json.Unmarshal(body, &r)
	return r.Code
}

func TestAMembersKeyIsPinnedUnderTheIDItMakes(t *testing.T) {
	m, serve := guarded(t)
	phone, forger := memberKeyOf(1), memberKeyOf(2)

	phoneID := letIn(t, m, serve, phone, relay.KindMobile)
	if status, body := serve(context.Background(), callAs(phone, relay.KindMobile)); status != http.StatusOK || string(body) != phoneID {
		t.Fatalf("the approved call = %d %s, want it served as %s", status, body, phoneID)
	}
	status, body := serve(context.Background(), signedCall(forger, phoneID, relay.KindMobile))
	if status != http.StatusForbidden || codeOf(body) != "memberKind" {
		t.Fatalf("another key in the phone's name = %d %s, want 403 memberKind", status, body)
	}
	status, body = serve(context.Background(), unsignedCall(phoneID))
	if status != http.StatusForbidden || codeOf(body) != "memberUnsigned" {
		t.Fatalf("an unsigned call in the phone's name = %d %s, want 403 memberUnsigned", status, body)
	}
	status, body = serve(context.Background(), callAs(phone, relay.KindExtension))
	if status != http.StatusForbidden || codeOf(body) != "memberKeyChanged" {
		t.Fatalf("the phone's key as a browser = %d %s, want 403 memberKeyChanged", status, body)
	}

	st, _ := m.Group(false, time.Now())
	if pin := st.Keys[phoneID]; !bytes.Equal(pin.Key, phone.Public().(ed25519.PublicKey)) || pin.Kind != relay.KindMobile {
		t.Fatalf("pinned %+v, want the phone's own key as a phone", pin)
	}
	// The pin outlives a restart.
	again, err := Load(filepath.Dir(m.group.path))
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := again.Guard(func(context.Context, relay.ProxyCall) (int, []byte) { return http.StatusOK, nil })(
		context.Background(), callAs(phone, relay.KindMobile)); status != http.StatusOK {
		t.Fatalf("after a restart the phone = %d, want it served", status)
	}
}

// TestNobodyPinsAKeyUnderAnotherDevicesID: whoever holds the words sees every
// id on the relay and signs first under the id of a device that has not
// called this instance yet, a new instance above all, which nobody calls
// before it greets. The call is refused and nothing is pinned, so the owner
// never approves a stranger under the device's name.
func TestNobodyPinsAKeyUnderAnotherDevicesID(t *testing.T) {
	m, serve := guarded(t)
	squatter := memberKeyOf(3)
	for _, real := range []struct {
		key  ed25519.PrivateKey
		kind string
	}{{memberKeyOf(4), relay.KindInstance}, {memberKeyOf(5), relay.KindMobile}, {memberKeyOf(6), relay.KindExtension}} {
		id := idOf(real.key, real.kind)
		if status, body := serve(context.Background(), signedCall(squatter, id, real.kind)); status != http.StatusForbidden || codeOf(body) != "memberKind" {
			t.Errorf("a stranger's key under %s = %d %s, want 403 memberKind", id, status, body)
		}
		if pending := m.PendingMembers(); len(pending) != 0 {
			t.Fatalf("pinned %+v under another device's id", pending)
		}
		if _, _, err := m.Approve(id, squatter.Public().(ed25519.PublicKey), real.kind); err != ErrKindMismatch {
			t.Errorf("an approval passed on for a stranger's key under %s = %v, want ErrKindMismatch", id, err)
		}
		if status, body := serve(context.Background(), callAs(real.key, real.kind)); codeOf(body) != "memberPending" {
			t.Errorf("the real device afterwards = %d %s, want it waiting for approval", status, body)
		}
		m.group.mu.Lock()
		m.group.state.Keys = nil
		m.group.mu.Unlock()
	}
}

func TestTakingADeviceOutRevokesItsKeyForGood(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(5)
	gone := letIn(t, m, serve, phone, relay.KindMobile)
	if err := m.RemoveApp(gone, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{relay.KindMobile, relay.KindExtension} {
		if status, body := serve(context.Background(), callAs(phone, kind)); status != http.StatusGone || string(body) != "removed" {
			t.Errorf("the removed key as a %s = %d %s, want 410 removed", kind, status, body)
		}
	}
	if status, body := serve(context.Background(), unsignedCall(gone)); codeOf(body) != "memberUnsigned" {
		t.Errorf("an unsigned call under the removed id = %d %s, want 403 memberUnsigned", status, body)
	}
}

func TestAnInstanceIsNotTakenOutAsAPhoneAndComesBackOnlyForApproval(t *testing.T) {
	m, serve := guarded(t)
	old := memberKeyOf(7)
	bravo := letIn(t, m, serve, old, relay.KindInstance)
	if err := m.RemoveApp(bravo, time.Now()); err != ErrIsInstance {
		t.Fatalf("removing an instance as a phone = %v, want ErrIsInstance", err)
	}
	if err := m.RemoveMember(bravo, time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, _ := serve(context.Background(), callAs(old, relay.KindInstance)); status != http.StatusGone {
		t.Errorf("the instance's old key = %d, want 410", status)
	}
	if status, body := serve(context.Background(), callAs(memberKeyOf(8), relay.KindInstance)); status != http.StatusForbidden || codeOf(body) != "memberPending" {
		t.Errorf("the instance entering the phrase again = %d %s, want it waiting for approval under its new id", status, body)
	}
}

// TestOneKeysCallsDoNotUseUpAnothersShare holds a member at its limit of
// calls at once and checks that another member is still served.
func TestOneKeysCallsDoNotUseUpAnothersShare(t *testing.T) {
	m := newManager(t)
	if err := m.SetJoined(time.Now()); err != nil {
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
	for _, key := range []ed25519.PrivateKey{forger, real} {
		if _, _, err := m.Approve(idOf(key, relay.KindMobile), key.Public().(ed25519.PublicKey), relay.KindMobile); err != nil {
			t.Fatal(err)
		}
	}
	busy := callAs(forger, relay.KindMobile)
	busy.Path = "/api/hold"
	var done sync.WaitGroup
	for range perMemberCalls {
		started.Add(1)
		done.Add(1)
		go func(c relay.ProxyCall) {
			defer done.Done()
			serve(context.Background(), c)
		}(withID(busy))
	}
	started.Wait()
	defer func() { close(hold); done.Wait() }()

	if status, body := serve(context.Background(), callAs(forger, relay.KindMobile)); status != http.StatusTooManyRequests || codeOf(body) != "memberBusy" {
		t.Errorf("the flooding key's next call = %d %s, want 429 memberBusy", status, body)
	}
	if status, _ := serve(context.Background(), callAs(real, relay.KindMobile)); status != http.StatusOK {
		t.Errorf("another member = %d, want it served while one key is at its limit", status)
	}
}

// TestOneSenderCannotUseUpTheHourForTheOthers mints keys through one relay
// connection and checks that a device on another one still gets pinned.
func TestOneSenderCannotUseUpTheHourForTheOthers(t *testing.T) {
	_, serve := guarded(t)
	minting := relay.WithSource(context.Background(), "relay\x00phone-minting")
	refused := 0
	for i := range maxNewPinsPerSource + 2 {
		_, body := serve(minting, callAs(memberKeyOf(byte(150+i)), relay.KindMobile))
		if codeOf(body) == "groupJoinsBusy" {
			refused++
		}
	}
	if refused != 2 {
		t.Errorf("%d keys from one connection were refused, want all past its %d", refused, maxNewPinsPerSource)
	}
	other := relay.WithSource(context.Background(), "relay\x00phone-honest")
	if status, body := serve(other, callAs(memberKeyOf(170), relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("a device on another connection = %d %s, want it pinned and waiting", status, body)
	}
}

// TestAFullGroupMakesRoomOnlyWithAnIdleApprovedKey: keys made to fill the
// group wait for the owner to refuse them. None of them pushes out another
// device, waiting or approved, and only an approved key nobody used for
// pinIdle makes room for a new one.
func TestAFullGroupMakesRoomOnlyWithAnIdleApprovedKey(t *testing.T) {
	m, serve := guarded(t)
	real := memberKeyOf(11)
	realID := letIn(t, m, serve, real, relay.KindMobile)
	waiting := memberKeyOf(12)
	serve(context.Background(), callAs(waiting, relay.KindMobile))
	m.group.mu.Lock()
	for i := len(m.group.state.Keys); i < maxPinned; i++ {
		key := memberKeyOf(byte(100 + i)).Public().(ed25519.PublicKey)
		m.group.state.Keys[relay.MemberID(relay.KindMobile, key)] = PinnedKey{Key: key, Kind: relay.KindMobile, Used: time.Now()}
	}
	m.group.mu.Unlock()

	late := memberKeyOf(250)
	if status, body := serve(context.Background(), callAs(late, relay.KindMobile)); status != http.StatusTooManyRequests || codeOf(body) != "groupFull" {
		t.Fatalf("a new key in a group full of waiting ones = %d %s, want 429 groupFull", status, body)
	}
	if status, body := serve(context.Background(), callAs(waiting, relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("the device that waited before the group filled = %d %s, want it still waiting", status, body)
	}

	m.group.mu.Lock()
	pin := m.group.state.Keys[realID]
	pin.Used = time.Now().Add(-pinIdle - time.Hour)
	m.group.state.Keys[realID] = pin
	m.group.mu.Unlock()
	if status, body := serve(context.Background(), callAs(late, relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("a new key once an approved one had been idle for long = %d %s, want it pinned and waiting", status, body)
	}
	if status, body := serve(context.Background(), callAs(waiting, relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("the waiting device after the new key came in = %d %s, want it still waiting", status, body)
	}
}

// TestAFullHourPushesNoWaitingDeviceOut: whoever holds the words opens one
// relay connection after another and pins as many keys through each as it
// may, and keeps on after the owner refused the first lot. A real phone that
// came before them stays listed for the owner, and new keys wait for the next
// hour instead of taking its place.
func TestAFullHourPushesNoWaitingDeviceOut(t *testing.T) {
	m, serve := guarded(t)
	real := memberKeyOf(250)
	realID := idOf(real, relay.KindMobile)
	ctx := relay.WithSource(context.Background(), "relay\x00realphone")
	if status, body := serve(ctx, callAs(real, relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Fatalf("a real new phone = %d %s, want it pinned and waiting", status, body)
	}
	mint := func(n int) []byte {
		from := relay.WithSource(context.Background(), "relay\x00conn"+strconv.Itoa(n))
		_, body := serve(from, callAs(memberKeyOf(byte(n)), relay.KindMobile))
		return body
	}
	for n := range maxNewPins - 1 {
		if body := mint(n); codeOf(body) != "memberPending" {
			t.Fatalf("minted key %d = %s, want it pinned while the hour has room", n, body)
		}
	}
	for _, p := range m.PendingMembers() {
		if p.ID != realID {
			if err := m.Refuse(p.ID, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if body := mint(maxNewPins); codeOf(body) != "groupJoinsBusy" {
		t.Errorf("a key minted once the hour was used up = %s, want 429 groupJoinsBusy", body)
	}
	listed := false
	for _, p := range m.PendingMembers() {
		listed = listed || p.ID == realID
	}
	if !listed {
		t.Fatal("the real phone was pushed off the list of devices waiting for approval")
	}
	if _, _, err := m.Approve(realID, nil, ""); err != nil {
		t.Fatal(err)
	}
	if status, _ := serve(ctx, callAs(real, relay.KindMobile)); status != http.StatusOK {
		t.Errorf("the real phone once approved = %d, want it served", status)
	}
}

// TestACallPlayedAgainIsDropped: a member's call runs once, and a second copy
// of it gets no answer at all.
func TestACallPlayedAgainIsDropped(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(12)
	letIn(t, m, serve, phone, relay.KindMobile)
	call := callAs(phone, relay.KindMobile)
	if status, _ := serve(context.Background(), call); status != http.StatusOK {
		t.Fatalf("the call = %d, want it served", status)
	}
	if status, body := serve(context.Background(), call); status != relay.StatusDrop {
		t.Errorf("the same call again = %d %s, want it dropped", status, body)
	}
	call.ID, call.Sent = nextCallID(), time.Now().Add(-10*time.Minute).Unix()
	if status, body := serve(context.Background(), call); status != relay.StatusDrop {
		t.Errorf("an old call = %d %s, want it dropped", status, body)
	}
}
