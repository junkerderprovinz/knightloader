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
// and a relay on real sockets. The first instance's calls are signed and its
// key pinned on the second; a call in its name under another key, or without
// one, is refused with a code that says why; a caller on the release before
// keys still gets through; and once the second instance is taken out, its old
// key is refused even after it left.
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
	firstID, secondID := firstApp.Settings.Get().InstanceID, secondApp.Settings.Get().InstanceID
	waitForMember(t, firstApp, secondID)
	waitForMember(t, secondApp, firstID)
	secret, err := seedphrase.Decode(phrase)
	if err != nil {
		t.Fatal(err)
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
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-forger", secondID, forger, tasks); code != http.StatusForbidden || refusalCode(body) != "memberKeyChanged" {
		t.Errorf("another key in the first instance's name = %d %s, want 403 memberKeyChanged", code, body)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-forger", secondID, nil, tasks); code != http.StatusForbidden || refusalCode(body) != "memberUnsigned" {
		t.Errorf("an unsigned call in the first instance's name = %d %s, want 403 memberUnsigned", code, body)
	}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-older", secondID, nil, relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"}); code != http.StatusOK {
		t.Errorf("a member on the release before keys = %d %s, want it served", code, body)
	}

	if code, body := call(t, secondToken, http.MethodGet, second.URL+"/api/instances/"+firstID+"/tasks", nil); code != http.StatusOK {
		t.Fatalf("the second instance asking the first = %d %s", code, body)
	}
	secondKey := memberKey(secondApp)
	if code, body := call(t, firstToken, http.MethodDelete, first.URL+"/api/connect/members/"+secondID, nil); code != http.StatusNoContent {
		t.Fatalf("taking the second instance out = %d %s", code, body)
	}
	byOldKey := relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks", From: secondID, Kind: relay.KindInstance}
	if code, body := relayCall(t, relaySrv.URL, secret, "phone-replay", firstID, secondKey, byOldKey); code != http.StatusGone {
		t.Errorf("the removed instance's old key = %d %s, want 410", code, body)
	}
}
