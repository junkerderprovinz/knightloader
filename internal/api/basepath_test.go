package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// servedUnder sets KL_BASE_PATH's value for one test. Tests calling it stay
// serial, since buildinfo is shared by the whole package.
func servedUnder(t *testing.T, base string) {
	t.Helper()
	prev := buildinfo.BasePath
	buildinfo.BasePath = base
	t.Cleanup(func() { buildinfo.BasePath = prev })
}

// fetch sends one request without following redirects and returns the
// response with its body read.
func fetch(t *testing.T, method, url string, header map[string]string, body []byte) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func baseElement(path string) string {
	return `<base href="` + path + `" />`
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}

func TestParseBasePath(t *testing.T) {
	t.Parallel()
	good := map[string]string{
		"":          "",
		"/":         "",
		" /kl/ ":    "/kl",
		"/kl":       "/kl",
		"/apps/kl":  "/apps/kl",
		"/apis":     "/apis",
		"/relays":   "/relays",
		"/k.l_~-1/": "/k.l_~-1",
	}
	for in, want := range good {
		got, err := ParseBasePath(in)
		if err != nil || got != want {
			t.Errorf("ParseBasePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"kl",
		"//evil.example",
		"https://evil.example/kl",
		"/kl//x",
		"/../kl",
		"/kl/.",
		"/k l",
		"/kl?x=1",
		"/%6Bl",
		`/kl"><script>`,
		"/api",
		"/api/kl",
		"/relay",
		"/relay/kl",
	} {
		if got, err := ParseBasePath(in); err == nil {
			t.Errorf("ParseBasePath(%q) = %q, want it refused", in, got)
		}
	}
}

func TestTheInterfaceAndTheAPIAnswerUnderTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	for _, path := range []string{"/kl", "/kl/", "/kl/settings/network", "/settings/network"} {
		resp, body := fetch(t, http.MethodGet, srv.URL+path, nil, nil)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, baseElement("/kl/")) {
			t.Errorf("GET %s = %d without the page's base element for /kl/:\n%.300s", path, resp.StatusCode, body)
		}
	}

	resp, _ := fetch(t, http.MethodGet, srv.URL+"/kl/manifest.webmanifest", nil, nil)
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != http.StatusOK || ct != "application/manifest+json" {
		t.Errorf("GET /kl/manifest.webmanifest = %d %q, want the manifest", resp.StatusCode, ct)
	}

	// The container's health check and the LAN address still reach the
	// routes without the prefix.
	for _, path := range []string{"/kl/api/health", "/api/health"} {
		resp, body := fetch(t, http.MethodGet, srv.URL+path, nil, nil)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status"`) {
			t.Errorf("GET %s = %d %q, want the health answer", path, resp.StatusCode, body)
		}
	}

	resp, _ = fetch(t, http.MethodGet, srv.URL+"/kl/api/no-such-route", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown route under the prefix answered %d, want 404 rather than the page", resp.StatusCode)
	}
}

func TestAWebSocketOpensUnderTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	c := dialWS(t, srv.URL+"/kl")
	readOneOfType(t, c, "snapshot")
}

func TestARelayServedUnderTheBasePathIsReachedAtTheInstanceAddress(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	address := srv.URL + "/kl"
	const key = "a-relay-served-under-a-base-path"
	if code, put := putRelayConfig(t, address, `{"relayUrl":"`+address+`","key":"`+key+`","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /kl/api/relay/config = %d %+v, want serve=true", code, put)
	}
	fixedSibling(t, address, key, "sibling-1")

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, cfg := getRelayConfig(t, address)
		if cfg.ServeClients == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("config = %+v, want this instance and the sibling both connected to the relay at %s", cfg, address)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestARedirectKeepsTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	resp, _ := fetch(t, http.MethodGet, srv.URL+"/kl//downloads?view=list", nil, nil)
	if resp.StatusCode/100 != 3 {
		t.Fatalf("GET /kl//downloads = %d, want the mux's redirect to the tidy path", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/kl/downloads?view=list" {
		t.Errorf("Location = %q, want /kl/downloads?view=list", loc)
	}
}

func TestTheSessionCookieStaysUnderTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"password": "a-good-password"})
	resp, _ := fetch(t, http.MethodPost, srv.URL+"/kl/api/auth/login", map[string]string{"Content-Type": "application/json"}, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login under /kl answered %d", resp.StatusCode)
	}
	session := sessionCookie(resp)
	if session == nil || session.Path != "/kl" {
		t.Fatalf("session cookie = %+v, want Path /kl so requests to other apps on the host do not carry it", session)
	}

	resp, _ = fetch(t, http.MethodGet, srv.URL+"/kl/api/tasks", map[string]string{"Cookie": session.Name + "=" + session.Value}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /kl/api/tasks with the session = %d, want 200", resp.StatusCode)
	}
	resp, _ = fetch(t, http.MethodGet, srv.URL+"/kl/api/tasks", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /kl/api/tasks without a session = %d, want 401", resp.StatusCode)
	}
}

func TestLoggingOutUnderTheBasePathEndsASessionFromTheRoot(t *testing.T) {
	servedUnder(t, "")
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: newJar(t)}
	if code := login(t, browser, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login at the root answered %d", code)
	}

	// The admin sets KL_BASE_PATH, and the cookie from the root still reaches
	// every path under it.
	servedUnder(t, "/kl")
	tasks := func() int {
		resp, err := browser.Get(srv.URL + "/kl/api/tasks")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := tasks(); code != http.StatusOK {
		t.Fatalf("GET /kl/api/tasks with the root session = %d, want 200", code)
	}
	resp, err := browser.Post(srv.URL+"/kl/api/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if code := tasks(); code != http.StatusUnauthorized {
		t.Errorf("GET /kl/api/tasks after logging out = %d, want 401", code)
	}
}

func TestTheRootServesAsItAlwaysHas(t *testing.T) {
	servedUnder(t, "")
	srv, a := testServer(t)
	defer srv.Close()

	resp, body := fetch(t, http.MethodGet, srv.URL+"/", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, baseElement("/")) {
		t.Errorf("GET / = %d without the root's base element:\n%.300s", resp.StatusCode, body)
	}
	// Without a prefix, /kl is one more client-side route.
	resp, body = fetch(t, http.MethodGet, srv.URL+"/kl/api/health", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, baseElement("/")) {
		t.Errorf("GET /kl/api/health at the root = %d, want the page as for any other route", resp.StatusCode)
	}

	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"password": "a-good-password"})
	resp, _ = fetch(t, http.MethodPost, srv.URL+"/api/auth/login", map[string]string{"Content-Type": "application/json"}, payload)
	if session := sessionCookie(resp); session == nil || session.Path != "/" {
		t.Errorf("session cookie = %+v, want Path /", session)
	}
}

func TestAProxyPrefixIsHonouredWhenNoneIsConfigured(t *testing.T) {
	servedUnder(t, "")
	srv, a := testServer(t)
	defer srv.Close()

	proxied := map[string]string{"X-Forwarded-Prefix": "/kl"}
	resp, body := fetch(t, http.MethodGet, srv.URL+"/downloads", proxied, nil)
	if !strings.Contains(body, baseElement("/kl/")) {
		t.Errorf("a stripped request with X-Forwarded-Prefix /kl got a page without its base element:\n%.300s", body)
	}
	if !strings.Contains(resp.Header.Get("Vary"), "X-Forwarded-Prefix") {
		t.Errorf("Vary = %q, want X-Forwarded-Prefix so a cache keeps the pages apart", resp.Header.Get("Vary"))
	}

	// A proxy that sends the header and leaves the prefix on the path.
	resp, _ = fetch(t, http.MethodGet, srv.URL+"/kl/api/health", proxied, nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("GET /kl/api/health with the header = %d %q, want the health answer", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	for _, forged := range []string{"//evil.example", "https://evil.example", "/api", `/x"><script>`} {
		_, body := fetch(t, http.MethodGet, srv.URL+"/", map[string]string{"X-Forwarded-Prefix": forged}, nil)
		if !strings.Contains(body, baseElement("/")) {
			t.Errorf("X-Forwarded-Prefix %q changed the page's base:\n%.300s", forged, body)
		}
	}

	// Taking the prefix off must not take a route out from under the guard.
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	resp, _ = fetch(t, http.MethodGet, srv.URL+"/x/api/tasks", map[string]string{"X-Forwarded-Prefix": "/x"}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /x/api/tasks with X-Forwarded-Prefix /x and no session = %d, want 401", resp.StatusCode)
	}
}

func TestAConfiguredBasePathOutranksTheProxyHeader(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	resp, body := fetch(t, http.MethodGet, srv.URL+"/kl/", map[string]string{"X-Forwarded-Prefix": "/other"}, nil)
	if !strings.Contains(body, baseElement("/kl/")) {
		t.Errorf("the header replaced KL_BASE_PATH:\n%.300s", body)
	}
	if strings.Contains(resp.Header.Get("Vary"), "X-Forwarded-Prefix") {
		t.Error("the page varies on a header it ignores")
	}

	var view selfTestRequestView
	resp, body = fetch(t, http.MethodGet, srv.URL+"/kl/api/selftest/request", map[string]string{"X-Forwarded-Prefix": "/other"}, nil)
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("self-test echo = %d %q: %v", resp.StatusCode, body, err)
	}
	if view.BasePath != "/kl" || view.ForwardedPrefix != "/other" || view.Path != "/api/selftest/request" {
		t.Errorf("echo = base %q, forwarded %q, path %q; want /kl, /other and the path without the prefix",
			view.BasePath, view.ForwardedPrefix, view.Path)
	}
}

func TestRemoteAccessAddressesCarryTheBasePath(t *testing.T) {
	requireContainerDeployment(t)
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/kl/api/remote-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	var info RemoteAccessInfo
	if code := doJSON(t, req, &info); code != http.StatusOK {
		t.Fatalf("GET /kl/api/remote-access answered %d", code)
	}
	for _, addr := range info.Addresses {
		if !strings.HasSuffix(addr.URL, "/kl") {
			t.Errorf("address %q drops the base path", addr.URL)
		}
	}
	if len(info.Addresses) == 0 || info.Addresses[0].URL != "https://example.com/kl" {
		t.Errorf("addresses = %+v, want https://example.com/kl first", info.Addresses)
	}
}

func TestTheModuleLinesNameAddressesUnderTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.DownloadClientAPI, s.SubfolderByPackage, s.Metrics = true, true, true
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.APITokens.Create("sonarr"); err != nil {
		t.Fatal(err)
	}

	resp, body := fetch(t, http.MethodGet, srv.URL+"/kl/api/features", nil, nil)
	var state FeatureState
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("GET /kl/api/features = %d %q: %v", resp.StatusCode, body, err)
	}
	want := map[string]map[string]string{
		"downloadclient": {
			"sabnzbd": "/kl/api/sabnzbd/api", "sabnzbdBase": "kl/api/sabnzbd",
			"qbittorrent": "/kl/api/qbittorrent", "qbittorrentBase": "kl/api/qbittorrent",
		},
		"metrics": {"path": "/kl/api/metrics"},
	}
	for _, f := range state.Modules {
		if w, ok := want[f.ID]; ok {
			for k, v := range w {
				if f.DetailArgs[k] != v {
					t.Errorf("%s line: %s = %q, want %q (%s)", f.ID, k, f.DetailArgs[k], v, f.Detail)
				}
			}
			delete(want, f.ID)
		}
	}
	for id := range want {
		t.Errorf("no %s row in /api/features", id)
	}
}

func TestMovingUnderABasePathReplacesTheDomainKnownAtTheRoot(t *testing.T) {
	requireContainerDeployment(t)
	servedUnder(t, "/kl")
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.KnownDomains = []string{"https://example.com", "https://other.example.org"}
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/kl/api/remote-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	if code := doJSON(t, req, nil); code != http.StatusOK {
		t.Fatalf("GET /kl/api/remote-access answered %d", code)
	}
	want := []string{"https://example.com/kl", "https://other.example.org"}
	if got := a.Settings.Get().KnownDomains; !slices.Equal(got, want) {
		t.Errorf("KnownDomains = %q, want %q", got, want)
	}

	// Seen from the LAN, the QR code carries the domain with its path.
	_, info := getRemoteAccess(t, srv.URL, "192.0.2.10:8749")
	if addr, _ := preferredAddress(info.Addresses); addr != "https://example.com/kl" {
		t.Errorf("preferred address = %q among %+v, want https://example.com/kl", addr, info.Addresses)
	}
}

func TestAKnownDomainKeepsItsPath(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/remote-access", nil)
	r.Host = ""
	addrs := remoteAddresses(r, []string{"https://example.com/kl/"})
	if len(addrs) == 0 || addrs[0].URL != "https://example.com/kl" {
		t.Errorf("addresses = %+v, want https://example.com/kl", addrs)
	}
}
