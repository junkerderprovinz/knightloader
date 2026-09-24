package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Cocoleech publishes no API reference, so these bodies follow what
// JDownloader's plugin and ResolveURL's resolver parse, and the host lists
// follow the live keyless answers.

const cocoleechTestKey = "0123456789abcdef01234567"

// cocoleechServe answers GETs from routes, where "{srv}" stands for the
// server's own address, and every HEAD with 200. Only the unlock and account
// calls may carry the key.
func cocoleechServe(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		want := ""
		if r.URL.Path == "/api" || r.URL.Path == "/api/info" {
			want = cocoleechTestKey
		}
		if got := r.URL.Query().Get("key"); got != want {
			t.Errorf("%s carried key %q, want %q", r.URL.Path, got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.ReplaceAll(body, "{srv}", srv.URL))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func cocoleechAt(base string) *CocoLeech {
	c := NewCocoLeech(cocoleechTestKey)
	c.base = base
	c.readyEvery = time.Millisecond
	return c
}

func TestCocoLeechHostsExpandsAliasesAndSkipsDownHosts(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api/domains": `[
			{"name":"Rapidgator.net","domains":["rapidgator.net","rg.to"]},
			{"name":"Katfile.com","domains":["katfile.com","www.Katfile.cloud"]},
			{"name":"Deadhost.example","domains":["deadhost.example","dh.example"]}]`,
		"/api/hosts-status": `{"result":[
			{"host":"Rapidgator.net","status":"Online"},
			{"host":"Katfile.com","status":"online"},
			{"host":"Deadhost.example","status":"Offline"}]}`,
	})
	hosts, err := cocoleechAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "katfile.com", "katfile.cloud"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, gone := range []string{"deadhost.example", "dh.example"} {
		if hosts[gone] {
			t.Errorf("%q belongs to a host marked offline and was kept", gone)
		}
	}
}

// The status list only ever removes hosts, so losing it must not lose the
// domain list.
func TestCocoLeechHostsSurviveAFailedStatusCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hosts-status" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html>Bad gateway</html>")
			return
		}
		_, _ = io.WriteString(w, `[{"name":"Rapidgator.net","domains":["rapidgator.net","rg.to"]}]`)
	}))
	defer srv.Close()

	hosts, err := cocoleechAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] || !hosts["rg.to"] {
		t.Errorf("Hosts = %v, want the domain list despite the status call failing", hosts)
	}
}

func TestCocoLeechUnlockReturnsTheDownloadLink(t *testing.T) {
	const link = "https://rapidgator.net/file/abc/File.ext.html"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		q := r.URL.Query()
		if r.URL.Path != "/api" || q.Get("key") != cocoleechTestKey || q.Get("link") != link {
			t.Errorf("unlock asked %s with key %q and link %q, want /api with both", r.URL.Path, q.Get("key"), q.Get("link"))
		}
		_, _ = io.WriteString(w, `{"download":"`+srv.URL+`/dl/File.ext","chunks":"4"}`)
	}))
	defer srv.Close()

	got, err := cocoleechAt(srv.URL).Unlock(context.Background(), link)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != srv.URL+"/dl/File.ext" {
		t.Errorf("Unlock = %+v, want the download field", got)
	}
}

func TestCocoLeechUnlockSkipsTheStrayDigitBeforeTheJSON(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{"/api": `1{"download":"{srv}/dl/File.ext"}`})
	got, err := cocoleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != srv.URL+"/dl/File.ext" {
		t.Errorf("Unlock = %+v, want the link behind the stray 1", got)
	}
}

// The brace in the page's inline CSS would pass for the start of an answer.
func TestCocoLeechErrorPageKeepsItsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `<html><style>body { margin: 0 }</style>Maintenance</html>`)
	}))
	defer srv.Close()

	_, err := cocoleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Unlock = %v, want an error that names the 503", err)
	}
}

func TestCocoLeechUnlockWaitsUntilTheLinkAnswers(t *testing.T) {
	var heads atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			if heads.Add(1) < 3 {
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		_, _ = io.WriteString(w, `{"download":"`+srv.URL+`/dl/File.ext"}`)
	}))
	defer srv.Close()

	if _, err := cocoleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if n := heads.Load(); n != 3 {
		t.Errorf("the link was probed %d times, want it polled until the third probe succeeded", n)
	}
}

func TestCocoLeechUnlockHandsOnALinkThatNeverAnswers(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"download":"`+srv.URL+`/dl/File.ext"}`)
	}))
	defer srv.Close()

	c := cocoleechAt(srv.URL)
	c.readyFor = 20 * time.Millisecond
	got, err := c.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL == "" {
		t.Error("Unlock gave up on the link instead of handing it to the engine")
	}
}

func TestCocoLeechUnlockTellsRefusalsApart(t *testing.T) {
	cases := []struct {
		name   string
		status string
		said   string
		want   string
	}{
		{"dead link", `"100"`, "Link is dead.", "the hoster says this file is gone"},
		// Cocoleech does not publish the next two texts, so these are guesses
		// that the word match has to recognise.
		{"unsupported host", `100`, "This host is not supported.", "this file hoster is not supported"},
		{"daily limit", `"100"`, "Daily limit reached for this host.", "a daily limit is used up"},
		{"blocked address", `"100"`, "Your IP is blocked for today. Please contact support.", "this IP address is blocked for today"},
		{"wrong key", `"100"`, "Incorrect API key.", "the API key was refused"},
		{"expired premium", `"100"`, "Premium membership expired.", "the premium membership has run out"},
	}
	for _, c := range cases {
		body := fmt.Sprintf(`{"status":%s,"message":%q}`, c.status, c.said)
		srv := cocoleechServe(t, map[string]string{"/api": body})
		_, err := cocoleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err == nil {
			t.Errorf("%s: Unlock succeeded against a refusal", c.name)
			continue
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "cocoleech") || !strings.Contains(msg, c.said) {
			t.Errorf("%s: error = %q, want it to name the service and keep its message", c.name, msg)
		}
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to say %q", c.name, msg, c.want)
		}
	}
}

func TestCocoLeechUnlockWithoutADownloadLinkFails(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{"/api": `{"chunks":"4"}`})
	_, err := cocoleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded without a download link")
	}
	if !strings.Contains(err.Error(), "no direct link") {
		t.Errorf("error = %q, want it to say no direct link came back", err)
	}
}

// Every refusal carries status "100", so one that comes without a message is
// still a refusal, on the account calls as much as on unlock.
func TestCocoLeechStatus100WithoutAMessageIsARefusal(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api":      `{"status":"100","message":""}`,
		"/api/info": `{"status":100}`,
	})
	c := cocoleechAt(srv.URL)
	_, unlockErr := c.Unlock(context.Background(), "https://rapidgator.net/file/x")
	authErr := c.Authenticate(context.Background())
	_, accountErr := c.Account(context.Background())
	for call, err := range map[string]error{"Unlock": unlockErr, "Authenticate": authErr, "Account": accountErr} {
		if err == nil {
			t.Errorf("%s accepted a status 100 answer", call)
			continue
		}
		if !strings.Contains(err.Error(), "without a reason") {
			t.Errorf("%s: error = %q, want it to say the service gave no reason", call, err)
		}
	}
}

// Cocoleech allows 4 connections per file unless the unlock answer names
// another number, which then holds for that host.
func TestCocoLeechHostCapFollowsTheChunksField(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{"/api": `{"download":"{srv}/dl/File.ext","chunks":"2"}`})
	c := cocoleechAt(srv.URL)
	r := Resolver{ServiceID: "cocoleech", Svc: c}
	if got := r.HostCap("rapidgator.net"); got != 4 {
		t.Errorf("HostCap before any unlock = %d, want 4", got)
	}
	if _, err := c.Unlock(context.Background(), "https://www.rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got := r.HostCap("rapidgator.net"); got != 2 {
		t.Errorf("HostCap after chunks 2 = %d, want 2", got)
	}
	if got := r.HostCap("katfile.com"); got != 4 {
		t.Errorf("HostCap for a host no unlock named = %d, want 4", got)
	}
}

// The host lists answer without a key, so only /api/info can tell a wrong key.
func TestCocoLeechAuthenticateRejectsAWrongKey(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api/info": `{"status":"100","message":"Incorrect API key."}`,
	})
	err := cocoleechAt(srv.URL).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate accepted a key the service refused")
	}
	if !strings.Contains(err.Error(), "API key was refused") {
		t.Errorf("error = %q, want it to name the key", err)
	}
	if strings.Contains(err.Error(), cocoleechTestKey) {
		t.Errorf("error = %q carries the key", err)
	}
}

func TestCocoLeechAuthenticateAcceptsAWorkingKey(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api/info": `{"username":"amy","type":"premium","traffic_left":"unlimited",
			"expire_date":"2031-05-06 07:08:09","package":"1 Month Premium"}`,
	})
	if err := cocoleechAt(srv.URL).Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestCocoLeechAuthenticateWithoutAKeyAsksNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an empty key reached the service: %s", r.URL.Path)
	}))
	defer srv.Close()

	c := NewCocoLeech(" ")
	c.base = srv.URL
	if err := c.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted an empty key")
	}
}

func TestCocoLeechAccountReadsPremiumWithUnlimitedTraffic(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api/info": `{"username":"amy","type":"premium","traffic_left":"unlimited",
			"expire_date":"2031-05-06 07:08:09","package":"1 Month Premium"}`,
	})
	info, err := cocoleechAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if !info.Traffic.Unlimited {
		t.Errorf("Traffic = %+v, want Unlimited", info.Traffic)
	}
	if want := time.Date(2031, 5, 6, 7, 8, 9, 0, time.UTC); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, want)
	}
}

// type has been seen reading "free" on a paid account, so a future expiry wins.
func TestCocoLeechAccountTrustsAFutureExpiryOverTheType(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.DateTime)
	srv := cocoleechServe(t, map[string]string{
		"/api/info": `{"type":"free","traffic_left":1073741824,"expire_date":"` + future + `"}`,
	})
	info, err := cocoleechAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium for an expiry 30 days ahead", info.Tier)
	}
	// Only what is left is reported, as a number here rather than a string.
	if info.Traffic.Unlimited || info.Traffic.LimitBytes != 1<<30 || info.Traffic.UsedBytes != 0 {
		t.Errorf("Traffic = %+v, want 1 GiB left and nothing spent", info.Traffic)
	}
}

func TestCocoLeechFreeAccountHasNoTrafficAndNoExpiry(t *testing.T) {
	srv := cocoleechServe(t, map[string]string{
		"/api/info": `{"type":"free","traffic_left":"0","expire_date":"","package":"No Package"}`,
	})
	info, err := cocoleechAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free", info.Tier)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time", info.ExpiresAt)
	}
	if info.Traffic != (TrafficInfo{}) {
		t.Errorf("Traffic = %+v, want it left empty for an account that cannot download", info.Traffic)
	}
}

// net/http prints the request URL in a transport error, and the URL carries
// the key.
func TestCocoLeechTransportErrorsNeverCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	c := cocoleechAt(srv.URL)
	_, unlockErr := c.Unlock(context.Background(), "https://rapidgator.net/file/x")
	_, accountErr := c.Account(context.Background())
	for _, err := range []error{unlockErr, accountErr} {
		if err == nil {
			t.Fatal("a call succeeded against a dropped connection")
		}
		if strings.Contains(err.Error(), cocoleechTestKey) {
			t.Errorf("error = %q carries the key", err)
		}
	}
}
