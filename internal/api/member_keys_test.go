package api

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
)

// onOwnRelay points an instance at relayURL, the way the Pairing page's own
// relay setting does.
func onOwnRelay(t *testing.T, token, base, relayURL string) {
	t.Helper()
	if code, body := call(t, token, http.MethodPut, base+"/api/relay/config", map[string]any{"relayUrl": relayURL, "mode": "own"}); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config answered %d: %s", code, body)
	}
}

// relayCall is one call a member of the group sends over the relay by hand:
// a hello in id's name, then call to target, signed by member when it is not
// nil. It returns what target answered.
func relayCall(t *testing.T, relayURL string, secret []byte, id, target string, member ed25519.PrivateKey, call relay.ProxyCall) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(relayURL, "http")+"/relay/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	frameKey := relay.DeriveFrameKey(secret)
	sealedID, err := relay.SealIdentity(frameKey, id, relay.Identity{Name: "by hand", Deployment: "mobile", Client: true})
	if err != nil {
		t.Fatal(err)
	}
	write := func(typ string, data any) {
		frame, err := relay.Encode(typ, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
			t.Fatal(err)
		}
	}
	write(relay.TypeHello, relay.Hello{Key: relay.DeriveKey(secret), Announce: relay.Announce{InstanceID: id, Sealed: sealedID}})
	requestID := hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	sealed, err := relay.SealSignedCall(frameKey, member, requestID, target, call)
	if err != nil {
		t.Fatal(err)
	}
	write(relay.TypeProxyRequest, relay.ProxyRequest{RequestID: requestID, Target: target, Sealed: sealed})
	for {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no answer from %s: %v", target, err)
		}
		env, err := relay.Decode(frame)
		if err != nil || env.Type != relay.TypeProxyResponse {
			continue
		}
		var resp relay.ProxyResponse
		if err := env.Into(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error != "" {
			t.Fatalf("the relay answered for %s: %s", target, resp.Error)
		}
		res, err := relay.OpenResult(frameKey, requestID, resp.Sealed)
		if err != nil {
			t.Fatal(err)
		}
		return res.Status, res.Body
	}
}

func refusalCode(body []byte) string {
	var r struct{ Code string }
	_ = json.Unmarshal(body, &r)
	return r.Code
}

func waitForMember(t *testing.T, a *app.App, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range a.Federation.Members() {
			if m.RelayID == id {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never showed up as a member", id)
}

// TestTwoInstancesSignTheirCallsAndNobodyElseSpeaksForThem runs two instances
// and a relay on real sockets. With their Pairing cards open, each instance
// lists the other as waiting for approval, whichever entered the words, and
// each owner approves the other; an instance somebody holding the words
// announces at that moment waits too. A call in an instance's name under
// another key, or without one, is refused with a code that says why, and so
// is an unsigned call naming nobody; approvals and removals made by the owner
// of the first reach the second, while a phone cannot pass one on; and once
// the second instance is taken out, it forgets the phrase and its old key is
// refused.
func TestTwoInstancesSignTheirCallsAndNobodyElseSpeaksForThem(t *testing.T) {
	t.Parallel()
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()
	first, firstApp, firstToken := pairingServer(t)
	second, secondApp, secondToken := pairingServer(t)
	phrase := activate(t, firstToken, first.URL)
	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/join", map[string]string{"phrase": phrase}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, body)
	}
	onOwnRelay(t, firstToken, first.URL, relaySrv.URL)
	onOwnRelay(t, secondToken, second.URL, relaySrv.URL)
	firstID, secondID := groupID(firstApp), groupID(secondApp)
	waitForMember(t, firstApp, secondID)
	waitForMember(t, secondApp, firstID)
	secret, err := seedphrase.Decode(phrase)
	if err != nil {
		t.Fatal(err)
	}

	// Both Pairing cards are open: each instance greets the other, so each
	// lists the other as waiting.
	waitingOn := func(token, base, id string) bool {
		for _, p := range connectInfoOf(t, token, base).Pending {
			if p.ID == id && p.Kind == relay.KindInstance && p.Fingerprint != "" {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		firstLists, secondLists := waitingOn(firstToken, first.URL, secondID), waitingOn(secondToken, second.URL, firstID)
		if firstLists && secondLists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the instances do not list each other as waiting: first %+v, second %+v",
				connectInfoOf(t, firstToken, first.URL).Pending, connectInfoOf(t, secondToken, second.URL).Pending)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code, body := call(t, secondToken, http.MethodGet, second.URL+"/api/instances/"+firstID+"/tasks", nil); code != http.StatusForbidden || refusalCode(body) != "memberPending" {
		t.Fatalf("the second instance asking the first before it was approved = %d %s, want 403 memberPending", code, body)
	}
	if !connectInfoOf(t, secondToken, second.URL).Awaiting {
		t.Error("the second instance does not say it waits for approval")
	}

	// Somebody holding the words announces an instance of their own while the
	// two meet. It waits like any other.
	impostor := ed25519.NewKeyFromSeed([]byte(strings.Repeat("i", ed25519.SeedSize)))
	impostorID := relay.MemberID(relay.KindInstance, impostor.Public().(ed25519.PublicKey))
	if code, body := relayCall(t, relaySrv.URL, secret, impostorID, secondID, impostor, relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: impostorID, Kind: relay.KindInstance}); code != http.StatusForbidden || refusalCode(body) != "memberPending" {
		t.Errorf("an instance announced by somebody holding the words = %d %s, want 403 memberPending", code, body)
	}

	if code, body := call(t, secondToken, http.MethodPost, second.URL+"/api/connect/devices/"+firstID+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the first instance on the second = %d %s", code, body)
	}
	if code, body := call(t, firstToken, http.MethodGet, first.URL+"/api/instances/"+secondID+"/tasks", nil); code != http.StatusOK {
		t.Fatalf("the first instance asking the second = %d %s", code, body)
	}
	st, _ := secondApp.Federation.Group(false, time.Now())
	pin, ok := st.Keys[firstID]
	if !ok || pin.Kind != relay.KindInstance || string(pin.Key) != string(memberKey(firstApp).Public().(ed25519.PublicKey)) {
		t.Fatalf("the second instance pinned %+v for the first, want the first's own member key as an instance", st.Keys)
	}

	tasks := relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: firstID, Kind: relay.KindInstance}
	forger := ed25519.NewKeyFromSeed([]byte(strings.Repeat("f", ed25519.SeedSize)))
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-forger", secondID, forger, tasks); code != http.StatusForbidden || refusalCode(body) != "memberKind" {
		t.Errorf("another key in the first instance's name = %d %s, want 403 memberKind", code, body)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-forger", secondID, nil, tasks); code != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("an unsigned call in the first instance's name = %d %s, want 403 memberUnsigned", code, body)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-older", secondID, nil, relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"}); code != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("an unsigned call naming nobody = %d %s, want 403 memberUnsigned", code, body)
	}

	// The first approves the second too.
	if code, body := call(t, firstToken, http.MethodPost, first.URL+"/api/connect/devices/"+secondID+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the second instance = %d %s", code, body)
	}
	if code, body := call(t, secondToken, http.MethodGet, second.URL+"/api/instances/"+firstID+"/tasks", nil); code != http.StatusOK {
		t.Fatalf("the second instance asking the first once approved = %d %s", code, body)
	}

	// A phone is approved on the first instance, and the second honours it.
	phone := ed25519.NewKeyFromSeed([]byte(strings.Repeat("p", ed25519.SeedSize)))
	phoneID := relay.MemberID(relay.KindMobile, phone.Public().(ed25519.PublicKey))
	asPhone := relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: phoneID, Kind: relay.KindMobile}
	if code, body := relayCall(t, relaySrv.URL, secret, phoneID, firstID, phone, asPhone); code != http.StatusForbidden || refusalCode(body) != "memberPending" {
		t.Fatalf("a new phone = %d %s, want 403 memberPending", code, body)
	}
	if code, body := call(t, firstToken, http.MethodPost, first.URL+"/api/connect/devices/"+phoneID+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the phone = %d %s", code, body)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		code, body := relayCall(t, relaySrv.URL, secret, phoneID, secondID, phone, asPhone)
		if code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the phone approved on the first instance = %d %s on the second, want the approval passed on", code, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-older", secondID, nil, relay.ProxyCall{Method: http.MethodPost, Path: "/api/connect/devices/phone-forged/approve", Body: []byte(`{"key":"AAAA"}`)}); code != http.StatusForbidden {
		t.Errorf("an unsigned member passing an approval on = %d %s, want it refused", code, body)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, phoneID, secondID, phone, relay.ProxyCall{Method: http.MethodDelete, Path: "/api/connect/apps/phone-older", From: phoneID, Kind: relay.KindMobile}); code != http.StatusForbidden || refusalCode(body) != "decisionNotFromInstance" {
		t.Errorf("an approved phone passing a removal on = %d %s, want it refused", code, body)
	}

	// The owner takes the phone out on the first instance, and the second
	// turns it away too.
	if code, body := call(t, firstToken, http.MethodDelete, first.URL+"/api/connect/apps/"+phoneID, nil); code != http.StatusNoContent {
		t.Fatalf("removing the phone = %d %s", code, body)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		code, body := relayCall(t, relaySrv.URL, secret, phoneID, secondID, phone, asPhone)
		if code == http.StatusGone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the phone removed on the first instance = %d %s on the second, want the removal passed on", code, body)
		}
		time.Sleep(50 * time.Millisecond)
	}

	secondKey := memberKey(secondApp)
	if code, body := call(t, firstToken, http.MethodDelete, first.URL+"/api/connect/members/"+secondID, nil); code != http.StatusNoContent {
		t.Fatalf("taking the second instance out = %d %s", code, body)
	}
	byOldKey := relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: secondID, Kind: relay.KindInstance}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-replay", firstID, secondKey, byOldKey); code != http.StatusGone {
		t.Errorf("the removed instance's old key = %d %s, want 410", code, body)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if stored, _ := secondApp.Accounts.Get(relay.SeedAccountService); stored == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second instance kept the phrase after the owner of the first took it out")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
