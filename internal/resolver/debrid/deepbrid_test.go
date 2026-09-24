package debrid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The bodies are the ones Deepbrid's API docs print, the live /hosts answer,
// and the per-link codes JDownloader's plugin handles.

const deepbridTestKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func deepbridAt(base string) *Deepbrid {
	d := NewDeepbrid(deepbridTestKey)
	d.base = base
	return d
}

// deepbridServe answers requests to path with status and body, and checks
// that each one carries the key and asks for JSON.
func deepbridServe(t *testing.T, path string, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+deepbridTestKey {
			t.Errorf("%s carried Authorization %q, want the key as bearer token", path, got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("%s carried Accept %q, want application/json", path, got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDeepbridHostsMapsTheLiveBareNamesToDomains(t *testing.T) {
	srv := deepbridServe(t, "/hosts", http.StatusOK, `["1fichier","mega","ddownload","katfile","turbobit","somenewhoster"]`)

	hosts, err := deepbridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"1fichier.com", "megadl.fr", "mega.nz", "mega.co.nz", "ddownload.com", "ddl.to",
		"katfile.com", "katfile.cloud", "turbobit.net", "turbo.to", "trbt.cc"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	// A bare name matches no link, an unknown one gets no guessed TLD, and a
	// domain JDownloader marks dead is not claimed.
	for _, bad := range []string{"mega", "somenewhoster", "somenewhoster.com", "turbobit.pw"} {
		if hosts[bad] {
			t.Errorf("Hosts claimed %q", bad)
		}
	}
}

func TestDeepbridHostsSkipsDownEntriesAndSplitsDomainLists(t *testing.T) {
	srv := deepbridServe(t, "/hosts", http.StatusOK, `[
		{"mega.nz":"up"},
		{"turbobit.net,turbo.to":"up"},
		{"WWW.Katfile.com":"UP"},
		{"gofile":"up"},
		{"youtube.com":"down (2026-03-01)"},
		{"uploadhaven.com":"maintenance"}]`)

	hosts, err := deepbridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"mega.nz", "turbobit.net", "turbo.to", "katfile.com", "gofile.io"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, down := range []string{"youtube.com", "youtu.be", "uploadhaven.com", "www.katfile.com"} {
		if hosts[down] {
			t.Errorf("%q was claimed", down)
		}
	}
}

func TestDeepbridHostsExtendsADomainToItsHostersOtherDomains(t *testing.T) {
	srv := deepbridServe(t, "/hosts", http.StatusOK, `[{"mega.nz":"up"},{"katfile.cloud":"up"},{"somenewhoster.com":"up"}]`)

	hosts, err := deepbridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"mega.nz", "mega.co.nz", "katfile.cloud", "katfile.com", "somenewhoster.com"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
}

func TestDeepbridHostsReadsAnArraySentAsAnObject(t *testing.T) {
	srv := deepbridServe(t, "/hosts", http.StatusOK, `{"0":"mega","1":{"gofile.io":"up"},"2":{"youtube.com":"down"}}`)

	hosts, err := deepbridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["mega.nz"] || !hosts["gofile.io"] {
		t.Errorf("Hosts is missing an entry: %v", hosts)
	}
	if hosts["youtube.com"] {
		t.Error("a host marked down was claimed")
	}
}

func TestDeepbridHostsWithNothingKnownIsAnError(t *testing.T) {
	srv := deepbridServe(t, "/hosts", http.StatusOK, `["somenewhoster"]`)

	if _, err := deepbridAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatal("Hosts succeeded without a single usable domain")
	}
}

func TestDeepbridUnlockReturnsTheGeneratedLink(t *testing.T) {
	const link = "https://mega.nz/file/abc123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/generate/link" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want a form", got)
		}
		if got := r.PostFormValue("link"); got != link {
			t.Errorf("link = %q, want the hoster URL", got)
		}
		// The key belongs in the header only.
		if r.URL.RawQuery != "" || r.PostForm.Has("apikey") {
			t.Errorf("the request carried more than the link: query %q, form %v", r.URL.RawQuery, r.PostForm)
		}
		_, _ = io.WriteString(w, `{"error":0,"message":"OK","original_link":"https://mega.nz/file/abc123",
			"hoster":"mega","hoster-icon":"mega.png","filename":"my_file.zip",
			"link":"https://premium-dl.deepbrid.com/d/xyz","stream":"hash_for_streaming","size":"1.50 GB"}`)
	}))
	defer srv.Close()

	got, err := deepbridAt(srv.URL).Unlock(context.Background(), link)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://premium-dl.deepbrid.com/d/xyz" || got.Name != "my_file.zip" {
		t.Errorf("Unlock = %+v, want the generated link and its file name", got)
	}
	if got.Size != 1610612736 {
		t.Errorf("Size = %d, want 1.50 GB in 1024 steps", got.Size)
	}
}

func TestDeepbridUnlockErrorsSayWhatWentWrong(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		want       []string
		not        []string
	}{
		{
			name: "unsupported host", status: http.StatusOK,
			body: `{"error":3,"message":"Link not supported"}`,
			want: []string{"does not support this host", "Link not supported"},
		},
		{
			name: "hoster quota", status: http.StatusOK,
			body: `{"error":9,"message":"Daily limit reached for this hoster"}`,
			want: []string{"daily limit for this hoster is used up", "Daily limit reached"},
		},
		{
			// Deepbrid documents no code for a dead link, so a code this client
			// does not know has to pass the message on unchanged.
			name: "dead link", status: http.StatusOK,
			body: `{"error":99,"message":"File not found"}`,
			want: []string{"deepbrid /generate/link: File not found"},
			not:  []string{"does not support", "limit"},
		},
		{
			name: "wait time", status: http.StatusOK,
			body: `{"error":8,"message":"You have already downloaded, wait <b> 14:11 minutes<\/b> to download again. <a href=\"..\/signup\" target=\"_blank\">Upgrade to premium<\/a>"}`,
			want: []string{"has to wait", "wait 14:11 minutes to download again. Upgrade to premium"},
			not:  []string{"<b>", "href"},
		},
		{
			name: "proxy detected", status: http.StatusOK,
			body: `{"error":15,"message":"Proxy, VPN or VPS detected."}`,
			want: []string{"proxy, VPN or VPS", "Proxy, VPN or VPS detected."},
		},
		{
			name: "free account", status: http.StatusForbidden,
			body: `{"error":403,"message":"Premium account required"}`,
			want: []string{"account was refused", "Premium account required"},
		},
		{
			name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "12",
			body: `{"error":429,"message":"Rate limit exceeded."}`,
			want: []string{"too many requests, retry in 12s"},
		},
		{
			name: "maintenance", status: http.StatusServiceUnavailable,
			body: `{"error":503,"message":"Service under maintenance"}`,
			want: []string{"under maintenance"},
		},
		{
			name: "no link", status: http.StatusOK,
			body: `{"error":0,"message":"OK","filename":"my_file.zip","size":"1.50 GB"}`,
			want: []string{"no direct link returned"},
		},
	}
	seen := map[string]string{}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.retryAfter != "" {
				w.Header().Set("Retry-After", c.retryAfter)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}))
		_, err := deepbridAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/abc123")
		srv.Close()
		if err == nil {
			t.Errorf("%s: Unlock succeeded", c.name)
			continue
		}
		msg := err.Error()
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: error = %q, want it to contain %q", c.name, msg, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(msg, n) {
				t.Errorf("%s: error = %q, should not contain %q", c.name, msg, n)
			}
		}
		if strings.Contains(msg, deepbridTestKey) {
			t.Errorf("%s: the API key leaked into the error", c.name)
		}
		if other, dup := seen[msg]; dup {
			t.Errorf("%s reads the same as %s: %q", c.name, other, msg)
		}
		seen[msg] = c.name
	}
}

func TestDeepbridReadsAnErrorCodeSentAsAString(t *testing.T) {
	srv := deepbridServe(t, "/generate/link", http.StatusOK, `{"error":"3","message":"Link not supported"}`)

	_, err := deepbridAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/abc123")
	if err == nil || !strings.Contains(err.Error(), "does not support this host") {
		t.Fatalf("Unlock = %v, want the unsupported-host error", err)
	}
}

func TestDeepbridErrorThatIsNotANumberStillRefuses(t *testing.T) {
	for _, body := range []string{
		`{"error":true,"message":"Invalid API key"}`,
		`{"error":"Invalid API key","message":"Invalid API key"}`,
	} {
		srv := deepbridServe(t, "/user", http.StatusOK, body)
		err := deepbridAt(srv.URL).Authenticate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
			t.Errorf("Authenticate against %s = %v, want the refusal", body, err)
		}
	}
}

func TestDeepbridAuthenticateAcceptsAWorkingKey(t *testing.T) {
	srv := deepbridServe(t, "/user", http.StatusOK, `{"username":"john","email":"user@example.com",
		"type":"premium","fidelity_points":0,"expiration":"2026-06-15","maxDownloads":5,"maxConnections":1}`)

	if err := deepbridAt(srv.URL).Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestDeepbridAuthenticateReportsARefusedKey(t *testing.T) {
	srv := deepbridServe(t, "/user", http.StatusUnauthorized, `{"error":401,"message":"Authentication required."}`)

	err := deepbridAt(srv.URL).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded against a 401")
	}
	if !strings.Contains(err.Error(), "API key was refused") || !strings.Contains(err.Error(), "Authentication required.") {
		t.Errorf("error = %q, want the refusal and the service's message", err)
	}
	if strings.Contains(err.Error(), deepbridTestKey) {
		t.Error("the API key leaked into the error")
	}
}

// Deepbrid sends a request it cannot authenticate to the login page when it
// does not take it for an API call.
func TestDeepbridLoginRedirectCountsAsARefusedKey(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			t.Error("the redirect to the login page was followed")
			return
		}
		http.Redirect(w, r, srv.URL+"/login", http.StatusFound)
	}))
	defer srv.Close()

	err := deepbridAt(srv.URL).Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "API key was refused") {
		t.Fatalf("Authenticate = %v, want the key reported as refused", err)
	}
}

// A 200 that is not JSON, such as a proxy's or a CDN's page, must not pass as
// a verified key.
func TestDeepbridAuthenticateRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>Sign in</body></html>")
	}))
	defer srv.Close()

	if err := deepbridAt(srv.URL).Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted an HTML page")
	}
}

func TestDeepbridAuthenticateWithoutAKeyAsksForOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went out without a key: %s", r.URL.Path)
	}))
	defer srv.Close()
	d := NewDeepbrid("")
	d.base = srv.URL

	if err := d.Authenticate(context.Background()); err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("Authenticate = %v, want it to ask for a key", err)
	}
}

func TestDeepbridAccountReadsPremiumUntilTheEndOfItsLastDay(t *testing.T) {
	srv := deepbridServe(t, "/user", http.StatusOK, `{"username":"john","email":"user@example.com",
		"type":"premium","fidelity_points":0,"expiration":"2026-06-15","maxDownloads":5,"maxConnections":1}`)

	info, err := deepbridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if want := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, want)
	}
	if !info.Traffic.Unlimited {
		t.Error("a premium account should read Unlimited")
	}
}

func TestDeepbridFreeAccountHasNoExpiryAndNoTraffic(t *testing.T) {
	srv := deepbridServe(t, "/user", http.StatusOK, `{"username":"john","type":"free","expiration":"2024-01-01","maxDownloads":1}`)

	info, err := deepbridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free", info.Tier)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time", info.ExpiresAt)
	}
	if info.Traffic.Unlimited {
		t.Error("a free account cannot unlock and should not read Unlimited")
	}
}

func TestDeepbridSizeReadsNumbersAndRoundedFigures(t *testing.T) {
	cases := map[string]int64{
		`125002`:     125002,
		`"125002"`:   125002,
		`"1.50 GB"`:  1610612736,
		`"700 MB"`:   734003200,
		`"2KB"`:      2048,
		`"512 B"`:    512,
		`"1 TiB"`:    1 << 40,
		`"unknown"`:  0,
		`null`:       0,
		`"12 parts"`: 0,
	}
	for raw, want := range cases {
		if got := deepbridSize(json.RawMessage(raw)); got != want {
			t.Errorf("deepbridSize(%s) = %d, want %d", raw, got, want)
		}
	}
}
