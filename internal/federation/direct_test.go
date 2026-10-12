package federation

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// TestASlowStrangerCannotTakeTheDirectCallSlots: everything a stranger needs
// to pass the checks before the body is in the clear, so the slots must not
// be taken before the body has arrived and been verified.
func TestASlowStrangerCannotTakeTheDirectCallSlots(t *testing.T) {
	secret := []byte("0123456789abcdef")
	callee, officeID := directCallee(t, secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, func(context.Context, relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte("served")
		})
	}))
	defer srv.Close()

	// Each stranger sends the headers and one byte of a body it says is far
	// longer, and then nothing.
	for i := 0; i < 16; i++ {
		c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: x\r\n%s: stranger\r\n%s: %s\r\n%s: %d\r\nContent-Length: %d\r\n\r\n{",
			DirectPath, headerPeer, headerTarget, officeID, headerTime, time.Now().Unix(), maxDirectCall)
	}
	time.Sleep(100 * time.Millisecond)

	caller := callerOf(t, secret, callee, officeID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, status, err := caller.callDirect(ctx, srv.URL, officeID, http.MethodGet, "/api/tasks", nil)
	if err != nil || status != http.StatusOK || string(body) != "served" {
		t.Fatalf("a member's call = %d %q %v, want it served while strangers dawdle", status, body, err)
	}
}

// TestCallsWithoutAPinnedKeyUseUpNoIDs: whatever reaches the direct route
// from somebody holding the words but no pinned key, signed or not, leaves the
// replay guard as it was, so nobody fills the record members' calls are
// checked against.
func TestCallsWithoutAPinnedKeyUseUpNoIDs(t *testing.T) {
	secret := []byte("0123456789abcdef")
	callee, officeID := directCallee(t, secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, callee.Guard(func(context.Context, relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte("served")
		}))
	}))
	defer srv.Close()
	for _, c := range []struct {
		self   string
		member ed25519.PrivateKey
	}{
		{bravoID, nil},
		{"not-an-instance-id", memberKeyOf(60)},
	} {
		caller := callerOf(t, secret, callee, officeID)
		caller.SetGroup(secret, c.self, c.member)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, status, err := caller.callDirect(ctx, srv.URL, officeID, http.MethodGet, "/api/tasks", nil)
		cancel()
		if err != nil || status != http.StatusForbidden {
			t.Errorf("a call from %s = %d %v, want it refused with a reason", c.self, status, err)
		}
	}
	if n := callee.replay.Len(); n != 0 {
		t.Errorf("the replay guard kept %d ids of calls without a pinned key, want none", n)
	}
}

// TestABodyThatNeverFinishesIsRefused also checks that a member's call whose
// answer outlasts the body deadline is still served.
func TestABodyThatNeverFinishesIsRefused(t *testing.T) {
	defer func(d time.Duration) { directReadTimeout = d }(directReadTimeout)
	directReadTimeout = 200 * time.Millisecond

	secret := []byte("0123456789abcdef")
	callee, officeID := directCallee(t, secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
			time.Sleep(2 * directReadTimeout)
			if ctx.Err() != nil {
				return http.StatusInternalServerError, nil
			}
			return http.StatusOK, []byte(strconv.Itoa(len(call.Body)))
		})
	}))
	defer srv.Close()

	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: x\r\n%s: stranger\r\n%s: %s\r\n%s: %d\r\nContent-Length: 100\r\n\r\n{",
		DirectPath, headerPeer, headerTarget, officeID, headerTime, time.Now().Unix())
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	answer, _ := io.ReadAll(c)
	if !strings.HasPrefix(string(answer), "HTTP/1.1 403") {
		t.Fatalf("a body that never finished got %q, want it refused once its time ran out", answer)
	}

	caller := callerOf(t, secret, callee, officeID)
	body, status, err := caller.callDirect(context.Background(), srv.URL, officeID, http.MethodPost, "/api/links", []byte(`{"links":"x"}`))
	if err != nil || status != http.StatusOK || string(body) != "13" {
		t.Fatalf("got %d %q %v, want the call served with its context intact", status, body, err)
	}
}

// directCallee is a member of the group secret makes, with a member key of its
// own, and the id it answers direct calls under.
func directCallee(t *testing.T, secret []byte) (*Manager, string) {
	t.Helper()
	key := memberKeyOf(70)
	id := idOf(key, relay.KindInstance)
	callee := newManager(t)
	callee.SetGroup(secret, id, key)
	return callee, id
}

// callerOf is another member of the group, which has approved callee as
// calleeID.
func callerOf(t *testing.T, secret []byte, callee *Manager, calleeID string) *Manager {
	t.Helper()
	caller := newManager(t)
	caller.SetGroup(secret, bravoID, nil)
	if _, _, err := caller.Approve(calleeID, callee.keys.member.Public().(ed25519.PublicKey), relay.KindInstance); err != nil {
		t.Fatal(err)
	}
	return caller
}

// TestADirectAnswerCountsOnlyFromAnApprovedKey: an answer over the direct
// route reaches the caller only signed by the key the target's id is made
// from, and only once the target is approved here.
func TestADirectAnswerCountsOnlyFromAnApprovedKey(t *testing.T) {
	secret := []byte("0123456789abcdef")
	callee, officeID := directCallee(t, secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, func(context.Context, relay.ProxyCall) (int, []byte) {
			return http.StatusGone, []byte("removed")
		})
	}))
	defer srv.Close()

	stranger := newManager(t)
	stranger.SetGroup(secret, bravoID, nil)
	if _, _, err := stranger.callDirect(context.Background(), srv.URL, officeID, http.MethodGet, "/api/tasks", nil); err == nil {
		t.Error("an answer from an instance nobody approved here was taken")
	}
	caller := callerOf(t, secret, callee, officeID)
	if _, status, err := caller.callDirect(context.Background(), srv.URL, officeID, http.MethodGet, "/api/tasks", nil); err != nil || status != http.StatusGone {
		t.Errorf("the approved target's answer = %d %v, want its 410", status, err)
	}
	callee.SetGroup(secret, officeID, nil)
	if _, _, err := caller.callDirect(context.Background(), srv.URL, officeID, http.MethodGet, "/api/tasks", nil); err == nil {
		t.Error("an unsigned answer was taken")
	}
}

// TestAnInstanceHearsItWaitsBeforeApprovingTheOther: two instances meet, and
// neither has approved the other yet. The other one's answer that this one
// waits for approval there still reaches the Pairing card, though nothing
// else in an answer from an instance not approved here counts.
func TestAnInstanceHearsItWaitsBeforeApprovingTheOther(t *testing.T) {
	secret := []byte("0123456789abcdef")
	callee, officeID := directCallee(t, secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, callee.Guard(func(context.Context, relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte(`{"secret":"x"}`)
		}))
	}))
	defer srv.Close()

	caller := newManager(t)
	key := memberKeyOf(71)
	caller.SetGroup(secret, idOf(key, relay.KindInstance), key)
	caller.SetDiscovery(&fakeLocal{members: []discovery.Peer{{ID: officeID, URL: srv.URL}}})
	body, _, err := caller.Proxy(context.Background(), officeID, http.MethodGet, "/api/auth", nil)
	if err == nil || body != nil {
		t.Fatalf("an answer from an instance not approved here = %q %v, want none", body, err)
	}
	if !caller.AwaitingApproval(time.Now()) {
		t.Error("the other instance said this one waits for approval there, and the card does not show it")
	}
}
