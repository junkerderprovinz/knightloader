package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// NeoDebrid publishes no API reference. The bodies below are the ones JD's
// plugin reads, and the /status body has the shape the live service answers.

const (
	neodebridTestEmail = "amy@example.com"
	neodebridTestPass  = "neodebrid-test-secret"
)

func neodebridAt(base string) *NeoDebrid {
	n := NewNeoDebrid(neodebridTestEmail, neodebridTestPass)
	n.base = base
	return n
}

// neodebridLogins answers /login with a fresh numbered token on every call and
// hands every other path to next.
func neodebridLogins(t *testing.T, logins *atomic.Int32, next http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/login" {
			next(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("email") != neodebridTestEmail || q.Get("password") != neodebridTestPass {
			t.Errorf("/login got email %q and password %q, want the configured pair", q.Get("email"), q.Get("password"))
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","api_token":"token-%d"}`, logins.Add(1))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNeoDebridHostsSkipsDownEntriesAndNeedsNoAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("/status carried %q, want no token and no credentials", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"result":[
			{"host":"Rapidgator.net","status":"Online"},
			{"host":"www.Nitroflare.com","status":"online"},
			{"host":"Www.Uploady.io","status":"Online"},
			{"host":"1Fichier.com","status":"Maintenance"},
			{"host":"DDownload.com","status":"Offline"},
			{"host":"Filer.net"},
			{"host":"Premium","status":"Online"}]}`)
	}))
	defer srv.Close()

	hosts, err := neodebridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "nitroflare.com", "uploady.io", "filer.net"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for h := range hosts {
		if strings.HasPrefix(h, "www.") {
			t.Errorf("Hosts kept the www. prefix on %q", h)
		}
	}
	for _, down := range []string{"1fichier.com", "ddownload.com"} {
		if hosts[down] {
			t.Errorf("%q is marked down but was taken as up", down)
		}
	}
	if hosts["premium"] {
		t.Error("a name without a dot was taken as a domain")
	}
}

// The website's own /hosts page redirects to a login form, so an HTML answer
// is a realistic failure.
func TestNeoDebridHostsRefusesAnHTMLPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<!doctype html><title>Login</title>`)
	}))
	defer srv.Close()

	if hosts, err := neodebridAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatalf("Hosts = %v from an HTML page, want an error", hosts)
	}
}

func TestNeoDebridUnlockLogsInOnceAndReusesTheToken(t *testing.T) {
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("token") != "token-1" || q.Get("link") != "https://rapidgator.net/file/x" {
			t.Errorf("/download got %v, want the session token and the hoster link", q)
		}
		if q.Has("password") || q.Has("email") {
			t.Error("/download carried the login credentials")
		}
		_, _ = io.WriteString(w, `{"status":"success","download":"https://dl.example/one","chunks":"1"}`)
	})

	n := neodebridAt(srv.URL)
	for range 2 {
		got, err := n.Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err != nil {
			t.Fatalf("Unlock: %v", err)
		}
		if got.URL != "https://dl.example/one" {
			t.Errorf("Unlock = %+v, want the download field", got)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Errorf("logged in %d times for two unlocks, want once", got)
	}
}

func TestNeoDebridParallelUnlocksShareOneLogin(t *testing.T) {
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","download":"https://dl.example/one"}`)
	})

	n := neodebridAt(srv.URL)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := n.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
				t.Errorf("Unlock: %v", err)
			}
		})
	}
	wg.Wait()
	if got := logins.Load(); got != 1 {
		t.Errorf("eight parallel unlocks logged in %d times, want once", got)
	}
}

// Dispatch asks for the cap by the task's own host, so it has to be kept
// under the hoster, never under the host of the direct link.
func TestNeoDebridUnlockLearnsTheSmallestChunkCapPerHoster(t *testing.T) {
	answers := []string{
		`{"status":"success","download":"https://dl.example/one","chunks":"4"}`,
		`{"status":"success","download":"https://dl.example/two","chunks":2}`,
		`{"status":"success","download":"https://dl.example/three"}`,
		`{"status":"success","download":"https://dl.example/four","chunks":8}`,
	}
	var logins, calls atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, answers[calls.Add(1)-1])
	})

	n := neodebridAt(srv.URL)
	res := Resolver{Svc: n}
	if got := res.HostCap("rapidgator.net"); got != 0 {
		t.Fatalf("HostCap before any unlock = %d, want 0 (no opinion)", got)
	}
	steps := []struct {
		link string
		want int
	}{
		{"https://rapidgator.net/file/a", 4},
		{"https://www.rapidgator.net/file/b", 2},
		{"https://rapidgator.net/file/c", 2},
		{"https://rapidgator.net/file/d", 2},
	}
	for _, s := range steps {
		if _, err := n.Unlock(context.Background(), s.link); err != nil {
			t.Fatalf("Unlock(%s): %v", s.link, err)
		}
		if got := res.HostCap("rapidgator.net"); got != s.want {
			t.Errorf("after unlocking %s, HostCap = %d, want %d", s.link, got, s.want)
		}
	}
	if got := res.HostCap("dl.example"); got != 0 {
		t.Errorf("HostCap(dl.example) = %d, want 0 for the direct link's host", got)
	}
}

func TestNeoDebridUnlockLogsInAgainWhenTheTokenRunsOut(t *testing.T) {
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "token-1" {
			_, _ = io.WriteString(w, `{"status":"error","reason":"Token not found."}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","download":"https://dl.example/one"}`)
	})

	got, err := neodebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" {
		t.Errorf("Unlock = %+v, want the link from the retry", got)
	}
	if got := logins.Load(); got != 2 {
		t.Errorf("logged in %d times, want a second login after the refused token", got)
	}
}

// Every unlock that held the refused token asks for a new one, and only the
// first of them may log in. A late one must not drop the token the first
// just fetched.
func TestNeoDebridParallelUnlocksShareOneLoginAfterTheTokenRunsOut(t *testing.T) {
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "token-1" {
			_, _ = io.WriteString(w, `{"status":"error","reason":"Token not found."}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","download":"https://dl.example/one"}`)
	})

	n := neodebridAt(srv.URL)
	if err := n.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := n.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
				t.Errorf("Unlock: %v", err)
			}
		})
	}
	wg.Wait()
	if got := logins.Load(); got != 2 {
		t.Errorf("logged in %d times, want the first login and one more for the refused token", got)
	}
}

func TestNeoDebridUnlockLogsInAgainOnlyOnce(t *testing.T) {
	var logins, downloads atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		_, _ = io.WriteString(w, `{"status":"error","reason":"Session expired. Please log-in again."}`)
	})

	_, err := neodebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded although every token was refused")
	}
	if !strings.Contains(err.Error(), "Session expired") {
		t.Errorf("error = %q, want the service's own reason", err)
	}
	if l, d := logins.Load(), downloads.Load(); l != 2 || d != 2 {
		t.Errorf("made %d logins and %d download calls, want two of each", l, d)
	}
}

// A dead file, an unsupported host and a spent quota must not read alike.
// NeoDebrid's wording for a dead file is not known, so it stands for any
// reason JD does not list, which reaches the user unchanged.
func TestNeoDebridUnlockErrorsSayWhatWentWrong(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   []string
		unlike string
	}{
		{"dead file", 200, `{"status":"error","reason":"File not found"}`,
			[]string{"neodebrid /download: File not found"}, "not supported"},
		{"unsupported host", 200, `{"status":"error","reason":"Filehost not supported."}`,
			[]string{"file hoster is not supported", "Filehost not supported."}, "traffic"},
		{"spent quota", http.StatusPaymentRequired, `{"status":"error","reason":"Traffic limit reached"}`,
			[]string{"no traffic left", "Traffic limit reached"}, "not supported"},
		{"spent quota without a body", http.StatusPaymentRequired, ``,
			[]string{"no traffic left"}, "unreadable"},
		{"premium-only host", 200, `{"status":"error","reason":"User not premium"}`,
			[]string{"needs a premium account", "User not premium"}, "traffic"},
		{"blocked address", 200, `{"status":"error","reason":"IP Blocked"}`,
			[]string{"blocked this server's IP address"}, "traffic"},
		{"refusal without a reason", 200, `{"status":"error"}`,
			[]string{"refused without a reason"}, "traffic"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var logins atomic.Int32
			srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			_, err := neodebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
			if err == nil {
				t.Fatal("Unlock succeeded against a refusal")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), c.unlike) {
				t.Errorf("error = %q reads like a different failure (%q)", err, c.unlike)
			}
			if got := logins.Load(); got != 1 {
				t.Errorf("logged in %d times, want no second login for a refusal about the link", got)
			}
		})
	}
}

func TestNeoDebridWrongCredentialsNameTheReasonNotThePassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"error","reason":"Wrong credentials"}`)
	}))
	defer srv.Close()

	err := neodebridAt(srv.URL).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded against a refusal")
	}
	if !strings.Contains(err.Error(), "Wrong credentials") || !strings.Contains(err.Error(), "password was refused") {
		t.Errorf("error = %q, want the explanation and the service's reason", err)
	}
	if strings.Contains(err.Error(), neodebridTestPass) || strings.Contains(err.Error(), neodebridTestEmail) {
		t.Errorf("error = %q carries the credentials", err)
	}
}

// Go's own error for a failed request quotes the URL, and /login puts the
// password into it.
func TestNeoDebridUnreachableServiceKeepsThePasswordOutOfTheError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	err := neodebridAt(base).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded against a closed server")
	}
	for _, leak := range []string{"password", "neodebrid-test", "amy"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error = %q carries the login query (%q)", err, leak)
		}
	}
}

func TestNeoDebridAuthenticateNeedsEmailAndPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went out without credentials: %s", r.URL.Path)
	}))
	defer srv.Close()

	n := NewNeoDebrid("", neodebridTestPass)
	n.base = srv.URL
	if err := n.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate succeeded without an email address")
	}
}

// timestamp arrives as a string here to show both shapes are read.
func TestNeoDebridAccountReadsPremiumExpiryAndUnlimitedTraffic(t *testing.T) {
	until := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second).UTC()
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" || r.URL.Query().Get("token") != "token-1" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","timestamp":"%d","traffic_left":"Unlimited"}`, until.Unix())
	})

	info, err := neodebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if !info.ExpiresAt.Equal(until) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, until)
	}
	if !info.Traffic.Unlimited {
		t.Error("traffic_left Unlimited did not read as unlimited")
	}
}

func TestNeoDebridAccountWithAnExpiredTimestampIsFree(t *testing.T) {
	past := time.Now().Add(-48 * time.Hour).Unix()
	var logins atomic.Int32
	srv := neodebridLogins(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"success","timestamp":%d,"traffic_left":"1 GB"}`, past)
	})

	info, err := neodebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free for an expiry in the past", info.Tier)
	}
	if info.Traffic.Unlimited || info.Traffic.LimitBytes != 0 || info.Traffic.UsedBytes != 0 {
		t.Errorf("Traffic = %+v, want it left empty for a sized traffic_left", info.Traffic)
	}
}
