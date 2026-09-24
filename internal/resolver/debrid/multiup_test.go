package debrid

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests answer with the bodies the MultiUp API page documents and the
// host list the live service returns, so none of them needs an account.

const (
	multiupTestUser = "amy.pond"
	multiupTestPass = "fish-custard-7"
	multiupPremium  = `{"error":"success","login":"amy.pond","user":4711,"account_type":"premium","premium_days_left":30}`
	multiupUnlocked = `{"error":"success","debrid_link":"https://debrid.multiup.io/dl/abc/File.ext"}`
)

func multiupAt(base string) *MultiUp {
	m := NewMultiUp(multiupTestUser, multiupTestPass)
	m.base = base
	return m
}

// multiupServer answers /login with login, hands /generate-debrid-link to
// unlock and counts the logins.
func multiupServer(t *testing.T, login string, unlock http.HandlerFunc) (*MultiUp, *atomic.Int32) {
	t.Helper()
	logins := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			if r.Method != http.MethodPost || r.URL.RawQuery != "" {
				t.Errorf("login went out as %s with query %q, want a POST with the credentials in the body", r.Method, r.URL.RawQuery)
			}
			if r.PostFormValue("username") != multiupTestUser || r.PostFormValue("password") != multiupTestPass {
				t.Error("login did not carry the configured username and password in its body")
			}
			_, _ = io.WriteString(w, login)
		case "/generate-debrid-link":
			if unlock == nil {
				t.Error("an unlock went out in a test that expects none")
				return
			}
			unlock(w, r)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return multiupAt(srv.URL), logins
}

func multiupSays(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

func multiupHostServer(t *testing.T, status int, body string) *MultiUp {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get-list-hosts-debrid" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return multiupAt(srv.URL)
}

// The body is the live answer, where 1fichier's alias domains are entries of
// their own, plus a www. prefix, capitals and a bare name.
func TestMultiUpHostsTakesEveryListedDomainWithItsAliases(t *testing.T) {
	m := multiupHostServer(t, http.StatusOK, `{"error":"success","hosts":["1fichier.com","alterupload.com",
		"cjoint.net","desfichiers.com","dfichiers.com","dl4free.com","megadl.fr","mesfichiers.org",
		"piecejointe.net","pjointe.com","rapidgator.net","rg.to","tenvoi.com","www.Example-Host.COM","1fichier",""]}`)

	hosts, err := m.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"1fichier.com", "alterupload.com", "megadl.fr", "tenvoi.com", "rapidgator.net", "rg.to", "example-host.com"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	if hosts["1fichier"] {
		t.Error("a bare name without a dot was taken as a domain")
	}
	if len(hosts) != 14 {
		t.Errorf("Hosts has %d entries, want the 14 domains: %v", len(hosts), hosts)
	}
}

func TestMultiUpHostsReadsAMapKeyedByDomain(t *testing.T) {
	m := multiupHostServer(t, http.StatusOK, `{"error":"success","hosts":{"rapidgator.net":{},"rg.to":{}}}`)

	hosts, err := m.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] || !hosts["rg.to"] || len(hosts) != 2 {
		t.Errorf("Hosts = %v, want the two keys", hosts)
	}
}

func TestMultiUpHostsPassesOnTheServiceMessage(t *testing.T) {
	m := multiupHostServer(t, http.StatusOK, `{"error":"Maintenance in progress"}`)

	_, err := m.Hosts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Maintenance in progress") {
		t.Fatalf("Hosts error = %v, want the service's own message", err)
	}
}

func TestMultiUpHostsWithAnEmptyListIsAnError(t *testing.T) {
	m := multiupHostServer(t, http.StatusOK, `{"error":"success","hosts":[]}`)

	if _, err := m.Hosts(context.Background()); err == nil {
		t.Fatal("Hosts succeeded with no hosts, which would empty the routing table")
	}
}

// A challenge page in front of the API is HTML, not JSON.
func TestMultiUpUnreadableAnswerNamesTheStatusLine(t *testing.T) {
	m := multiupHostServer(t, http.StatusForbidden, `<html><title>Just a moment...</title></html>`)

	_, err := m.Hosts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unreadable") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Hosts error = %v, want an unreadable answer naming HTTP 403", err)
	}
}

func TestMultiUpUnlockLogsInAndReturnsTheDebridLink(t *testing.T) {
	m, logins := multiupServer(t, multiupPremium, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.PostFormValue("link") != "https://rapidgator.net/file/x" {
			t.Errorf("unlock went out as %s with link %q, want a POST with the hoster link", r.Method, r.PostFormValue("link"))
		}
		_, _ = io.WriteString(w, multiupUnlocked)
	})

	got, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://debrid.multiup.io/dl/abc/File.ext" {
		t.Errorf("Unlock = %+v, want the debrid_link", got)
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times, want once before the unlock", n)
	}
}

func TestMultiUpUnlockReusesARecentLogin(t *testing.T) {
	m, logins := multiupServer(t, multiupPremium, multiupSays(multiupUnlocked))

	for range 3 {
		if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
			t.Fatalf("Unlock: %v", err)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times for three unlocks, want once", n)
	}
}

func TestMultiUpUnlockLogsInAgainOnceTheLoginIsOld(t *testing.T) {
	m, logins := multiupServer(t, multiupPremium, multiupSays(multiupUnlocked))

	if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	m.loggedIn = time.Now().Add(-multiupLoginTTL - time.Second)
	if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logged in %d times, want a second login once the first was old", n)
	}
}

func TestMultiUpParallelUnlocksShareOneLogin(t *testing.T) {
	m, logins := multiupServer(t, multiupPremium, multiupSays(multiupUnlocked))

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
				t.Errorf("Unlock: %v", err)
			}
		})
	}
	wg.Wait()
	if n := logins.Load(); n != 1 {
		t.Errorf("eight parallel unlocks logged in %d times, want once", n)
	}
}

func TestMultiUpUnlockLogsInAgainWhenTheLoginLapsed(t *testing.T) {
	var unlocks atomic.Int32
	m, logins := multiupServer(t, multiupPremium, func(w http.ResponseWriter, _ *http.Request) {
		if unlocks.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"error":"Login failed","status":"401"}`)
			return
		}
		_, _ = io.WriteString(w, multiupUnlocked)
	})

	got, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL == "" {
		t.Error("Unlock returned no URL after logging in again")
	}
	if l, u := logins.Load(), unlocks.Load(); l != 2 || u != 2 {
		t.Errorf("logins = %d, unlocks = %d, want one fresh login and one retry", l, u)
	}
}

func TestMultiUpUnlockTriesOnlyOneFreshLogin(t *testing.T) {
	var unlocks atomic.Int32
	m, logins := multiupServer(t, multiupPremium, func(w http.ResponseWriter, _ *http.Request) {
		unlocks.Add(1)
		_, _ = io.WriteString(w, `{"error":"Login failed","status":401}`)
	})

	_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	var r *multiupRefusal
	if !errors.As(err, &r) || r.kind != multiupBadLogin {
		t.Fatalf("Unlock error = %v, want a refused login", err)
	}
	if l, u := logins.Load(), unlocks.Load(); l != 2 || u != 2 {
		t.Errorf("logins = %d, unlocks = %d, want exactly one retry", l, u)
	}
}

// The unlock call takes a protected file's password, so a refusal about one
// says nothing about the account.
func TestMultiUpFilePasswordRefusalIsNotALoginProblem(t *testing.T) {
	var unlocks atomic.Int32
	m, logins := multiupServer(t, multiupPremium, func(w http.ResponseWriter, _ *http.Request) {
		unlocks.Add(1)
		_, _ = io.WriteString(w, `{"error":"Wrong password for this file"}`)
	})

	_, err := m.Unlock(context.Background(), "https://1fichier.com/?abc123")
	var r *multiupRefusal
	if !errors.As(err, &r) || r.kind == multiupBadLogin {
		t.Fatalf("Unlock error = %v, want a refusal of the link, not of the login", err)
	}
	if !strings.Contains(err.Error(), "Wrong password for this file") || strings.Contains(err.Error(), "login") {
		t.Errorf("error = %q, want the service's message without blaming the login", err)
	}
	if l, u := logins.Load(), unlocks.Load(); l != 1 || u != 1 {
		t.Errorf("logins = %d, unlocks = %d, want no fresh login and no retry", l, u)
	}
}

// MultiUp allows three connections per address in total, fewer than one
// download opens by default.
func TestMultiUpHoldsEachDownloadToOneConnection(t *testing.T) {
	res := Resolver{ServiceID: "multiup", Svc: NewMultiUp(multiupTestUser, multiupTestPass)}
	for _, host := range []string{"rapidgator.net", "1fichier.com", "rg.to"} {
		if got := res.HostCap(host); got != 1 {
			t.Errorf("HostCap(%q) = %d, want 1", host, got)
		}
	}
}

func TestMultiUpWrongPasswordStopsBeforeTheUnlock(t *testing.T) {
	m, logins := multiupServer(t, `{"error":"Invalid username or password"}`, nil)

	_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded with a refused login")
	}
	if !strings.Contains(err.Error(), "login was refused") || !strings.Contains(err.Error(), "Invalid username or password") {
		t.Errorf("error = %q, want a refused login with the service's message", err)
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times, want no second try with a password already refused", n)
	}
}

// "User not found" would read as a missing file anywhere else.
func TestMultiUpAuthenticateRefusalCarriesTheMessageButNeverTheCredentials(t *testing.T) {
	m, _ := multiupServer(t, `{"error":"User not found"}`, nil)

	err := m.Authenticate(context.Background())
	var r *multiupRefusal
	if !errors.As(err, &r) || r.kind != multiupBadLogin {
		t.Fatalf("Authenticate error = %v, want a refused login", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "User not found") {
		t.Errorf("error = %q, want the service's own message", msg)
	}
	if strings.Contains(msg, multiupTestUser) || strings.Contains(msg, multiupTestPass) {
		t.Errorf("error = %q carries the credentials", msg)
	}
}

func TestMultiUpAuthenticateWithoutAPasswordSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went out without a password: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	m := NewMultiUp(multiupTestUser, "")
	m.base = srv.URL

	if err := m.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate succeeded without a password")
	}
}

func TestMultiUpUnlockRefusalsReadByKind(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		kind    multiupKind
		says    string
		carries string
	}{
		{"dead link", http.StatusOK, `{"error":"File not found"}`,
			multiupFileGone, "file is gone", "File not found"},
		{"unsupported host", http.StatusOK, `{"error":"Host not supported"}`,
			multiupHostUnsupported, "not supported or is down", "Host not supported"},
		{"hoster offline in the older status", http.StatusOK, `{"error":"hoster offline","status":"404"}`,
			multiupHostUnsupported, "not supported or is down", "hoster offline"},
		{"quota", http.StatusOK, `{"error":"Daily download limit reached"}`,
			multiupLimitReached, "download limit", "Daily download limit reached"},
		{"payment required", http.StatusPaymentRequired, `<html>Payment Required</html>`,
			multiupLimitReached, "download limit", "402"},
		{"service down", http.StatusOK, `{"error":"Service unavailable","status":503}`,
			multiupUnavailable, "service is unavailable", "Service unavailable"},
		{"anything else", http.StatusOK, `{"error":"Something unexpected"}`,
			multiupOther, "/generate-debrid-link", "Something unexpected"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := multiupServer(t, multiupPremium, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})

			_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
			var r *multiupRefusal
			if !errors.As(err, &r) {
				t.Fatalf("Unlock error = %v, want a refusal", err)
			}
			if r.kind != c.kind {
				t.Errorf("kind = %d, want %d", r.kind, c.kind)
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "multiup") || !strings.Contains(msg, c.says) || !strings.Contains(msg, c.carries) {
				t.Errorf("error = %q, want it to name the service, say %q and carry %q", msg, c.says, c.carries)
			}
			seen[c.name] = strings.TrimSuffix(msg, r.msg)
		})
	}
	if seen["dead link"] == seen["unsupported host"] || seen["dead link"] == seen["quota"] || seen["unsupported host"] == seen["quota"] {
		t.Errorf("a dead link, an unsupported host and a quota hit read alike: %q", seen)
	}
}

func TestMultiUpUnlockWithoutADebridLinkIsAnError(t *testing.T) {
	m, _ := multiupServer(t, multiupPremium, multiupSays(`{"error":"success"}`))

	_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil || !strings.Contains(err.Error(), "no direct link") {
		t.Fatalf("Unlock error = %v, want no direct link", err)
	}
}

// premium_days_left is documented as an integer; a string is read as well.
func TestMultiUpAccountReadsPremiumAndDaysLeft(t *testing.T) {
	m, _ := multiupServer(t, `{"error":"success","login":"amy.pond","user":"4711","account_type":"Premium","premium_days_left":"30"}`, nil)

	info, err := m.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if !info.Traffic.Unlimited {
		t.Error("a premium account was not read as unlimited")
	}
	if d := time.Until(info.ExpiresAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("ExpiresAt is %v away, want about 30 days", d)
	}
}

func TestMultiUpFreeAccountHasNoExpiryAndNoUnlimitedTraffic(t *testing.T) {
	m, _ := multiupServer(t, `{"error":"success","login":"amy.pond","user":4711,"account_type":"free","premium_days_left":0}`, nil)

	info, err := m.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free", info.Tier)
	}
	if info.Traffic.Unlimited || !info.ExpiresAt.IsZero() {
		t.Errorf("Account = %+v, want no unlimited traffic and no expiry", info)
	}
}

func TestMultiUpAccountLoginCountsForTheNextUnlock(t *testing.T) {
	m, logins := multiupServer(t, multiupPremium, multiupSays(multiupUnlocked))

	if _, err := m.Account(context.Background()); err != nil {
		t.Fatalf("Account: %v", err)
	}
	if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times, want the account check's login reused", n)
	}
}
