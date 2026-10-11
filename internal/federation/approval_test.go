package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

func TestANewDeviceIsRefusedUntilApproved(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(20)
	for range 2 {
		if status, body := serve(context.Background(), signedCall(phone, "phone-new", relay.KindMobile)); status != http.StatusForbidden || codeOf(body) != "memberPending" {
			t.Fatalf("a new device = %d %s, want 403 memberPending", status, body)
		}
	}
	pending := m.PendingMembers()
	if len(pending) != 1 || pending[0].ID != "phone-new" || pending[0].Kind != relay.KindMobile || pending[0].Fingerprint != Fingerprint(phone.Public().(ed25519.PublicKey)) {
		t.Fatalf("pending = %+v, want the phone with its kind and fingerprint", pending)
	}
	key, kind, err := m.Approve("phone-new", nil, "")
	if err != nil || kind != relay.KindMobile || string(key) != string(phone.Public().(ed25519.PublicKey)) {
		t.Fatalf("approve = %x %q %v, want the phone's key and kind to pass on", key, kind, err)
	}
	if status, _ := serve(context.Background(), signedCall(phone, "phone-new", relay.KindMobile)); status != http.StatusOK {
		t.Fatalf("the approved device = %d, want 200", status)
	}
	if len(m.PendingMembers()) != 0 {
		t.Error("the approved device is still listed as waiting")
	}
	if _, _, err := m.Approve("phone-unknown", nil, ""); err != ErrNoSuchPending {
		t.Errorf("approving an id nobody pinned = %v, want ErrNoSuchPending", err)
	}
}

// TestAnApprovalPassedOnLetsTheDeviceInBeforeItCalls is the second instance's
// side: the approval arrives with the key, and the device's first call there
// is served under it while another key in its name is not.
func TestAnApprovalPassedOnLetsTheDeviceInBeforeItCalls(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(21)
	if _, _, err := m.Approve("phone-x", phone.Public().(ed25519.PublicKey), relay.KindMobile); err != nil {
		t.Fatal(err)
	}
	if status, _ := serve(context.Background(), signedCall(phone, "phone-x", relay.KindMobile)); status != http.StatusOK {
		t.Fatalf("the device approved elsewhere = %d, want 200", status)
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(22), "phone-x", relay.KindMobile)); codeOf(body) != "memberKeyChanged" {
		t.Errorf("another key in its name = %d %s, want memberKeyChanged", status, body)
	}
	if _, _, err := m.Approve("phone-x", memberKeyOf(22).Public().(ed25519.PublicKey), relay.KindMobile); err != ErrKeyMismatch {
		t.Errorf("an approval passed on for another key = %v, want ErrKeyMismatch", err)
	}
}

func TestARefusedDeviceStaysOutAndComesBackOnlyAsANewOneWaiting(t *testing.T) {
	m, serve := guarded(t)
	phone := memberKeyOf(23)
	serve(context.Background(), signedCall(phone, "phone-r", relay.KindMobile))
	if err := m.Refuse("phone-r", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, body := serve(context.Background(), signedCall(phone, "phone-r", relay.KindMobile)); status != http.StatusGone || string(body) != "removed" {
		t.Errorf("the refused device = %d %s, want 410 removed", status, body)
	}
	if _, _, err := m.Approve("phone-r", phone.Public().(ed25519.PublicKey), relay.KindMobile); err != ErrGone {
		t.Errorf("approving the refused device afterwards = %v, want ErrGone", err)
	}
	if status, body := serve(context.Background(), signedCall(phone, "phone-other", relay.KindMobile)); status != http.StatusGone {
		t.Errorf("the refused key under a new id = %d %s, want 410", status, body)
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(24), "phone-r2", relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("the device with a new key and id = %d %s, want it waiting for approval", status, body)
	}
}

// browserID is a browser extension's member id, shaped like an instance's.
var browserID = strings.Repeat("f", 40)

// fromBeforeKeys is a manager whose group.json was written by a release
// before member keys and knows a phone and a browser.
func fromBeforeKeys(t *testing.T) (*Manager, relay.ProxyHandler) {
	t.Helper()
	dir := t.TempDir()
	old, err := json.Marshal(map[string]any{
		"joinedAt": time.Now().Add(-48 * time.Hour),
		"apps": map[string]any{
			"phone-old": map[string]any{"name": "Pixel", "lastSeen": time.Now()},
			browserID:   map[string]any{"name": "Firefox", "deployment": "extension", "lastSeen": time.Now()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "group.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m, m.Guard(func(ctx context.Context, _ relay.ProxyCall) (int, []byte) {
		c, _ := CallerOf(ctx)
		return http.StatusOK, []byte(c.ID)
	})
}

func unsignedCall(from string) relay.ProxyCall {
	return relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: from}
}

// TestThePhonesAndBrowsersFromBeforeKeysAreLetInWithoutApproval covers the
// update: the phones and browsers a group knew are approved when they first
// sign, and nobody else is.
func TestThePhonesAndBrowsersFromBeforeKeysAreLetInWithoutApproval(t *testing.T) {
	_, serve := fromBeforeKeys(t)
	for _, c := range []relay.ProxyCall{
		signedCall(memberKeyOf(30), "phone-old", relay.KindMobile),
		signedCall(memberKeyOf(32), browserID, relay.KindExtension),
	} {
		if status, body := serve(context.Background(), c); status != http.StatusOK {
			t.Errorf("%s from before keys = %d %s, want it served", c.From, status, body)
		}
	}
	for _, c := range []relay.ProxyCall{
		signedCall(memberKeyOf(31), nasID, relay.KindInstance),
		signedCall(memberKeyOf(33), "phone-new", relay.KindMobile),
	} {
		if status, body := serve(context.Background(), c); codeOf(body) != "memberPending" {
			t.Errorf("%s, unknown before the update = %d %s, want it waiting", c.From, status, body)
		}
	}
}

// TestNothingLaterAddsToTheMembersFromBeforeKeys holds the list from before
// keys to what the update found: neither a call without a key nor an announce
// without one makes a member of a new id, so whoever holds the words cannot
// sign in as an approved instance that way.
func TestNothingLaterAddsToTheMembersFromBeforeKeys(t *testing.T) {
	m, serve := fromBeforeKeys(t)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: laptopID, Name: "laptop"},
		{InstanceID: "phone-announced", Client: true, Deployment: "mobile"},
	}})
	m.List()
	if _, err := m.Apps(time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{nasID, "phone-unsigned"} {
		if status, body := serve(context.Background(), unsignedCall(from)); status != http.StatusForbidden || codeOf(body) != "memberUnsigned" {
			t.Errorf("an unsigned call naming %s = %d %s, want 403 memberUnsigned", from, status, body)
		}
	}
	for i, c := range []struct{ id, kind string }{
		{nasID, relay.KindInstance},
		{laptopID, relay.KindInstance},
		{"phone-unsigned", relay.KindMobile},
		{"phone-announced", relay.KindMobile},
	} {
		evil := memberKeyOf(byte(70 + i))
		if status, body := serve(context.Background(), signedCall(evil, c.id, c.kind)); codeOf(body) != "memberPending" {
			t.Errorf("%s signing after it was named = %d %s, want it waiting for approval", c.id, status, body)
		}
		if c.kind == relay.KindInstance && m.ApprovedInstance(Caller{ID: c.id, Kind: c.kind, Key: evil.Public().(ed25519.PublicKey)}) {
			t.Errorf("%s counts as an approved instance", c.id)
		}
	}
	if st, _ := m.Group(false, time.Now()); len(st.Legacy) != 2 {
		t.Errorf("the list from before keys = %v, want only the phone and the browser the update found", st.Legacy)
	}
}

// TestACallWithoutAKeyIsServedOnlyForAKnownDeviceStillIn checks that a call
// without a member key gets nothing in the name of nobody, of a device the
// instance does not know from before keys, or of one taken out.
func TestACallWithoutAKeyIsServedOnlyForAKnownDeviceStillIn(t *testing.T) {
	m, serve := fromBeforeKeys(t)
	if status, body := serve(context.Background(), unsignedCall("phone-old")); status != http.StatusOK || string(body) != "phone-old" {
		t.Fatalf("the phone from before keys without a key = %d %s, want it served as itself", status, body)
	}
	for _, from := range []string{"", "phone-other", officeID} {
		if status, body := serve(context.Background(), unsignedCall(from)); status != http.StatusForbidden || codeOf(body) != "memberUnsigned" {
			t.Errorf("an unsigned call naming %q = %d %s, want 403 memberUnsigned", from, status, body)
		}
	}
	if err := m.RemoveApp("phone-old", time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, body := serve(context.Background(), unsignedCall("phone-old")); status != http.StatusGone {
		t.Errorf("the removed phone without a key = %d %s, want 410", status, body)
	}
	if err := m.Refuse(browserID, relay.KindExtension, time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, body := serve(context.Background(), unsignedCall(browserID)); status != http.StatusGone {
		t.Errorf("the refused browser without a key = %d %s, want 410", status, body)
	}
	if m.StillMember("phone-old") || m.StillMember(browserID) {
		t.Error("a device taken out still counts as a member")
	}
}

// TestACallMustNameAnIdItsKindUses refuses a key for an id that no device of
// the kind it claims would mint, so an instance's key cannot be pinned under
// a phone's id or a browser's.
func TestACallMustNameAnIdItsKindUses(t *testing.T) {
	m, serve := fromBeforeKeys(t)
	for i, c := range []struct{ id, kind string }{
		{"phone-old", relay.KindInstance},
		{browserID, relay.KindInstance},
		{nasID, relay.KindMobile},
		{"phone-Old!", relay.KindMobile},
		{"phone-x", relay.KindExtension},
		{nasID, ""},
		{nasID, "robot"},
	} {
		if status, body := serve(context.Background(), signedCall(memberKeyOf(byte(80+i)), c.id, c.kind)); status != http.StatusForbidden || codeOf(body) != "memberKind" {
			t.Errorf("%q signing as %q = %d %s, want 403 memberKind", c.id, c.kind, status, body)
		}
	}
	if pending := m.PendingMembers(); len(pending) != 0 {
		t.Errorf("pinned %+v for ids that do not fit their kind", pending)
	}
	if _, _, err := m.Approve("phone-new", memberKeyOf(90).Public().(ed25519.PublicKey), relay.KindInstance); err != ErrKindMismatch {
		t.Errorf("an approval passed on for an instance under a phone's id = %v, want ErrKindMismatch", err)
	}
}

// TestAKeyPinnedUnderTheWrongKindCanStillBeTakenOut covers a group file where
// an instance's key sits under a phone's id: the phone's card removes it.
func TestAKeyPinnedUnderTheWrongKindCanStillBeTakenOut(t *testing.T) {
	m, serve := fromBeforeKeys(t)
	m.group.mu.Lock()
	m.group.state.Keys = map[string]PinnedKey{"phone-old": {Key: memberKeyOf(91).Public().(ed25519.PublicKey), Kind: relay.KindInstance, Approved: true}}
	m.group.mu.Unlock()
	if err := m.RemoveMember("phone-old", time.Now()); err != ErrNotInstance {
		t.Errorf("removing the phone's id as an instance = %v, want ErrNotInstance", err)
	}
	if err := m.RemoveApp("phone-old", time.Now()); err != nil {
		t.Fatalf("removing the phone's id as a phone = %v, want it taken out", err)
	}
	if status, _ := serve(context.Background(), signedCall(memberKeyOf(91), "phone-old", relay.KindMobile)); status != http.StatusGone {
		t.Errorf("the key after the removal = %d, want 410", status)
	}

	m.group.mu.Lock()
	m.group.state.Keys["phone-mis"] = PinnedKey{Key: memberKeyOf(92).Public().(ed25519.PublicKey), Kind: relay.KindInstance}
	m.group.state.Apps["phone-mis"] = KnownApp{Name: "Pixel"}
	m.group.mu.Unlock()
	if err := m.Refuse("phone-mis", "", time.Now()); err != nil {
		t.Fatalf("refusing the wrongly pinned key = %v, want it refused", err)
	}
	if !m.Removed("phone-mis") {
		t.Error("the refused phone's id was not taken out")
	}
}

// TestRefusingAnInstanceWithoutAKeyDoesNotBanIt: an instance keeps its id when
// it leaves, so a refusal passed on for one this instance has no key of takes
// nothing away for good.
func TestRefusingAnInstanceWithoutAKeyDoesNotBanIt(t *testing.T) {
	m, serve := guarded(t)
	for _, kind := range []string{"", relay.KindInstance} {
		if err := m.Refuse(nasID, kind, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if m.Removed(nasID) {
		t.Error("refusing an instance without a key banned its id")
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(93), nasID, relay.KindInstance)); codeOf(body) != "memberPending" {
		t.Errorf("the instance signing in afterwards = %d %s, want it waiting for approval", status, body)
	}
	if err := m.Refuse("phone-q", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !m.Removed("phone-q") {
		t.Error("refusing a phone without a key left its id free")
	}
}

// TestAnInstanceEnteringTheWordsTakesTheInstancesAlreadyThere checks the
// joining side: the instances it meets at first count as approved if the
// group lists them, phones do not, and later instances wait.
func TestAnInstanceEnteringTheWordsTakesTheInstancesAlreadyThere(t *testing.T) {
	m := newManager(t)
	joined := time.Now()
	if err := m.SetJoined(joined, true); err != nil {
		t.Fatal(err)
	}
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{{InstanceID: officeID, Name: "office"}}})
	if _, r := m.admit(signedCall(memberKeyOf(40), officeID, relay.KindInstance), "", joined.Add(time.Minute)); r != nil {
		t.Errorf("an instance of the group met right after entering the words = %s, want it served", r.body)
	}
	if _, r := m.admit(signedCall(memberKeyOf(43), nasID, relay.KindInstance), "", joined.Add(time.Minute)); r == nil {
		t.Error("an instance the group does not list was let in without approval right after entering the words")
	}
	if _, r := m.admit(signedCall(memberKeyOf(41), "phone-p", relay.KindMobile), "", joined.Add(time.Minute)); r == nil {
		t.Error("a phone met right after entering the words was let in without approval")
	}
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{{InstanceID: bravoID, Name: "bravo"}}})
	if _, r := m.admit(signedCall(memberKeyOf(42), bravoID, relay.KindInstance), "", joined.Add(trustWindow+time.Minute)); r == nil {
		t.Error("an instance met long after entering the words was let in without approval")
	}
}
