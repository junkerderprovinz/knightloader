package api

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
)

// callUnder sends one call to target over the relay at relayURL under
// relayKey, sealed under frameKey and signed by member, and returns the
// answer. answered is false when none came within wait or the relay answered
// instead of the target.
func callUnder(t *testing.T, relayURL, relayKey string, frameKey []byte, target string, member ed25519.PrivateKey, call relay.ProxyCall, wait time.Duration) (status int, body []byte, answered bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(relayURL, "http")+"/relay/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	const self = "phone-old-words"
	sealedID, err := relay.SealIdentity(frameKey, self, relay.Identity{Name: "old phone", Deployment: relay.KindMobile, Client: true})
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
	write(relay.TypeHello, relay.Hello{Key: relayKey, Announce: relay.Announce{InstanceID: self, Sealed: sealedID}})
	requestID := hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	call.From, call.Kind = self, relay.KindMobile
	sealed, err := relay.SealSignedCall(frameKey, member, requestID, target, call)
	if err != nil {
		t.Fatal(err)
	}
	write(relay.TypeProxyRequest, relay.ProxyRequest{RequestID: requestID, Target: target, Sealed: sealed})
	for {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			return 0, nil, false
		}
		env, err := relay.Decode(frame)
		if err != nil || env.Type != relay.TypeProxyResponse {
			continue
		}
		var resp relay.ProxyResponse
		if err := env.Into(&resp); err != nil || resp.Error != "" {
			return 0, nil, false
		}
		res, err := relay.OpenResult(frameKey, requestID, resp.Sealed)
		if err != nil {
			t.Fatal(err)
		}
		return res.Status, res.Body, true
	}
}

// TestAResetTellsTheOldGroupToPairAgain: after a password reset a phone with
// the old words learns that it has to pair again, whatever it asks, while
// somebody on the old relay key without the old words learns nothing, and the
// instance keeps the old group's keys but not its words.
func TestAResetTellsTheOldGroupToPairAgain(t *testing.T) {
	t.Parallel()
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()
	srv, a, token := pairingServer(t)
	phrase := activate(t, token, srv.URL)
	onOwnRelay(t, token, srv.URL, relaySrv.URL)
	old, err := seedphrase.Decode(phrase)
	if err != nil {
		t.Fatal(err)
	}
	self := groupID(a)
	if _, err := renewGroup(a); err != nil {
		t.Fatal(err)
	}
	phone := ed25519.NewKeyFromSeed([]byte(strings.Repeat("o", ed25519.SeedSize)))

	for _, call := range []relay.ProxyCall{
		{Method: http.MethodGet, Path: "/api/tasks"},
		{Method: http.MethodPost, Path: "/api/queue/stop"},
		{Method: http.MethodPost, Path: "/api/connect/devices/phone-old-words/approve"},
	} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			status, body, answered := callUnder(t, relaySrv.URL, relay.DeriveKey(old), relay.DeriveFrameKey(old), self, phone, call, 2*time.Second)
			if answered {
				if status != http.StatusGone || string(body) != "reset" {
					t.Errorf("%s %s with the old words = %d %s, want 410 reset", call.Method, call.Path, status, body)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s %s with the old words got no answer, want 410 reset", call.Method, call.Path)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if a.Queue().Halted {
		t.Error("a call under the old words halted the queue")
	}

	other, _, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if status, body, answered := callUnder(t, relaySrv.URL, relay.DeriveKey(old), relay.DeriveFrameKey(other), self, phone,
		relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"}, 500*time.Millisecond); answered {
		t.Errorf("a call on the old relay key without the old words = %d %s, want no answer", status, body)
	}

	stored, _ := a.Accounts.Get(retiredAccountService)
	if stored == "" || strings.Contains(stored, hex.EncodeToString(old)) {
		t.Errorf("the instance keeps %q of the old group, want its relay and frame key and not its words", stored)
	}
}

// TestTheOldGroupHearsNothingAfterThirtyDays: once the time is up the instance
// leaves the old relay key and forgets its keys.
func TestTheOldGroupHearsNothingAfterThirtyDays(t *testing.T) {
	t.Parallel()
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()
	srv, a, token := pairingServer(t)
	phrase := activate(t, token, srv.URL)
	onOwnRelay(t, token, srv.URL, relaySrv.URL)
	old, err := seedphrase.Decode(phrase)
	if err != nil {
		t.Fatal(err)
	}
	self := groupID(a)
	if err := retireGroup(a, time.Now().Add(-retiredFor-time.Minute)); err != nil {
		t.Fatal(err)
	}
	fresh, _, err := seedphrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := enterGroup(a, fresh); err != nil {
		t.Fatal(err)
	}
	phone := ed25519.NewKeyFromSeed([]byte(strings.Repeat("o", ed25519.SeedSize)))
	time.Sleep(300 * time.Millisecond)
	if status, body, answered := callUnder(t, relaySrv.URL, relay.DeriveKey(old), relay.DeriveFrameKey(old), self, phone,
		relay.ProxyCall{Method: http.MethodGet, Path: "/api/tasks"}, time.Second); answered {
		t.Errorf("the old words after thirty days = %d %s, want no answer", status, body)
	}
	if stored, _ := a.Accounts.Get(retiredAccountService); stored != "" {
		t.Errorf("the old group's keys are still kept after thirty days: %q", stored)
	}
	if status, _ := retiredAnswer(time.Now().Add(-time.Second))(context.Background(), relay.ProxyCall{}); status != relay.StatusDrop {
		t.Errorf("an answer past its time = %d, want none", status)
	}
}
