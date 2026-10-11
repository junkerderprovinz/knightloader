package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

// TestMembersFromBeforeKeysAreLetInWithoutApproval covers the update: the
// phones a group knew, members calling without keys and instances announcing
// without them are approved when they first sign, and nobody new is.
func TestMembersFromBeforeKeysAreLetInWithoutApproval(t *testing.T) {
	dir := t.TempDir()
	old, err := json.Marshal(map[string]any{
		"joinedAt": time.Now().Add(-48 * time.Hour),
		"apps":     map[string]any{"phone-old": map[string]any{"name": "Pixel", "lastSeen": time.Now()}},
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
	serve := m.Guard(func(context.Context, relay.ProxyCall) (int, []byte) { return http.StatusOK, nil })
	m.noteLegacy([]string{nasID})
	serve(context.Background(), relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: "browser-old"})

	for _, c := range []relay.ProxyCall{
		signedCall(memberKeyOf(30), "phone-old", relay.KindMobile),
		signedCall(memberKeyOf(31), nasID, relay.KindInstance),
		signedCall(memberKeyOf(32), "browser-old", relay.KindExtension),
	} {
		if status, body := serve(context.Background(), c); status != http.StatusOK {
			t.Errorf("%s from before keys = %d %s, want it served", c.From, status, body)
		}
	}
	if status, body := serve(context.Background(), signedCall(memberKeyOf(33), "phone-new", relay.KindMobile)); codeOf(body) != "memberPending" {
		t.Errorf("a device new since the update = %d %s, want it waiting", status, body)
	}

	if err := m.RemoveMember(nasID, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.noteLegacy([]string{nasID})
	if status, body := serve(context.Background(), signedCall(memberKeyOf(34), nasID, relay.KindInstance)); codeOf(body) != "memberPending" {
		t.Errorf("an instance from before keys that was taken out and came back = %d %s, want it waiting", status, body)
	}
}

// TestAnInstanceEnteringTheWordsTakesTheInstancesAlreadyThere checks the
// joining side: the instances it meets at first count as approved, phones do
// not, and later instances wait.
func TestAnInstanceEnteringTheWordsTakesTheInstancesAlreadyThere(t *testing.T) {
	m := newManager(t)
	joined := time.Now()
	if err := m.SetJoined(joined, true); err != nil {
		t.Fatal(err)
	}
	if _, r := m.admit(signedCall(memberKeyOf(40), officeID, relay.KindInstance), joined.Add(time.Minute)); r != nil {
		t.Errorf("an instance met right after entering the words = %s, want it served", r.body)
	}
	if _, r := m.admit(signedCall(memberKeyOf(41), "phone-p", relay.KindMobile), joined.Add(time.Minute)); r == nil {
		t.Error("a phone met right after entering the words was let in without approval")
	}
	if _, r := m.admit(signedCall(memberKeyOf(42), bravoID, relay.KindInstance), joined.Add(trustWindow+time.Minute)); r == nil {
		t.Error("an instance met long after entering the words was let in without approval")
	}
}

// TestOnlyMembersAnnouncingWithoutKeysCountAsFromBefore: an instance or a
// phone that announces it signs is new, and waits for approval.
func TestOnlyMembersAnnouncingWithoutKeysCountAsFromBefore(t *testing.T) {
	m, serve := guarded(t)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: nasID, Name: "nas"},
		{InstanceID: laptopID, Name: "laptop", Signs: true},
		{InstanceID: "phone-old-relay", Client: true, Deployment: "mobile"},
		{InstanceID: "phone-new-relay", Client: true, Deployment: "mobile", Signs: true},
	}})
	m.List()
	if _, err := m.Apps(time.Now()); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		id, kind, code string
	}{
		{nasID, relay.KindInstance, ""},
		{laptopID, relay.KindInstance, "memberPending"},
		{"phone-old-relay", relay.KindMobile, ""},
		{"phone-new-relay", relay.KindMobile, "memberPending"},
	} {
		status, body := serve(context.Background(), signedCall(memberKeyOf(byte(60+i)), c.id, c.kind))
		if (c.code == "" && status != http.StatusOK) || (c.code != "" && codeOf(body) != c.code) {
			t.Errorf("%s = %d %s, want %q", c.id, status, body, c.code)
		}
	}
}
