package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dialWithOrigin opens a WebSocket at path on srvURL carrying origin, and
// returns the handshake's status.
func dialWithOrigin(t *testing.T, srvURL, path, origin string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srvURL, "http")+path, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}},
	})
	if err == nil {
		c.CloseNow()
		return http.StatusSwitchingProtocols
	}
	if resp == nil {
		t.Fatalf("dial %s with Origin %s: %v", path, origin, err)
	}
	return resp.StatusCode
}

func TestTheRelaySocketTakesBrowserExtensions(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	for _, origin := range []string{
		"chrome-extension://abcdefghijklmnopabcdefghijklmnop",
		"moz-extension://0f4e5c2a-8b1d-4c7e-9a3f-2d6b8e1c5a7f",
		"safari-web-extension://0F4E5C2A-8B1D-4C7E-9A3F-2D6B8E1C5A7F",
	} {
		if got := dialWithOrigin(t, srv.URL, "/relay/connect", origin); got != http.StatusSwitchingProtocols {
			t.Errorf("relay handshake from %s answered %d, want 101", origin, got)
		}
	}
}

func TestTheRelaySocketStillRefusesOtherSites(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	for _, origin := range []string{
		"https://evil.example",
		"http://chrome-extension.example",
		"null",
	} {
		if got := dialWithOrigin(t, srv.URL, "/relay/connect", origin); got != http.StatusForbidden {
			t.Errorf("relay handshake from %s answered %d, want 403", origin, got)
		}
	}
}

func TestAnExtensionOriginOpensNothingButTheRelaySocket(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	if got := dialWithOrigin(t, srv.URL, "/api/ws", "chrome-extension://abcdefghijklmnopabcdefghijklmnop"); got != http.StatusForbidden {
		t.Errorf("the UI socket from an extension answered %d, want 403", got)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/queue", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "moz-extension://0f4e5c2a-8b1d-4c7e-9a3f-2d6b8e1c5a7f")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET /api/queue from an extension answered %d, want 403", resp.StatusCode)
	}
}
