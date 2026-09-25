package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const remotePassword = "correct horse battery"

// sessionCookie mirrors auth.CookieName without importing the app.
const sessionCookie = "kl_session"

type linksBody struct {
	Links     string   `json:"links"`
	Package   string   `json:"package"`
	Origin    string   `json:"origin"`
	Passwords []string `json:"passwords"`
}

type optionsBody struct {
	Ids      []string `json:"ids"`
	Password string   `json:"password"`
}

type containerUpload struct {
	filename string
	data     []byte
	pkg      string
}

// fakeRemote stands in for a KnightLoader instance. It records what the
// bridge sent and can be locked and expired.
type fakeRemote struct {
	srv *httptest.Server

	mu     sync.Mutex
	locked bool
	// session is the token the remote accepts; empty means nobody is logged
	// in, which is also how an expired session looks.
	session      string
	logins       int
	linkAttempts int // every POST /api/links, including the ones answered 401
	links        []linksBody
	options      []optionsBody
	ids          []string // the task ids POST /api/links reports back

	containerAttempts int // every POST /api/containers, including the ones answered 401
	containers        []containerUpload
	// containerFails makes /api/containers answer like an instance without a
	// JD backend.
	containerFails bool
	// cookiePath is where the session cookie applies, the base path of a
	// remote behind a proxy that mounts it under one.
	cookiePath string
}

func newFakeRemote(t *testing.T, locked bool, ids ...string) *fakeRemote {
	t.Helper()
	f := &fakeRemote{locked: locked, ids: ids, cookiePath: "/"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
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
		token, path := f.session, f.cookiePath
		f.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: path})
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
	mux.HandleFunc("POST /api/containers", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.containerAttempts++
		fails := f.containerFails
		f.mu.Unlock()
		if !f.authorize(w, r) {
			return
		}
		if fails {
			http.Error(w, "this container is encrypted, and only the headless JDownloader backend can open it; none is configured", http.StatusServiceUnavailable)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "send the container as a multipart form field named \"file\"", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "could not read the uploaded file", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.containers = append(f.containers, containerUpload{filename: header.Filename, data: data, pkg: r.FormValue("package")})
		f.mu.Unlock()
		writeJSON(w, map[string]any{"kind": "dlc", "handedTo": "jd", "expiresIn": 120})
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

// newFakeRemoteUnder is a fake remote that answers only under base and limits
// its session cookie to it, as a KnightLoader behind a reverse proxy at that
// path does.
func newFakeRemoteUnder(t *testing.T, base string, locked bool, ids ...string) *fakeRemote {
	t.Helper()
	f := newFakeRemote(t, locked, ids...)
	f.mu.Lock()
	f.cookiePath = base
	f.mu.Unlock()
	mounted := httptest.NewServer(http.StripPrefix(base, f.srv.Config.Handler))
	t.Cleanup(mounted.Close)
	f.srv = mounted
	return f
}

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

func (f *fakeRemote) snapshotContainers() (attempts int, uploads []containerUpload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.containerAttempts, slices.Clone(f.containers)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

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
	if links[0].Origin != "cnl" {
		t.Fatalf("origin = %q, want cnl: these links reached this process by Click'n'Load", links[0].Origin)
	}
	if len(options) != 0 {
		t.Fatalf("POST /api/tasks/options happened %d times for a submission without passwords, want 0", len(options))
	}
}

func TestAddLinksCnLIgnoresEmptySubmission(t *testing.T) {
	f := newFakeRemote(t, false)
	b := newBridge(t, f, "")

	b.AddLinksCnL(nil, "MySite", nil)

	if _, attempts, _, _ := f.snapshot(); attempts != 0 {
		t.Fatalf("POST /api/links attempted %d times for an empty submission, want 0", attempts)
	}
}

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

// TestExpiredSessionTriggersExactlyOneReLogin checks that an expired session
// costs one login and one retry, not one login per request and not a dropped
// submission.
func TestExpiredSessionTriggersExactlyOneReLogin(t *testing.T) {
	f := newFakeRemote(t, true, "task-1")
	b := newBridge(t, f, remotePassword)

	if err := b.Check(context.Background()); err != nil {
		t.Fatalf("Check against a locked remote with the right password: %v", err)
	}
	if logins, _, _, _ := f.snapshot(); logins != 1 {
		t.Fatalf("logins after Check = %d, want 1", logins)
	}

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
			<-start
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
				t.Fatalf("posted to /api/tasks/options %d times; passwords travel with the links", len(options))
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

func TestDeadRemoteLogsAndDoesNotPanic(t *testing.T) {
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

// TestARemoteUnderABasePathKeepsItsPrefix covers an instance behind a reverse
// proxy at https://example.com/kl/: every request, and the session cookie that
// lives under /kl, has to go there, whether or not the address was typed with
// a trailing slash.
func TestARemoteUnderABasePathKeepsItsPrefix(t *testing.T) {
	f := newFakeRemoteUnder(t, "/kl", true, "task-1")
	b, err := New(Options{Remote: f.srv.URL + "/kl/", Password: remotePassword, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := b.Check(context.Background()); err != nil {
		t.Fatalf("Check against a remote under /kl: %v", err)
	}
	b.AddLinksCnL([]string{"https://a.example/1"}, "CnL", nil)
	if err := b.AddContainerCnL([]byte("rsa-encrypted-stand-in"), "CnL"); err != nil {
		t.Fatalf("AddContainerCnL against a remote under /kl: %v", err)
	}

	logins, attempts, links, _ := f.snapshot()
	if logins != 1 || attempts != 1 {
		t.Errorf("logins=%d attempts=%d, want one login whose cookie the next request carried", logins, attempts)
	}
	if len(links) != 1 {
		t.Errorf("delivered links = %v, want the submission to land", links)
	}
	if containerAttempts, uploads := f.snapshotContainers(); containerAttempts != 1 || len(uploads) != 1 {
		t.Errorf("container attempts=%d uploads=%d, want one clean upload", containerAttempts, len(uploads))
	}
}

func TestAddContainerCnLPostsMultipartUpload(t *testing.T) {
	f := newFakeRemote(t, false)
	b := newBridge(t, f, "")

	payload := []byte("rsa-encrypted-stand-in")
	if err := b.AddContainerCnL(payload, "CryptedSite"); err != nil {
		t.Fatalf("AddContainerCnL: %v", err)
	}

	attempts, uploads := f.snapshotContainers()
	if attempts != 1 {
		t.Fatalf("POST /api/containers happened %d times, want 1", attempts)
	}
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if string(uploads[0].data) != string(payload) {
		t.Errorf("uploaded bytes = %q, want the original payload %q", uploads[0].data, payload)
	}
	if uploads[0].pkg != "CryptedSite" {
		t.Errorf("package = %q, want CryptedSite", uploads[0].pkg)
	}
	if uploads[0].filename == "" {
		t.Error("uploaded filename is empty; the remote's container detection needs a name to work with")
	}
}

func TestAddContainerCnLIgnoresEmptySubmission(t *testing.T) {
	f := newFakeRemote(t, false)
	b := newBridge(t, f, "")

	if err := b.AddContainerCnL(nil, "MySite"); err != nil {
		t.Fatalf("AddContainerCnL(nil, ...): %v", err)
	}
	if attempts, _ := f.snapshotContainers(); attempts != 0 {
		t.Fatalf("POST /api/containers attempted %d times for an empty submission, want 0", attempts)
	}
}

func TestAddContainerCnLSurfacesARemoteFailure(t *testing.T) {
	f := newFakeRemote(t, false)
	f.containerFails = true
	b := newBridge(t, f, "")

	if err := b.AddContainerCnL([]byte("payload"), "MySite"); err == nil {
		t.Fatal("AddContainerCnL against a remote with no JD backend returned no error")
	}
}

func TestAddContainerCnLRetriesLoginOn401(t *testing.T) {
	f := newFakeRemote(t, true)
	b := newBridge(t, f, remotePassword)

	if err := b.AddContainerCnL([]byte("payload"), "CnL"); err != nil {
		t.Fatalf("AddContainerCnL against a locked remote: %v", err)
	}

	logins, _, _, _ := f.snapshot()
	if logins != 1 {
		t.Fatalf("logins = %d, want exactly 1", logins)
	}
	attempts, uploads := f.snapshotContainers()
	if attempts != 2 {
		t.Fatalf("POST /api/containers attempted %d times, want 2 (the 401 and the retry)", attempts)
	}
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want the one that landed after the login", len(uploads))
	}
}
