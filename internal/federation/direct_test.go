package federation

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// TestASlowStrangerCannotTakeTheDirectCallSlots: everything a stranger needs
// to pass the checks before the body is in the clear, so the slots must not
// be taken before the body has arrived and been verified.
func TestASlowStrangerCannotTakeTheDirectCallSlots(t *testing.T) {
	secret := []byte("0123456789abcdef")
	callee := newManager(t)
	callee.SetGroup(secret, officeID)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callee.ServeDirect(w, r, func(context.Context, relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte("served")
		})
	}))
	defer srv.Close()

	// Each stranger sends the headers and one byte of a body it says is far
	// longer, and then nothing.
	for i := 0; i < maxDirectCalls; i++ {
		c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: x\r\n%s: stranger\r\n%s: %s\r\n%s: %d\r\nContent-Length: %d\r\n\r\n{",
			DirectPath, headerPeer, headerTarget, officeID, headerTime, time.Now().Unix(), maxDirectCall)
	}
	time.Sleep(100 * time.Millisecond)

	caller := newManager(t)
	caller.SetGroup(secret, bravoID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, status, err := caller.callDirect(ctx, srv.URL, officeID, http.MethodGet, "/api/tasks", nil, "")
	if err != nil || status != http.StatusOK || string(body) != "served" {
		t.Fatalf("a member's call = %d %q %v, want it served while strangers dawdle", status, body, err)
	}
}

// TestABodyThatNeverFinishesIsRefused also checks that a member's call whose
// answer outlasts the body deadline is still served.
func TestABodyThatNeverFinishesIsRefused(t *testing.T) {
	defer func(d time.Duration) { directReadTimeout = d }(directReadTimeout)
	directReadTimeout = 200 * time.Millisecond

	secret := []byte("0123456789abcdef")
	callee := newManager(t)
	callee.SetGroup(secret, officeID)
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

	caller := newManager(t)
	caller.SetGroup(secret, bravoID)
	body, status, err := caller.callDirect(context.Background(), srv.URL, officeID, http.MethodPost, "/api/links", []byte(`{"links":"x"}`), "")
	if err != nil || status != http.StatusOK || string(body) != "13" {
		t.Fatalf("got %d %q %v, want the call served with its context intact", status, body, err)
	}
}
