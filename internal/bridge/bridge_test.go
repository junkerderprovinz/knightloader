package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// remotePassword is the password the fake remote is locked with.
const remotePassword = "correct horse battery"

// sessionCookie mirrors auth.CookieName. It is spelled out rather than imported
// so this package stays independent of the app it talks to.
const sessionCookie = "kl_session"

type linksBody struct {
	Links     string   `json:"links"`
	Package   string   `json:"package"`
	Passwords []string `json:"passwords"`
}

type optionsBody struct {
	Ids      []string `json:"ids"`
	Password string   `json:"password"`
}

// fakeRemote stands in for a KnightLoader instance. It records what the bridge
// sent and can be locked and expired, which is how the tests reach the auth
// paths without running a real app.
type fakeRemote struct {
	srv *httptest.Server

	mu sync.Mutex
	// locked mirrors an instance with a password set: guarded routes want the
	// session cookie the login handed out.
	locked bool
	// session is the token the remote currently accepts; empty means nobody is
	// logged in, which is also what an expired session looks like from outside.
	session      string
	logins       int
	linkAttempts int // every POST /api/links, including the ones answered 401
	links        []linksBody
	options      []optionsBody
	ids          []string // the task ids POST /api/links reports back
}

func newFakeRemote(t *testing.T, locked bool, ids ...string) *fakeRemote {
	t.Helper()
	f := &fakeRemote{locked: locked, ids: ids}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		// Open even while locked, exactly like the real instance.
		writeJSON(w, map[string]string{"status": "ok", "version": "test"})
	})
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if body.Password != remotePassword {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.logins++
		f.session = fmt.Sprintf("session-%d", f.logins)
		token := f.session
		f.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/"})
		writeJSON(w, map[string]bool{"enabled": true, "authenticated": true})
	})
	mux.HandleFunc("POST /api/links", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.linkAttempts++
		f.mu.Unlock()
		if !f.authorize(w, r) {
			return
		}
		var body linksBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.links = append(f.links, body)
		ids := slices.Clone(f.ids)
		f.mu.Unlock()

		out := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, map[string]string{"id": id})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/tasks/options", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorize(w, r) {
			return
		}
		var body optionsBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.options = append(f.options, body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// authorize mirrors the real guard: once a password is set, an API call needs
// the session cookie that the login handed out.
func (f *fakeRemote) authorize(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	locked, want := f.locked, f.session
	f.mu.Unlock()
	if !locked {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || want == "" || c.Value != want {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// expire invalidates the session the bridge is holding, the way the real
// instance does once the cookie's TTL runs out.
func (f *fakeRemote) expire() {
	f.mu.Lock()
	f.session = ""
	f.mu.Unlock()
}

func (f *fakeRemote) snapshot() (logins, attempts int, links []linksBody, options []optionsBody) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins, f.linkAttempts, slices.Clone(f.links), slices.Clone(f.options)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newBridge aims a Bridge at a fake remote with a timeout short enough that a
// hung test fails rather than stalls.
func newBridge(t *testing.T, f *fakeRemote, password string) *Bridge {
	t.Helper()
	b, err := New(Options{Remote: f.srv.URL, Password: password, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

// captureLog redirects the standard logger into buf for one test. Tests that
// use it must not run in parallel.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(out); log.SetFlags(flags) })
	return &buf
}

// TestAddLinksCnLPostsLinksAndPackage pins the core relay. If it failed, the
// remote would never see the links, or would stage them under the wrong
// package, which is the entire job of the bridge.
func TestAddLinksCnLPostsLinksAndPackage(t *testing.T) {
	f := newFakeRemote(t, false)
	b := newBridge(t, f, "")

	b.AddLinksCnL([]string{"https://a.example/1", "https://b.example/2"}, "MySite", nil)

	_, _, links, options := f.snapshot()
	if len(links) != 1 {
		t.Fatalf("POST /api/links happened %d times, want 1", len(links))
	}
	if want := "https://a.example/1\nhttps://b.example/2"; links[0].Links != want {
		t.Fatalf("links = %q, want the urls newline separated as %q", links[0].Links, want)
	}
	if links[0].Package != "MySite" {
		t.Fatalf("package = %q, want MySite", links[0].Package)
	}
	// Without passwords the options endpoint must stay untouched, otherwise
	// every plain submission would clear the task's password.
	if len(options) != 0 {
		t.Fatalf("POST /api/tasks/options happened %d times for a submission without passwords, want 0", len(options))
	}
}

// TestAddLinksCnLIgnoresEmptySubmission guards against a pointless round trip:
// the CnL listener rejects empty lists itself, but a stray call must not stage
// an empty package on the remote either.
func TestAddLinksCnLIgnoresEmptySubmission(t *testing.T) {
	f := newFakeRemote(t, false)
	b := newBridge(t, f, "")

	b.AddLinksCnL(nil, "MySite", nil)

	if _, attempts, _, _ := f.snapshot(); attempts != 0 {
		t.Fatalf("POST /api/links attempted %d times for an empty submission, want 0", attempts)
	}
}

// TestLockedRemoteIsLoggedInThenLinksGoThrough pins the auth handshake. If it
// failed, every CnL click against a password-protected instance would be lost
// with a 401.
func TestLockedRemoteIsLoggedInThenLinksGoThrough(t *testing.T) {
	f := newFakeRemote(t, true, "task-1")
	b := newBridge(t, f, remotePassword)

	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", nil)

	logins, attempts, links, _ := f.snapshot()
	if logins != 1 {
		t.Fatalf("logins = %d, want exactly 1", logins)
	}
	if attempts != 2 {
		t.Fatalf("POST /api/links attempted %d times, want 2 (the 401 and the retry)", attempts)
	}
	if len(links) != 1 || links[0].Links != "https://a.example/1" {
		t.Fatalf("delivered links = %v, want the one url to arrive after the login", links)
	}
}

// TestExpiredSessionTriggersExactlyOneReLogin pins the self-healing path. A
// bridge runs for weeks, so a session that dies mid-life must cost one login
// and one retry. If it re-logged in per request the remote would be hammered;
// if it did not re-login at all, every submission after the expiry would be
// dropped.
func TestExpiredSessionTriggersExactlyOneReLogin(t *testing.T) {
	f := newFakeRemote(t, true, "task-1")
	b := newBridge(t, f, remotePassword)

	if err := b.Check(context.Background()); err != nil {
		t.Fatalf("Check against a locked remote with the right password: %v", err)
	}
	if logins, _, _, _ := f.snapshot(); logins != 1 {
		t.Fatalf("logins after Check = %d, want 1", logins)
	}

	// This one rides the session Check established, so it must not log in again.
	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", nil)
	if logins, attempts, _, _ := f.snapshot(); logins != 1 || attempts != 1 {
		t.Fatalf("logins=%d attempts=%d after a submission on a healthy session, want 1 and 1", logins, attempts)
	}

	f.expire()
	b.AddLinksCnL([]string{"https://a.example/2"}, "CnL", nil)

	logins, attempts, links, _ := f.snapshot()
	if logins != 2 {
		t.Fatalf("logins = %d, want exactly one re-login after the session expired", logins)
	}
	if attempts != 3 {
		t.Fatalf("POST /api/links attempted %d times, want 3 (ok, 401, one retry)", attempts)
	}
	if len(links) != 2 || links[1].Links != "https://a.example/2" {
		t.Fatalf("delivered links = %v, want both submissions to land", links)
	}
}

// TestConcurrentSubmissionsShareOneReLogin pins the epoch guard. The CnL
// listener calls the bridge from its handler goroutines, so several submissions
// can trip over the same expired session at once. Without the guard each of
// them would log in separately, and the remote would see a burst of logins for
// what is really one expiry.
func TestConcurrentSubmissionsShareOneReLogin(t *testing.T) {
	f := newFakeRemote(t, true, "task-1")
	b := newBridge(t, f, remotePassword)

	if err := b.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	f.expire()

	const submissions = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range submissions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // fire them all at the dead session together
			b.AddLinksCnL([]string{fmt.Sprintf("https://a.example/%d", i)}, "CnL", nil)
		}()
	}
	close(start)
	wg.Wait()

	logins, _, links, _ := f.snapshot()
	if logins != 2 {
		t.Fatalf("logins = %d, want 2 (the Check login plus one shared re-login)", logins)
	}
	if len(links) != submissions {
		t.Fatalf("delivered %d of %d submissions, want every one to survive the expiry", len(links), submissions)
	}
}

// TestWrongPasswordIsReportedNotRetried pins that a bad password fails loudly
// and once. Retrying it would lock nothing out but would bury the real cause.
func TestWrongPasswordIsReportedNotRetried(t *testing.T) {
	f := newFakeRemote(t, true, "task-1")
	b := newBridge(t, f, "not the password")
	buf := captureLog(t)

	err := b.Check(context.Background())
	if err == nil {
		t.Fatal("Check with a wrong password returned nil, want an error")
	}
	if !strings.Contains(err.Error(), "rejected the configured password") {
		t.Fatalf("Check error = %v, want it to name the rejected password", err)
	}

	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", nil)
	if _, attempts, links, _ := f.snapshot(); attempts != 1 || len(links) != 0 {
		t.Fatalf("attempts=%d delivered=%d, want a single rejected attempt", attempts, len(links))
	}
	if !strings.Contains(buf.String(), "dropped") {
		t.Fatalf("log = %q, want the dropped links to be reported", buf.String())
	}
}

// TestPasswordsRideWithTheLinks pins that every password a submission carried
// reaches the remote. They used to be posted separately to an endpoint that
// takes exactly one, so anything past the first was lost between the website
// and the NAS with nothing to show for it.
func TestPasswordsRideWithTheLinks(t *testing.T) {
	cases := []struct {
		name      string
		passwords []string
		want      []string
	}{
		{name: "no passwords", passwords: nil, want: nil},
		{name: "empty list", passwords: []string{}, want: nil},
		{name: "one password", passwords: []string{"secret"}, want: []string{"secret"}},
		{name: "all of them survive", passwords: []string{"secret", "other"}, want: []string{"secret", "other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRemote(t, false, "id-1", "id-2")
			b := newBridge(t, f, "")

			b.AddLinksCnL([]string{"https://a.example/1", "https://a.example/2"}, "CnL", tc.passwords)

			_, _, links, options := f.snapshot()
			if len(options) != 0 {
				t.Fatalf("posted to /api/tasks/options %d times; passwords travel with the links now", len(options))
			}
			if len(links) != 1 {
				t.Fatalf("POST /api/links happened %d times, want once", len(links))
			}
			if !slices.Equal(links[0].Passwords, tc.want) {
				t.Fatalf("passwords = %v, want %v", links[0].Passwords, tc.want)
			}
		})
	}
}

// TestDeadRemoteLogsAndDoesNotPanic pins the failure everyone will hit at some
// point: the NAS is off. Losing the links is unavoidable then, but taking the
// CnL listener down with a panic, or dropping them in silence, is not.
func TestDeadRemoteLogsAndDoesNotPanic(t *testing.T) {
	// Closing the server immediately leaves an address nothing answers on.
	srv := httptest.NewServer(http.NewServeMux())
	addr := srv.URL
	srv.Close()

	b, err := New(Options{Remote: addr, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	buf := captureLog(t)

	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", []string{"secret"})

	logged := buf.String()
	if !strings.Contains(logged, addr) {
		t.Fatalf("log = %q, want the unreachable remote %s named in it", logged, addr)
	}
	if !strings.Contains(logged, "dropped") {
		t.Fatalf("log = %q, want the lost links called out", logged)
	}
	if err := b.Check(context.Background()); err == nil {
		t.Fatal("Check against a dead remote returned nil, want an error")
	}
}

// TestNewRejectsUnusableRemote pins the early validation. A remote that is not
// an http(s) base URL can never work, and failing at startup beats logging a
// mangled URL on every click forever.
func TestNewRejectsUnusableRemote(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		wantErr bool
		want    string // normalised base URL when it is accepted
	}{
		{name: "empty", remote: "", wantErr: true},
		{name: "blank", remote: "   ", wantErr: true},
		{name: "no scheme", remote: "nas:8749", wantErr: true},
		{name: "wrong scheme", remote: "ftp://nas", wantErr: true},
		{name: "no host", remote: "http://", wantErr: true},
		{name: "plain http", remote: "http://nas:8749", want: "http://nas:8749"},
		{name: "trailing slash trimmed", remote: "https://nas:8749/", want: "https://nas:8749"},
		{name: "surrounding space trimmed", remote: "  http://nas:8749/  ", want: "http://nas:8749"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := New(Options{Remote: tc.remote})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("New(%q) returned no error, want one", tc.remote)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q): %v", tc.remote, err)
			}
			// A trailing slash would make every path "//api/links".
			if b.Remote() != tc.want {
				t.Fatalf("Remote() = %q, want %q", b.Remote(), tc.want)
			}
			if b.timeout != DefaultTimeout {
				t.Fatalf("timeout = %v, want the default %v when none is given", b.timeout, DefaultTimeout)
			}
		})
	}
}

// TestUnlockedRemoteNeverLogsIn pins that an instance without a password costs
// no auth traffic at all, which is the common case.
func TestUnlockedRemoteNeverLogsIn(t *testing.T) {
	f := newFakeRemote(t, false, "task-1")
	b := newBridge(t, f, "")

	if err := b.Check(context.Background()); err != nil {
		t.Fatalf("Check against an open remote: %v", err)
	}
	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", nil)

	logins, attempts, links, _ := f.snapshot()
	if logins != 0 {
		t.Fatalf("logins = %d against an unlocked remote, want 0", logins)
	}
	if attempts != 1 || len(links) != 1 {
		t.Fatalf("attempts=%d delivered=%d, want one clean delivery", attempts, len(links))
	}
}
