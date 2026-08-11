package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// requireContainerDeployment guards every test in this file that assumes
// the ordinary container path against buildinfo.Deployment being left on
// "desktop" by an unrelated, earlier test elsewhere in this package.
//
// It has to do the save-and-restore itself rather than trust the value
// already there: routes_lifecycle_test.go's TestDeploymentReflectsDesktop
// sets buildinfo.Deployment = "desktop" and only THEN calls its own
// lifecycleServer helper, whose prev := buildinfo.Deployment therefore
// snapshots "desktop" instead of whatever came before it, so its own
// t.Cleanup restores "desktop" right back, not the container default -
// the shared global stays stuck on "desktop" for every test that happens
// to run after it in the same binary, which is exactly what this file's
// own tests were seen failing to, only when run as part of the full
// package rather than filtered to this file alone. Fixed here rather than
// there: that file is a different lane, and defending against a global a
// test elsewhere can leave in either state is the more robust fix anyway,
// since it holds regardless of which other test mutated it or how.
func requireContainerDeployment(t *testing.T) {
	t.Helper()
	prev := buildinfo.Deployment
	buildinfo.Deployment = "container"
	t.Cleanup(func() { buildinfo.Deployment = prev })
}

// getRemoteAccess is remote access's own request builder, because several
// tests below need to set the Host header to something other than the
// loopback address httptest.NewServer actually listens on, and http.Get has
// no way to do that.
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

// doJSON is getJSON (links_test.go) for a request already built, needed here
// because Host cannot be set on a plain http.Get URL.
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

// TestRemoteAccessListsTheConnectionThatAskedFirst pins the one address this
// route can state as proven rather than inferred: whatever Host the request
// itself arrived on, first in the list.
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

// TestRemoteAccessNotExposedWhenLoopbackAndUnprotected: no password is the
// ordinary state of a fresh install, and by itself must never trip the loud
// warning. It needs the OTHER half too, an actual non-loopback request.
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

// TestRemoteAccessExposedOnNonLoopbackRequestWithNoPassword is the loud
// warning's proof half: a request that genuinely arrived addressed to a
// non-loopback host, with nothing protecting it, must be flagged.
func TestRemoteAccessExposedOnNonLoopbackRequestWithNoPassword(t *testing.T) {
	requireContainerDeployment(t)
	srv, _ := testServer(t)
	defer srv.Close()

	_, info := getRemoteAccess(t, srv.URL, "192.0.2.10:8749")
	if !info.Exposed {
		t.Error("Exposed = false for a non-loopback Host with no password set")
	}
}

// TestRemoteAccessNotExposedOnNonLoopbackRequestWithAPassword: the same
// non-loopback request is not a problem once a password protects it, which
// is the entire condition the warning exists to catch, not "non-loopback" by
// itself.
//
// Authenticated with a Bearer token rather than a login cookie: Go's
// net/http cookiejar keys its lookup off the request being sent, and a
// request whose Host has been overridden away from the jar's own idea of
// where it is going (exactly what this test needs, to simulate a
// non-loopback Host without actually standing up a second listener) does
// not get the cookie attached, empirically confirmed against the standard
// library rather than assumed. A token's Authorization header has no such
// interaction, and is a perfectly realistic way for a real client to
// authenticate this exact route in the first place.
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

// TestRemoteAccessIgnoresKLAddr pins a deliberate decision: the loud warning
// is proof-based (see requestIsNonLoopback's own comment for why), and does
// not additionally trip, or fail to trip, based on how KL_ADDR is set. A
// change that reintroduces a KL_ADDR-derived signal has to edit this test on
// purpose, not discover it broke something by accident.
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

// TestRemoteAccessExposedWhenListeningWidelyEvenFromLoopback closes the gap
// TestRemoteAccessNotExposedWhenLoopbackAndUnprotected above deliberately
// does NOT cover: that test's own request is loopback specifically to prove
// loopback-plus-no-password is not automatically a problem, but an admin
// looking at their own Access page is *always* on loopback relative to
// themselves, no matter how exposed the instance actually is to everyone
// else. Reproduced live before this fix: a LAN-reachable, password-less
// instance showed the warning to a visitor from another machine and never
// once to the admin sitting at the box itself, because every request they
// ever made was, by definition, the loopback case the other test covers.
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

// TestFirstNonLoopback is the pure half of the QR-loopback fix: given a
// mixed list, it must skip every loopback entry and return the first real
// one, not just the head of the slice.
func TestFirstNonLoopback(t *testing.T) {
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := firstNonLoopback(c.addrs)
			if ok != c.found || got != c.want {
				t.Errorf("firstNonLoopback(%v) = (%q, %v), want (%q, %v)", c.addrs, got, ok, c.want, c.found)
			}
		})
	}
}

// TestRemoteAccessQRMatchesThePrimaryAddress: the code has to be for
// something, and it has to be the same something the page's own address
// list already names first, or scanning it takes you somewhere the page
// never mentioned.
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

// TestRemoteAccessDesktopReportsNothingToWarnAbout: the desktop build never
// opens a TCP port at all (see remoteAccessInfo's own comment), so it must
// not print a network address or an exposure warning built from request
// fields that describe an in-process asset fetch instead of a real peer.
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
