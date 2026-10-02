package api

// The accounts page's routes for the own Usenet servers.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestUsenetServerRoutes(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	reg := newRegistry()
	registerUsenetServers(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	news := nntptest.New(t)
	news.User, news.Pass = "reader", "secret"

	send := func(method, path string, body any) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return resp.StatusCode, b.Bytes()
	}

	server := map[string]any{"host": news.Host, "port": news.Port, "enabled": true, "username": "reader", "password": "secret"}
	code, out := send(http.MethodPost, "/api/usenet/servers", server)
	if code != http.StatusOK {
		t.Fatalf("save answered %d: %s", code, out)
	}
	if bytes.Contains(out, []byte("secret")) {
		t.Fatal("the listing carries the password")
	}
	rows := a.Settings.Get().UsenetServers
	if len(rows) != 1 || rows[0].ID != "127.0.0.1" || rows[0].Connections != settings.DefaultUsenetConnections {
		t.Fatalf("stored rows = %+v", rows)
	}
	if login := a.UsenetLogin("127.0.0.1"); login.Username != "reader" || !login.HasPassword {
		t.Fatalf("login = %+v", login)
	}

	// The stored password is tested through the placeholder, and a wrong one
	// is reported in the answer.
	stored := map[string]any{"id": "127.0.0.1", "host": news.Host, "port": news.Port, "username": "reader", "password": "********"}
	if _, out := send(http.MethodPost, "/api/usenet/servers/test", stored); !bytes.Contains(out, []byte(`"ok":true`)) {
		t.Fatalf("test of the stored login answered %s", out)
	}
	stored["password"] = "wrong"
	if _, out := send(http.MethodPost, "/api/usenet/servers/test", stored); !bytes.Contains(out, []byte(`"ok":false`)) {
		t.Fatalf("test of a wrong login answered %s", out)
	}

	// A second server on the same host gets an id of its own.
	if code, out := send(http.MethodPost, "/api/usenet/servers", server); code != http.StatusOK {
		t.Fatalf("second save answered %d: %s", code, out)
	}
	if rows := a.Settings.Get().UsenetServers; len(rows) != 2 || rows[1].ID != "127.0.0.1-2" {
		t.Fatalf("stored rows = %+v", rows)
	}

	bad := map[string]any{"host": "news.example.com:563"}
	if code, _ := send(http.MethodPost, "/api/usenet/servers", bad); code != http.StatusBadRequest {
		t.Fatalf("a host with a port in it answered %d", code)
	}

	if code, _ := send(http.MethodDelete, "/api/usenet/servers/127.0.0.1", nil); code != http.StatusNoContent {
		t.Fatalf("delete answered %d", code)
	}
	if login := a.UsenetLogin("127.0.0.1"); login.Username != "" {
		t.Fatal("the login outlived its server")
	}
	if code, _ := send(http.MethodDelete, "/api/usenet/servers/127.0.0.1", nil); code != http.StatusNotFound {
		t.Fatalf("a second delete answered %d", code)
	}
}
