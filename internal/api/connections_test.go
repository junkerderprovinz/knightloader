package api

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

// connectionsServer is the connection routes on a throwaway app. registerSettings
// comes along because one test reads the list back the way a browser does.
func connectionsServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerConnections(reg, a)
	registerSettings(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// postConn posts to one of these routes and decodes the answer, failing on
// anything but a 200 so a test never asserts against an error page.
func postConn[T any](t *testing.T, srv *httptest.Server, path string, body any) T {
	t.Helper()
	code, raw := postJSON(t, http.MethodPost, srv.URL+path, body)
	if code != http.StatusOK {
		t.Fatalf("POST %s answered %d: %s", path, code, raw)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return v
}

// recordingProxy is an HTTP proxy that answers a CONNECT and reports the Basic
// credential it was offered, which is how these tests see what the server sent
// without ever asking the server to hand a password back.
func recordingProxy(t *testing.T, accept string) (host string, port int, seen <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				br := bufio.NewReader(c)
				offered := ""
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if h := strings.TrimSpace(line); strings.HasPrefix(h, "Proxy-Authorization: Basic ") {
						raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Proxy-Authorization: Basic "))
						offered = string(raw)
					}
					if strings.TrimSpace(line) == "" {
						break
					}
				}
				ch <- offered
				if offered != accept {
					_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
					return
				}
				_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
			}()
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return h, n, ch
}

// store puts one connection in the settings the way a save would, and hands back
// the row as the server holds it — with the ID Sanitize assigned.
func storeConnection(t *testing.T, a *app.App, e proxycfg.Entry) proxycfg.Entry {
	t.Helper()
	s := a.Settings.Get()
	s.Connections = []proxycfg.Entry{e}
	applied, err := a.ApplySettings(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Connections) != 1 {
		t.Fatalf("the connection was dropped on the way in: %+v", applied.Connections)
	}
	return applied.Connections[0]
}

// TestTestingARowDoesNotAskForThePasswordAgain is the behaviour the whole
// redact-and-merge arrangement exists for, seen from the one place it is easiest
// to get wrong.
//
// The client was never sent the password, so it cannot send one back. If the
// test route probed with what the client posted, every test of a saved proxy
// would fail on credentials that are perfectly correct — and the obvious "fix"
// for that is a form that makes the user retype the password to change a host
// filter.
func TestTestingARowDoesNotAskForThePasswordAgain(t *testing.T) {
	a, srv := connectionsServer(t)
	host, port, seen := recordingProxy(t, "alice:secret")
	stored := storeConnection(t, a, proxycfg.Entry{
		Kind: proxycfg.KindHTTP, Host: host, Port: port,
		Username: "alice", Password: "secret", Enabled: true,
	})

	// What the browser holds: the row as it was served, which is redacted, with
	// an edit that does not touch the connection itself.
	edited := stored.Redacted()
	edited.Filter = []string{"example.org"}
	edited.MaxDownloads = 3
	if edited.Password != "" {
		t.Fatalf("the row the client holds still has a password: %+v", edited)
	}

	report := postConn[proxycfg.Report](t, srv, "/api/connections/test",
		map[string]any{"entry": edited, "target": "example.org"})
	if !report.OK {
		t.Fatalf("the probe failed: %+v", report)
	}
	if got := <-seen; got != "alice:secret" {
		t.Errorf("the proxy was offered %q, want the stored credentials", got)
	}
}

// TestAnEditedEndpointDoesNotTakeTheStoredPasswordWithIt is the other half, and
// it is a security property rather than a convenience: the client posting this
// row is the one the password was withheld from, so a row it has re-pointed at a
// machine it controls must not arrive there carrying the secret.
//
// The page has to say so, because from the user's side this looks like the
// password being forgotten for no reason.
func TestAnEditedEndpointDoesNotTakeTheStoredPasswordWithIt(t *testing.T) {
	a, srv := connectionsServer(t)
	host, port, seen := recordingProxy(t, "alice:secret")
	stored := storeConnection(t, a, proxycfg.Entry{
		Kind: proxycfg.KindHTTP, Host: host, Port: port,
		Username: "alice", Password: "secret", Enabled: true,
	})

	moved := stored.Redacted()
	moved.Username = "mallory"

	report := postConn[proxycfg.Report](t, srv, "/api/connections/test",
		map[string]any{"entry": moved, "target": "example.org"})
	if report.OK {
		t.Errorf("the probe succeeded with credentials the client never sent: %+v", report)
	}
	got := <-seen
	if strings.Contains(got, "secret") {
		t.Errorf("the stored password followed the row to a new user name: offered %q", got)
	}
	if got != "mallory:" {
		t.Errorf("the proxy was offered %q, want only what the client sent", got)
	}
}

// TestARedactedRowIsTellableFromARowWithNoPassword. Without this the form shows
// an empty password box for a working proxy, the user concludes the password was
// lost, and types it in again — which is the retyping this whole arrangement was
// built to avoid.
func TestARedactedRowIsTellableFromARowWithNoPassword(t *testing.T) {
	a, srv := connectionsServer(t)
	s := a.Settings.Get()
	s.Connections = []proxycfg.Entry{
		{Kind: proxycfg.KindHTTP, Host: "with.example", Port: 8080, Username: "alice", Password: "secret", Enabled: true},
		{Kind: proxycfg.KindHTTP, Host: "without.example", Port: 8080, Username: "bob", Enabled: true},
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	r, err := srv.Client().Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	body, _ := io.ReadAll(r.Body)
	if bytes.Contains(body, []byte("secret")) {
		t.Fatalf("the settings response carries a proxy password: %s", body)
	}

	var served struct {
		Connections []proxycfg.Entry `json:"connections"`
	}
	if err := json.Unmarshal(body, &served); err != nil {
		t.Fatal(err)
	}
	if len(served.Connections) != 2 {
		t.Fatalf("served %d connections, want 2", len(served.Connections))
	}
	if !served.Connections[0].HasPassword {
		t.Error("the row with a stored password does not say so")
	}
	if served.Connections[1].HasPassword {
		t.Error("the row with no password claims to have one")
	}
}

// TestImportStoresNothing. The refusals are only worth naming if somebody gets
// to read them before the list is committed, and the page holds an unsaved draft
// that a write here would silently disagree with.
func TestImportStoresNothing(t *testing.T) {
	a, srv := connectionsServer(t)
	got := postConn[proxycfg.Import](t, srv, "/api/connections/import", map[string]any{
		"text": "http://proxy.lan:8080\nsocks5://alice:secret@proxy.example.org:1080",
	})
	if len(got.Entries) != 2 {
		t.Fatalf("parsed %d entries, want 2", len(got.Entries))
	}
	if n := len(a.Settings.Get().Connections); n != 0 {
		t.Errorf("the import wrote %d connections; it must only parse", n)
	}
}

// TestImportRefusesAgainstWhatIsAlreadyConfigured. The stored list is the half
// the client cannot check for itself: the browser holds a draft, and a duplicate
// of a row already saved is exactly the one a user cannot see coming.
func TestImportRefusesAgainstWhatIsAlreadyConfigured(t *testing.T) {
	a, srv := connectionsServer(t)
	storeConnection(t, a, proxycfg.Entry{Kind: proxycfg.KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true})

	got := postConn[proxycfg.Import](t, srv, "/api/connections/import", map[string]any{
		"text": "http://proxy.lan:8080\nsocks5://proxy.example.org:1080\nnonsense",
	})
	if len(got.Entries) != 1 {
		t.Fatalf("parsed %d entries, want 1: %+v", len(got.Entries), got.Entries)
	}
	if len(got.Rejected) != 2 {
		t.Fatalf("refused %d lines, want 2: %+v", len(got.Rejected), got.Rejected)
	}
	if got.Rejected[0].Line != 1 || !strings.Contains(got.Rejected[0].Reason, "already in the list") {
		t.Errorf("the duplicate of a stored row was not named: %+v", got.Rejected[0])
	}
	if got.Rejected[1].Line != 3 {
		t.Errorf("the unreadable line is reported at %d, want 3", got.Rejected[1].Line)
	}
}

// TestConnectionRoutesNeedASession: neither of these may answer without one.
// Import reads the stored connection list, and test dials whatever it is given
// with the stored credentials — an open test route is a port scanner with the
// user's passwords attached.
func TestConnectionRoutesNeedASession(t *testing.T) {
	reg := newRegistry()
	registerConnections(reg, testApp(t))
	for _, r := range reg.Routes() {
		if r.Open {
			t.Errorf("%s %s answers without a session", r.Method, r.Path)
		}
	}
}
