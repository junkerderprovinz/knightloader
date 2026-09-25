package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// requireContainerDeployment sets buildinfo.Deployment to "container" for the
// test and restores it afterwards, whatever state another test left the
// shared global in.
func requireContainerDeployment(t *testing.T) {
	t.Helper()
	prev := buildinfo.Deployment
	buildinfo.Deployment = "container"
	t.Cleanup(func() { buildinfo.Deployment = prev })
}

// getRemoteAccess requests /api/remote-access, optionally with a Host other
// than the loopback address httptest listens on.
func getRemoteAccess(t *testing.T, url, hostOverride string) (int, RemoteAccessInfo) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/api/remote-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	if hostOverride != "" {
		req.Host = hostOverride
	}
	var out RemoteAccessInfo
	code := doJSON(t, req, &out)
	return code, out
}

// doJSON is getJSON for a request already built.
func doJSON(t *testing.T, req *http.Request, into any) int {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("%s answered unparseable JSON: %v", req.URL, err)
		}
	}
	return resp.StatusCode
}

// TestRemoteAccessListsTheConnectionThatAskedFirst checks that the address the
// request arrived on, the only one that is proven, comes first.
func TestRemoteAccessListsTheConnectionThatAskedFirst(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	code, info := getRemoteAccess(t, srv.URL, "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/remote-access answered %d", code)
	}
	if len(info.Addresses) == 0 {
		t.Fatal("no addresses reported at all")
	}
	first := info.Addresses[0]
	if first.Label != "this connection" {
		t.Errorf("first address label = %q, want %q", first.Label, "this connection")
	}
	if !first.Loopback {
		t.Error("a request against httptest's own loopback server was not reported as loopback")
	}
	if !strings.Contains(first.URL, strings.TrimPrefix(srv.URL, "http://")) {
		t.Errorf("first address = %q, does not name the server actually answering (%s)", first.URL, srv.URL)
	}
}

// TestRemoteAccessTrustsForwardedProtoForScheme covers a reverse proxy that
// terminates TLS and talks plain HTTP to the container.
func TestRemoteAccessTrustsForwardedProtoForScheme(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/remote-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "knightloader.example.tld"
	req.Header.Set("X-Forwarded-Proto", "https")

	var info RemoteAccessInfo
	code := doJSON(t, req, &info)
	if code != http.StatusOK {
		t.Fatalf("GET /api/remote-access answered %d", code)
	}
	if len(info.Addresses) == 0 {
		t.Fatal("no addresses reported at all")
	}
	first := info.Addresses[0]
	if !strings.HasPrefix(first.URL, "https://") {
		t.Errorf("first address = %q, want an https:// scheme trusted from X-Forwarded-Proto", first.URL)
	}
	if !strings.Contains(first.URL, "knightloader.example.tld") {
		t.Errorf("first address = %q, does not carry the forwarded Host", first.URL)
	}
}

// TestRemoteAccessNotExposedWhenLoopbackAndUnprotected checks that a missing
// password alone, the state of every fresh install, does not trip the warning.
func TestRemoteAccessNotExposedWhenLoopbackAndUnprotected(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "")
	if info.PasswordSet {
		t.Fatal("a fresh test server already has a password set")
	}
	if info.Exposed {
		t.Error("Exposed = true for a request that arrived over loopback, httptest's own server")
	}
}

func TestRemoteAccessExposedOnNonLoopbackRequestWithNoPassword(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "192.0.2.10:8749")
	if !info.Exposed {
		t.Error("Exposed = false for a non-loopback Host with no password set")
	}
}

// TestRemoteAccessNotExposedOnNonLoopbackRequestWithAPassword authenticates
// with a bearer token because the cookie jar does not attach a cookie to a
// request whose Host was overridden.
func TestRemoteAccessNotExposedOnNonLoopbackRequestWithAPassword(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.Create("test script")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/remote-access", nil)
	req.Host = "192.0.2.10:8749"
	req.Header.Set("Authorization", "Bearer "+secret)
	var info RemoteAccessInfo
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/remote-access with a token and an overridden Host answered %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if !info.PasswordSet {
		t.Fatal("PasswordSet = false right after setting one")
	}
	if info.Exposed {
		t.Error("Exposed = true despite a password being set")
	}
}

// TestRemoteAccessIgnoresKLAddr pins that the warning does not depend on
// KL_ADDR (see requestIsNonLoopback).
func TestRemoteAccessIgnoresKLAddr(t *testing.T) {
	for _, addr := range []string{"", ":8749", "0.0.0.0:8749", "127.0.0.1:8749"} {
		t.Run("KL_ADDR="+addr, func(t *testing.T) {
			requireContainerDeployment(t)
			if addr != "" {
				t.Setenv("KL_ADDR", addr)
			}
			srv, _ := testServer(t)
			defer srv.Close()

			_, info := getRemoteAccess(t, srv.URL, "")
			if info.Exposed {
				t.Errorf("Exposed = true for a loopback request regardless of KL_ADDR=%q", addr)
			}
		})
	}
}

// TestRemoteAccessExposedWhenListeningWidelyEvenFromLoopback checks that an
// admin viewing the page from loopback still sees the warning when the
// listener is reachable from the LAN.
func TestRemoteAccessExposedWhenListeningWidelyEvenFromLoopback(t *testing.T) {
	requireContainerDeployment(t)
	prev := buildinfo.ListensWidely
	buildinfo.ListensWidely = true
	t.Cleanup(func() { buildinfo.ListensWidely = prev })

	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "")
	if info.PasswordSet {
		t.Fatal("a fresh test server already has a password set")
	}
	if !info.Exposed {
		t.Error("Exposed = false for a loopback request against a widely-bound, password-less instance")
	}
}

// TestPreferredAddress checks that loopback entries are skipped and a known
// domain wins over a LAN IP wherever it sits in the list.
func TestPreferredAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		addrs []ReachableAddress
		want  string
		found bool
	}{
		{"empty", nil, "", false},
		{"all loopback", []ReachableAddress{{URL: "http://127.0.0.1:8749", Loopback: true}}, "", false},
		{
			"loopback first, then real",
			[]ReachableAddress{
				{URL: "http://127.0.0.1:8749", Loopback: true},
				{URL: "http://192.168.1.20:8749", Loopback: false},
			},
			"http://192.168.1.20:8749", true,
		},
		{
			"a known domain outranks a LAN IP even though it sorts after it",
			[]ReachableAddress{
				{URL: "http://127.0.0.1:8749", Loopback: true},
				{URL: "http://192.168.1.20:8749", Loopback: false, Domain: false},
				{URL: "https://knightloader.example.com", Loopback: false, Domain: true},
			},
			"https://knightloader.example.com", true,
		},
		{
			"no domain known falls back to the first real address",
			[]ReachableAddress{
				{URL: "http://192.168.1.20:8749", Loopback: false, Domain: false},
				{URL: "http://192.168.1.30:8749", Loopback: false, Domain: false},
			},
			"http://192.168.1.20:8749", true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := preferredAddress(c.addrs)
			if ok != c.found || got != c.want {
				t.Errorf("preferredAddress(%v) = (%q, %v), want (%q, %v)", c.addrs, got, ok, c.want, c.found)
			}
		})
	}
}

func TestRemoteAccessQRMatchesThePrimaryAddress(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "")
	if len(info.Addresses) == 0 {
		t.Fatal("no addresses to build a QR from")
	}
	if info.QR == nil {
		t.Fatal("no QR code in the response")
	}
	if info.QR.Size <= 0 || len(info.QR.Bits) != info.QR.Size {
		t.Fatalf("QR = %+v, want Size rows of Bits", info.QR)
	}
	for _, row := range info.QR.Bits {
		if len(row) != info.QR.Size {
			t.Fatalf("QR row %q has length %d, want %d", row, len(row), info.QR.Size)
		}
		if strings.Trim(row, "01") != "" {
			t.Fatalf("QR row %q has a character that is not 0 or 1", row)
		}
	}
}

func TestIsDomainHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		hostport string
		want     bool
	}{
		{"knightloader.example.com", true},
		{"knightloader.example.com:8749", true},
		{"192.168.1.20", false},
		{"192.168.1.20:8749", false},
		{"127.0.0.1", false},
		{"[::1]:8749", false},
		{"[2001:db8::1]:8749", false},
		{"localhost", true}, // a name, even though isLoopbackHost treats it as loopback separately
	}
	for _, c := range cases {
		t.Run(c.hostport, func(t *testing.T) {
			if got := isDomainHost(c.hostport); got != c.want {
				t.Errorf("isDomainHost(%q) = %v, want %v", c.hostport, got, c.want)
			}
		})
	}
}

// TestRemoteAccessRemembersDomainSeenOnARequest checks that a domain a request
// arrived on is saved, so it stays listed when later requests use the LAN IP.
func TestRemoteAccessRemembersDomainSeenOnARequest(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/remote-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "knightloader.example.tld"
	req.Header.Set("X-Forwarded-Proto", "https")
	if code := doJSON(t, req, nil); code != http.StatusOK {
		t.Fatalf("GET /api/remote-access answered %d", code)
	}

	known := a.Settings.Get().KnownDomains
	want := "https://knightloader.example.tld"
	if len(known) != 1 || known[0] != want {
		t.Fatalf("KnownDomains = %v, want [%q]", known, want)
	}
}

func TestRemoteAccessDoesNotRememberLoopbackOrBareIP(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := getRemoteAccess(t, srv.URL, ""); code != http.StatusOK {
		t.Fatalf("loopback GET /api/remote-access answered %d", code)
	}
	if code, info := getRemoteAccess(t, srv.URL, "192.0.2.10:8749"); code != http.StatusOK {
		t.Fatalf("bare-IP GET /api/remote-access answered %d", code)
	} else if len(info.Addresses) == 0 {
		t.Fatal("no addresses reported for the bare-IP request")
	}

	if known := a.Settings.Get().KnownDomains; len(known) != 0 {
		t.Fatalf("KnownDomains = %v, want none after only loopback/bare-IP requests", known)
	}
}

func TestRemoteAccessDoesNotDuplicateAlreadyKnownDomain(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()

	cfg := a.Settings.Get()
	cfg.KnownDomains = []string{"https://knightloader.example.tld"}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/remote-access", nil)
	req.Host = "knightloader.example.tld"
	req.Header.Set("X-Forwarded-Proto", "https")
	if code := doJSON(t, req, nil); code != http.StatusOK {
		t.Fatalf("GET /api/remote-access answered %d", code)
	}

	if known := a.Settings.Get().KnownDomains; len(known) != 1 {
		t.Fatalf("KnownDomains = %v, want the same single entry, not a duplicate", known)
	}
}

// TestRemoteAccessKnownDomainKeepsItsOwnScheme checks that a plain HTTP LAN
// request does not turn a remembered https domain into http.
func TestRemoteAccessKnownDomainKeepsItsOwnScheme(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()

	cfg := a.Settings.Get()
	cfg.KnownDomains = []string{"https://knightloader.example.tld"}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	_, info := getRemoteAccess(t, srv.URL, "192.0.2.10:8749")

	var found *ReachableAddress
	for i := range info.Addresses {
		if info.Addresses[i].URL == "https://knightloader.example.tld" {
			found = &info.Addresses[i]
		}
	}
	if found == nil {
		t.Fatalf("addresses = %v, want the known domain listed with its own https scheme", info.Addresses)
	}
	if !found.Domain || found.Loopback {
		t.Errorf("known domain entry = %+v, want Domain=true Loopback=false", found)
	}
}

// TestRemoteAccessDesktopReportsNothingToWarnAbout checks that the desktop
// build, which opens no TCP port, reports no address and no exposure.
func TestRemoteAccessDesktopReportsNothingToWarnAbout(t *testing.T) {
	prev := buildinfo.Deployment
	buildinfo.Deployment = "desktop"
	t.Cleanup(func() { buildinfo.Deployment = prev })

	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "203.0.113.5:8749")
	if info.Deployment != "desktop" {
		t.Fatalf("Deployment = %q, want desktop", info.Deployment)
	}
	if info.Exposed {
		t.Error("Exposed = true on the desktop build, which has no network listener to be exposed on")
	}
	if len(info.Addresses) != 0 {
		t.Errorf("Addresses = %v, want none on the desktop build", info.Addresses)
	}
	if info.QR != nil {
		t.Error("a QR code was built for the desktop build, which has nothing to scan")
	}
}
