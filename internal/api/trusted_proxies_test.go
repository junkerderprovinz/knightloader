package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// TestTheServedRelayBelievesOnlyTheTrustedProxies: the relay an instance
// serves counts failed handshakes per caller, and a caller on the same
// network is not a proxy unless the list says so.
func TestTheServedRelayBelievesOnlyTheTrustedProxies(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		srv, a := testServer(t)
		if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","serve":true}`); code != http.StatusOK || !put.Serve {
			t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
		}
		if trusted {
			trustLoopback(t, a)
		}
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/relay/connect"
		dial := func(from string) (*websocket.Conn, int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"X-Forwarded-For": {from}}})
			if err != nil {
				if resp == nil {
					t.Fatalf("dial: %v", err)
				}
				return nil, resp.StatusCode
			}
			return c, http.StatusSwitchingProtocols
		}
		blocked := false
		for range 30 {
			c, code := dial("203.0.113.66")
			if code == http.StatusTooManyRequests {
				blocked = true
				break
			}
			hello, _ := relay.Encode(relay.TypeHello, relay.Hello{Key: "not-this-relays-key"})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = c.Write(ctx, websocket.MessageText, hello)
			for {
				if _, _, err := c.Read(ctx); err != nil {
					break
				}
			}
			cancel()
			_ = c.CloseNow()
		}
		if !blocked {
			t.Fatal("the failing caller was never blocked, so this test proves nothing")
		}
		c, code := dial("198.51.100.7")
		if c != nil {
			_ = c.CloseNow()
		}
		if trusted && code == http.StatusTooManyRequests {
			t.Error("behind a trusted proxy, another caller was refused with the failing caller's 429")
		}
		if !trusted && code != http.StatusTooManyRequests {
			t.Errorf("with no trusted proxy, a header naming another caller answered %d, want the shared 429", code)
		}
		srv.Close()
	}
}

func trustLoopback(t *testing.T, a *app.App) {
	t.Helper()
	if _, err := a.PatchSettings(map[string]json.RawMessage{"trustedProxies": json.RawMessage(`["127.0.0.1","::1"]`)}); err != nil {
		t.Fatal(err)
	}
}
