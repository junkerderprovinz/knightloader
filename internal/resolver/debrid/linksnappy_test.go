package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// lsAccountFake answers the account calls on a session cookie AUTHENTICATE
// hands out, each sign-in a new one, as Linksnappy mails a confirmation for
// a sign-in from a new place.
type lsAccountFake struct {
	srv *httptest.Server

	mu       sync.Mutex
	logins   int
	sessions map[string]bool
	calls    map[string]int
	// down answers USERDETAILS with a page of the proxy in front.
	down bool
}

func newLsAccountFake(t *testing.T) *lsAccountFake {
	f := &lsAccountFake{sessions: map[string]bool{}, calls: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *lsAccountFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api")
	f.calls[path]++
	if path == "/AUTHENTICATE" {
		f.logins++
		session := fmt.Sprintf("s%d", f.logins)
		f.sessions[session] = true
		http.SetCookie(w, &http.Cookie{Name: "lslogin", Value: session, Path: "/"})
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":"Logged in"}`)
		return
	}
	if path == "/USERDETAILS" && f.down {
		http.Error(w, "<html>Bad gateway</html>", http.StatusBadGateway)
		return
	}
	if c, err := r.Cookie("lslogin"); err != nil || !f.sessions[c.Value] {
		_, _ = io.WriteString(w, `{"status":"ERROR","error":"Please login first.","return":false}`)
		return
	}
	switch path {
	case "/USERDETAILS":
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{"type":"elite","expire":"lifetime"}}`)
	case "/FILEHOSTS":
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{"rapidgator.net":{"Status":"1","canDownload":1}}}`)
	case "/linkgen":
		_, _ = io.WriteString(w, `{"links":[{"status":"OK","generated":"https://cdn.example/a.bin","filename":"a.bin","size":"12"}]}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *lsAccountFake) endSessions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.sessions)
}

func (f *lsAccountFake) goDown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = true
}

func (f *lsAccountFake) signIns() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins
}

func (f *lsAccountFake) asked(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *lsAccountFake) client(kept SessionStore) *Linksnappy {
	l := NewLinksnappy("u", "p")
	l.base = f.srv.URL + "/api"
	l.Sessions = kept
	return l
}

// memorySessions keeps sessions as the app's sealed store does.
type memorySessions struct {
	mu   sync.Mutex
	kept map[string]string
}

func (m *memorySessions) Session(user, pass string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.kept[user+"\x00"+pass]
}

func (m *memorySessions) KeepSession(user, pass, cookies string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.kept == nil {
		m.kept = map[string]string{}
	}
	m.kept[user+"\x00"+pass] = cookies
}

func TestLinksnappySignsInOnceForTheHostsAnUnlockAndTheAccount(t *testing.T) {
	f := newLsAccountFake(t)
	l := f.client(nil)
	ctx := context.Background()

	if _, err := l.Hosts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Unlock(ctx, "https://rapidgator.net/file/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Account(ctx); err != nil {
		t.Fatal(err)
	}
	if logins := f.signIns(); logins != 1 {
		t.Errorf("signed in %d times, want once", logins)
	}
}

func TestLinksnappyClientsShareTheKeptSession(t *testing.T) {
	f := newLsAccountFake(t)
	kept := &memorySessions{}
	ctx := context.Background()

	if _, err := f.client(kept).Hosts(ctx); err != nil {
		t.Fatal(err)
	}
	if got := kept.Session("u", "p"); got != "lslogin=s1" {
		t.Fatalf("kept %q, want the session the sign-in opened", got)
	}
	// A client built afresh, as for every account reading and after a
	// restart, takes up the kept session.
	info, err := f.client(kept).Account(ctx)
	if err != nil || info.Tier != "elite" {
		t.Fatalf("Account = %+v, %v", info, err)
	}
	if err := f.client(kept).Authenticate(ctx); err != nil {
		t.Fatal(err)
	}
	if logins, details := f.signIns(), f.asked("/USERDETAILS"); logins != 1 || details < 2 {
		t.Errorf("signed in %d times with %d account readings, want once, the kept session checked on USERDETAILS", logins, details)
	}
}

func TestLinksnappySignsInAgainOnlyOnceTheKeptSessionStoppedWorking(t *testing.T) {
	f := newLsAccountFake(t)
	kept := &memorySessions{}
	kept.KeepSession("u", "p", "lslogin=gone")
	ctx := context.Background()

	if _, err := f.client(kept).Account(ctx); err != nil {
		t.Fatal(err)
	}
	if logins := f.signIns(); logins != 1 {
		t.Errorf("signed in %d times, want once for the session that stopped working", logins)
	}
	if got := kept.Session("u", "p"); got != "lslogin=s1" {
		t.Errorf("kept %q, want the new session", got)
	}

	f.endSessions()
	l := f.client(kept)
	if _, err := l.Account(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Hosts(ctx); err != nil {
		t.Fatal(err)
	}
	if logins := f.signIns(); logins != 2 {
		t.Errorf("signed in %d times, want a second sign-in once Linksnappy ended the first session", logins)
	}
}

func TestLinksnappyDoesNotSignInAfreshWhileTheServiceIsOutOfReach(t *testing.T) {
	f := newLsAccountFake(t)
	kept := &memorySessions{}
	ctx := context.Background()
	if err := f.client(kept).Authenticate(ctx); err != nil {
		t.Fatal(err)
	}

	f.goDown()
	if err := f.client(kept).Authenticate(ctx); err == nil {
		t.Error("a check that met the proxy's error page passed")
	}
	if logins := f.signIns(); logins != 1 {
		t.Errorf("signed in %d times, want the kept session left alone during the outage", logins)
	}
	if got := kept.Session("u", "p"); got != "lslogin=s1" {
		t.Errorf("kept %q, want the session kept through the outage", got)
	}
}
