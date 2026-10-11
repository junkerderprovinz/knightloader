package federation

import (
	"context"
	"crypto/ed25519"
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
	if err := m.Refuse("phone-r", time.Now()); err != nil {
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

// browserID is a browser extension's member id.
var browserID = "browser-" + strings.Repeat("f", 32)

func unsignedCall(from string) relay.ProxyCall {
	return relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: from, ID: nextCallID(), Sent: time.Now().Unix()}
}

// TestNoCallWithoutAKeyIsServed holds every device to signing: a phone, a
// browser or an instance, in a group or outside one, known or not.
func TestNoCallWithoutAKeyIsServed(t *testing.T) {
	m, serve := guarded(t)
	letIn(t, m, serve, memberKeyOf(30), "phone-known", relay.KindMobile)
	outside := newManager(t).Guard(func(context.Context, relay.ProxyCall) (int, []byte) { return http.StatusOK, nil })
	for _, from := range []string{"", "phone-known", "phone-other", browserID, nasID} {
		for name, s := range map[string]relay.ProxyHandler{"in a group": serve, "outside a group": outside} {
			if status, body := s(context.Background(), unsignedCall(from)); status != http.StatusForbidden || codeOf(body) != "memberUnsigned" {
				t.Errorf("an unsigned call naming %q %s = %d %s, want 403 memberUnsigned", from, name, status, body)
			}
		}
	}
}

// TestACallMustNameAnIdItsKindUses refuses a key for an id that no device of
// the kind it claims would mint, so no key is pinned under another kind's id.
func TestACallMustNameAnIdItsKindUses(t *testing.T) {
	m, serve := guarded(t)
	for i, c := range []struct{ id, kind string }{
		{"phone-old", relay.KindInstance},
		{browserID, relay.KindInstance},
		{nasID, relay.KindMobile},
		{nasID, relay.KindExtension},
		{"phone-Old!", relay.KindMobile},
		{"phone-x", relay.KindExtension},
		{"browser-xyz", relay.KindExtension},
		{browserID, relay.KindMobile},
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
	if _, _, err := m.Approve(nasID, memberKeyOf(91).Public().(ed25519.PublicKey), relay.KindExtension); err != ErrKindMismatch {
		t.Errorf("an approval passed on for a browser under an instance's id = %v, want ErrKindMismatch", err)
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(92), browserID, relay.KindExtension)); codeOf(body) != "memberPending" {
		t.Errorf("a browser under its own kind of id = %d %s, want it waiting for approval", status, body)
	}
}

// TestAnInstancesIDCannotBePinnedAsABrowser: whoever holds the words signs as
// a browser under an approved instance's id first. Nothing is pinned for it,
// and refusing or removing that id as a device never bans the instance.
func TestAnInstancesIDCannotBePinnedAsABrowser(t *testing.T) {
	m, serve := guarded(t)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{{InstanceID: nasID, Name: "nas"}}})
	if status, body := serve(context.Background(), signedCall(memberKeyOf(220), nasID, relay.KindExtension)); codeOf(body) != "memberKind" {
		t.Fatalf("a browser under the NAS's id = %d %s, want 403 memberKind", status, body)
	}
	if pending := m.PendingMembers(); len(pending) != 0 {
		t.Fatalf("pinned %+v under the NAS's id", pending)
	}
	if err := m.Refuse(nasID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveApp(nasID, time.Now()); err != ErrIsInstance {
		t.Errorf("removing the NAS's id as a phone or a browser = %v, want ErrIsInstance", err)
	}
	if m.Removed(nasID) {
		t.Fatal("refusing a device under the NAS's id banned the NAS")
	}
	nas := memberKeyOf(221)
	letIn(t, m, serve, nas, nasID, relay.KindInstance)
	if status, body := serve(context.Background(), signedCall(nas, nasID, relay.KindInstance)); status != http.StatusOK {
		t.Errorf("the real NAS once approved = %d %s, want it served", status, body)
	}
}

// TestABrowserCardUnderAnInstancesIDIsNeverKept: a client announces an
// approved instance's id as its member id. No card is made for it, a card a
// damaged or older group file has under such an id goes, and the instance
// keeps its say in the group.
func TestABrowserCardUnderAnInstancesIDIsNeverKept(t *testing.T) {
	m, serve := guarded(t)
	nas := memberKeyOf(230)
	letIn(t, m, serve, nas, nasID, relay.KindInstance)
	m.group.mu.Lock()
	m.group.state.Apps = map[string]KnownApp{officeID: {Name: "office", Deployment: relay.KindExtension, LastSeen: time.Now()}}
	m.group.mu.Unlock()
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: nasID, Name: "nas"},
		{InstanceID: strings.Repeat("9", 40), Client: true, Deployment: relay.KindExtension, Name: "nas", Member: nasID},
		{InstanceID: "phone-impostor", Client: true, Deployment: relay.KindExtension, Name: "phone"},
		{InstanceID: strings.Repeat("8", 40), Client: true, Deployment: relay.KindMobile, Name: "laptop", Member: laptopID},
	}})
	apps, err := m.Apps(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("cards = %+v, want none for ids that are not a phone's or a browser's", apps)
	}
	if status, body := serve(context.Background(), signedCall(nas, nasID, relay.KindInstance)); status != http.StatusOK {
		t.Errorf("the NAS after the owner opened the Pairing page = %d %s, want it served", status, body)
	}
	pin := m.group.state.Keys[nasID]
	if !m.ApprovedInstance(Caller{ID: nasID, Kind: relay.KindInstance, Key: pin.Key}) {
		t.Error("the NAS lost its say in the group")
	}
	if err := m.RemoveApp(nasID, time.Now()); err != ErrIsInstance {
		t.Errorf("removing the NAS's id as a card = %v, want ErrIsInstance", err)
	}
	if m.Removed(nasID) {
		t.Error("the NAS's id was banned")
	}
}

// TestRefusingAnInstanceWithoutAKeyDoesNotBanIt: an instance keeps its id when
// it leaves, so a refusal passed on for one this instance has no key of takes
// nothing away for good.
func TestRefusingAnInstanceWithoutAKeyDoesNotBanIt(t *testing.T) {
	m, serve := guarded(t)
	if err := m.Refuse(nasID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if m.Removed(nasID) {
		t.Error("refusing an instance without a key banned its id")
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(93), nasID, relay.KindInstance)); codeOf(body) != "memberPending" {
		t.Errorf("the instance signing in afterwards = %d %s, want it waiting for approval", status, body)
	}
	if err := m.Refuse("phone-q", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !m.Removed("phone-q") {
		t.Error("refusing a phone without a key left its id free")
	}
}

// TestAnInstanceEnteringTheWordsApprovesTheInstancesAlreadyThere: the
// instances an instance meets right after its owner entered the words of a
// group wait for approval there like any other device, the real ones as well
// as one somebody holding the words announces at that moment.
func TestAnInstanceEnteringTheWordsApprovesTheInstancesAlreadyThere(t *testing.T) {
	m := newManager(t)
	joined := time.Now()
	if err := m.SetJoined(joined); err != nil {
		t.Fatal(err)
	}
	evilID := strings.Repeat("e0", 20)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{{InstanceID: officeID, Name: "office"}, {InstanceID: evilID, Name: "office"}}})
	office, evil := memberKeyOf(40), memberKeyOf(41)
	for _, c := range []relay.ProxyCall{signedCall(office, officeID, relay.KindInstance), signedCall(evil, evilID, relay.KindInstance)} {
		caller, r := m.admit(c, "", joined.Add(time.Second))
		if r == nil || codeOf(r.body) != "memberPending" {
			t.Errorf("%s signing right after the words were entered = %+v, want it waiting for approval", c.From, r)
		}
		if m.ApprovedInstance(Caller{ID: c.From, Kind: relay.KindInstance, Key: c.Key}) || caller.ID != "" {
			t.Errorf("%s counts as an approved instance", c.From)
		}
	}
	if _, _, err := m.Approve(officeID, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, r := m.admit(signedCall(office, officeID, relay.KindInstance), "", joined.Add(2*time.Second)); r != nil {
		t.Errorf("the office once its owner approved it here = %s, want it served", r.body)
	}
}

func TestADamagedGroupFileStopsTheStart(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveApp("phone-gone", time.Now()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "group.json")
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, whole[:len(whole)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a cut-off group.json loaded as a group with nobody taken out")
	}
	if err := os.WriteFile(path, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Removed("phone-gone") {
		t.Error("the removal did not survive a restart")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".group.json.tmp-*")); len(left) != 0 {
		t.Errorf("writing the file left %v behind", left)
	}
}
