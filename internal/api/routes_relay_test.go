package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// getRelayConfig and putRelayConfig are this file's two request builders. The
// PUT body is handed over as a raw string rather than marshalled from a
// struct, because half of what these tests pin is the difference between a
// key field that is ABSENT and one that is present and empty - a distinction
// a Go struct with a *string in it can express but which is far easier to
// read as the JSON that actually goes over the wire.
func getRelayConfig(t *testing.T, base string) (int, relayConfig) {
	t.Helper()
	resp, err := http.Get(base + "/api/relay/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out relayConfig
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("GET /api/relay/config answered unparseable JSON: %v", err)
		}
	}
	return resp.StatusCode, out
}

func putRelayConfig(t *testing.T, base, body string) (int, relayConfig) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/relay/config", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out relayConfig
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("PUT /api/relay/config answered unparseable JSON: %v", err)
		}
	} else {
		b, _ := io.ReadAll(resp.Body)
		t.Logf("PUT /api/relay/config answered %d: %s", resp.StatusCode, b)
	}
	return resp.StatusCode, out
}

// TestRelayConfigStartsUnconfigured: a fresh install dials nothing and has no
// key, and the route has to say so plainly rather than 404 or error - the
// settings card asks this before anything has ever been saved.
func TestRelayConfigStartsUnconfigured(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, cfg := getRelayConfig(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/relay/config answered %d", code)
	}
	if cfg.RelayURL != "" || cfg.KeySet {
		t.Errorf("config = %+v, want an empty address and keySet=false before anything is saved", cfg)
	}
}

// TestRelayConfigRoundTripsWithoutLeakingTheKey is the whole contract in one
// test: what was PUT comes back from a later GET, the address verbatim (bar
// the normalising sanitizeRelay does), the key only as a boolean - and the
// key's plaintext appears nowhere in either response body, which is checked
// against the raw bytes rather than the decoded struct, because a struct can
// only fail to see a field the server should never have sent.
func TestRelayConfigRoundTripsWithoutLeakingTheKey(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	const secret = "s3cret-relay-key-abc123-0123456789"
	code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com/","key":"`+secret+`"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config answered %d", code)
	}
	if put.RelayURL != "https://relay.example.com" || !put.KeySet {
		t.Errorf("PUT answered %+v, want the sanitised address and keySet=true", put)
	}

	code, got := getRelayConfig(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/relay/config answered %d", code)
	}
	if got != put {
		t.Errorf("GET answered %+v, want the same %+v PUT just reported", got, put)
	}

	resp, err := http.Get(srv.URL + "/api/relay/config")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if bytes.Contains(raw, []byte(secret)) {
		t.Errorf("GET /api/relay/config answered %s, which carries the stored key itself", raw)
	}

	// It really was stored, rather than quietly dropped in the name of not
	// leaking it: the relay client has to be able to dial with this.
	stored, err := a.Accounts.Get(relay.AccountService)
	if err != nil {
		t.Fatal(err)
	}
	if stored != secret {
		t.Errorf("stored key = %q, want the one that was PUT", stored)
	}
}

// TestRelayConfigWithoutAKeyLeavesTheStoredOne covers the ordinary save of an
// edited address, from a form that was never shown the key and therefore has
// nothing to send back. An absent key field must not silently disconnect the
// instance by clearing the credential it dials with.
func TestRelayConfigWithoutAKeyLeavesTheStoredOne(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com","key":"keep-me-relay-test-key-0123456789"}`); code != http.StatusOK {
		t.Fatalf("first PUT answered %d", code)
	}
	code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay2.example.com"}`)
	if code != http.StatusOK {
		t.Fatalf("second PUT answered %d", code)
	}
	if put.RelayURL != "https://relay2.example.com" {
		t.Errorf("RelayURL = %q, want the newly saved address", put.RelayURL)
	}
	if !put.KeySet {
		t.Error("keySet = false after a PUT that did not mention the key; the stored key was cleared")
	}
	if stored, _ := a.Accounts.Get(relay.AccountService); stored != "keep-me-relay-test-key-0123456789" {
		t.Errorf("stored key = %q, want it untouched by a PUT that did not name it", stored)
	}
}

// TestRelayConfigEmptyKeyClearsIt is the other half of the pointer: an
// explicit empty string is the only way a stored secret can be removed on
// purpose, the same convention accounts.Store.Set has always had.
func TestRelayConfigEmptyKeyClearsIt(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com","key":"drop-me-relay-test-key"}`); code != http.StatusOK {
		t.Fatalf("first PUT answered %d", code)
	}
	code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com","key":""}`)
	if code != http.StatusOK {
		t.Fatalf("clearing PUT answered %d", code)
	}
	if put.KeySet {
		t.Error("keySet = true after an explicitly empty key; there is then no way to ever remove one")
	}
	if stored, _ := a.Accounts.Get(relay.AccountService); stored != "" {
		t.Errorf("stored key = %q, want it cleared", stored)
	}
}

// TestRelayConfigAddressReachesSettings pins where the public half actually
// lands: settings.json, beside KnownDomains, and not in the sealed credential
// store the key goes to. The two are stored apart on purpose (see
// settings_relay.go), and a change that quietly moved the address into the
// keyring - or the key into the settings - would otherwise pass every other
// test in this file.
func TestRelayConfigAddressReachesSettings(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"  ws://192.168.20.11:8760/  ","key":"ws-relay-test-key-xyz"}`); code != http.StatusOK {
		t.Fatalf("PUT answered %d", code)
	}
	if got := a.Settings.Get().RelayURL; got != "ws://192.168.20.11:8760" {
		t.Errorf("Settings.RelayURL = %q, want the sanitised address saved through the settings store", got)
	}
}

// TestRelayConnectsAndProxiesBothDirections is the end-to-end proof that
// PUT /api/relay/config actually results in a connected relay.Client wired
// into a.Federation - not just a stored address and key nobody ever dials
// with. Runs against a real relay (internal/relay.Server) and a second,
// independent relay client standing in for a sibling instance, over real
// network sockets and a real HTTP round trip in each direction - the only
// way a mistake in the wiring between this file, internal/app and
// internal/api would actually show up, the way it did the first time this
// route was built and every individual piece's own unit tests still passed.
func TestRelayConnectsAndProxiesBothDirections(t *testing.T) {
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()

	srv, a := testServer(t)
	defer srv.Close()

	const key = "end-to-end-relay-test-key-0123456789"
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"`+relaySrv.URL+`","key":"`+key+`"}`); code != http.StatusOK || !put.KeySet {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	// A second, independent relay client standing in for a sibling instance -
	// answers every call with a fixed body, so the app's own outbound Proxy
	// can be checked against something known rather than another real API.
	sibling, err := relay.NewClient(relay.ClientOptions{
		URL:      relaySrv.URL,
		Key:      key,
		FrameKey: relay.FrameKeyFromRelayKey(key),
		Self:     relay.Announce{InstanceID: "sibling-1", Name: "Sibling", Deployment: "container"},
		Serve: func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte(`{"from":"sibling"}`)
		},
	})
	if err != nil {
		t.Fatalf("build sibling client: %v", err)
	}
	sibling.Start()
	defer sibling.Close()

	// Both sides connect and announce asynchronously, so this is the one
	// thing here worth polling rather than asserting on the first try.
	var list []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		RelayID     string `json:"relayId"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(srv.URL + "/api/instances"); err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&list)
			resp.Body.Close()
		}
		if len(list) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Name is the address - always the InstanceID for a relay peer, never
	// the name it announced (federation.Manager.reachable's own doc comment
	// on why) - DisplayName carries "Sibling" instead.
	if len(list) != 1 || list[0].Name != "sibling-1" || list[0].DisplayName != "Sibling" || list[0].RelayID != "sibling-1" {
		t.Fatalf("GET /api/instances = %+v, want the sibling visible through the relay", list)
	}

	// Outbound: the app calls the sibling through the relay, addressed by
	// its InstanceID - the same address the list above just returned.
	body, status, err := a.Federation.Proxy(context.Background(), "sibling-1", http.MethodGet, "/api/tasks", nil)
	if err != nil {
		t.Fatalf("proxy to sibling: %v", err)
	}
	if status != http.StatusOK || string(body) != `{"from":"sibling"}` {
		t.Errorf("proxy to sibling = %d %s, want the sibling's own fixed answer", status, body)
	}

	// Inbound: the sibling calls the app's own real API through the relay,
	// proving SetSelfServeHandler + relayProxyHandler actually reach it
	// rather than the relay having nothing to answer with on that side.
	selfID := a.Settings.Get().InstanceID
	inBody, inStatus, err := sibling.Proxy(context.Background(), selfID, http.MethodGet, "/api/tasks", nil, "")
	if err != nil {
		t.Fatalf("sibling proxy into the app: %v", err)
	}
	if inStatus != http.StatusOK {
		t.Fatalf("sibling proxy into the app = %d %s, want 200", inStatus, inBody)
	}
	var tasks []any
	if err := json.Unmarshal(inBody, &tasks); err != nil {
		t.Fatalf("the app's own /api/tasks answered unparseable JSON through the relay: %v (%s)", err, inBody)
	}
}

// TestChangingInstanceNameReconnectsTheRelayClient: the relay only learns a
// display name once, in the hello frame a connection opens with - a name
// changed afterwards on Settings has to reconnect the client, or every
// sibling keeps showing the old one until something else happens to drop the
// connection. Caught live on the actual Bottich deployment before this test
// existed: two real instances configured with the same relay showed each
// other's container hostname long after both had a proper InstanceName set.
func TestChangingInstanceNameReconnectsTheRelayClient(t *testing.T) {
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()

	srv, _ := testServer(t)
	defer srv.Close()

	const key = "instance-name-reconnect-test-key"
	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"`+relaySrv.URL+`","key":"`+key+`"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config failed: %d", code)
	}

	observer, err := relay.NewClient(relay.ClientOptions{
		URL:      relaySrv.URL,
		Key:      key,
		FrameKey: relay.FrameKeyFromRelayKey(key),
		Self:     relay.Announce{InstanceID: "observer-1", Name: "Observer", Deployment: "container"},
	})
	if err != nil {
		t.Fatalf("build observer client: %v", err)
	}
	observer.Start()
	defer observer.Close()

	waitForSiblingName := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		var last string
		for time.Now().Before(deadline) {
			for _, sib := range observer.Siblings() {
				last = sib.Name
				if sib.Name == want {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("observer never saw sibling name %q, last seen %q", want, last)
	}
	waitForAnySibling := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if len(observer.Siblings()) > 0 {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("observer never saw the app connect at all")
	}

	// Before any InstanceName is set, the app announces under its hostname -
	// whatever that is, it is not yet "Renamed Instance".
	waitForAnySibling()
	if got := observer.Siblings()[0].Name; got == "Renamed Instance" {
		t.Fatal("the app announced the not-yet-set name before it was ever saved")
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/api/settings", bytes.NewReader([]byte(`{"instanceName":"Renamed Instance"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /api/settings answered %d", resp.StatusCode)
	}

	waitForSiblingName("Renamed Instance")
}

// TestRelayProxyHonoursTheAuthorizationField pins the one thing that lets the
// mobile companion app reach an instance it can only see through a relay.
//
// A relay-proxied call is replayed against the target's own real handler, and
// that handler's guard accepts exactly two credentials: a session cookie (a
// browser thing, which nothing on this transport has) and a bearer token. The
// frame carried neither until ProxyRequest.Authorization existed, so every
// relay call arrived unauthenticated - fine between instances, which is how
// federation has always worked, but it left a phone able to talk only to
// instances with no password at all. Those are precisely the instances nobody
// should be exposing to a relay, so the feature was inverted: it worked only
// where it should not be used.
//
// Tested against the real Handler(a) rather than a stub, because what is
// being asserted is the interaction with the actual auth guard - a stub would
// prove the header is copied and nothing about whether it is believed.
func TestRelayProxyHonoursTheAuthorizationField(t *testing.T) {
	_, a := testServer(t)
	serve := relayProxyHandler(Handler(a))

	// Unprotected first: the pre-existing contract, and the baseline that
	// makes the 401s below mean "the password did it", not "this route was
	// broken all along".
	if status, body := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
	}); status != http.StatusOK {
		t.Fatalf("unprotected instance answered %d (%s), want 200 - relay calls have always worked without a credential here", status, body)
	}

	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	// A sibling with no bearer token still gets through, because getting HERE
	// already required presenting the group key. This is the case that used to
	// answer 401 and broke a phrase group the moment one instance was given a
	// password - proved live on two preview containers before it was fixed.
	if status, body := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
	}); status != http.StatusOK {
		t.Errorf("a group sibling with no token = %d (%s), want 200 - the relay socket IS the credential", status, body)
	}

	// A garbage token must not be worse than no token. It is not evidence of
	// anything either way, and rejecting the request for carrying one would
	// make a stale credential on a peer fail harder than an absent one.
	if status, _ := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
		Authorization: "Bearer not-a-real-token",
	}); status != http.StatusOK {
		t.Errorf("a group sibling with a stale token = %d, want 200", status)
	}

	_, secret, err := a.APITokens.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	status, body := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
		Authorization: "Bearer " + secret,
	})
	if status != http.StatusOK {
		t.Fatalf("a real API token = %d (%s), want 200", status, body)
	}
	var tasks []any
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Errorf("authenticated relay call answered unparseable JSON: %v (%s)", err, body)
	}
}

// Being in the group buys a named list of routes and nothing else. Without
// this the mark relayProxyHandler attaches would be a password bypass for the
// whole API, reachable by anyone holding the phrase - which is everyone in
// the group, but not for THESE routes.
func TestRelayProxyRefusesEverythingButTasksAndLinks(t *testing.T) {
	_, a := testServer(t)
	serve := relayProxyHandler(Handler(a))
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) int {
		status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodGet, Path: path})
		return status
	}

	// The working surface: what a sibling or the phone app actually does.
	for _, allowed := range []string{
		"/api/tasks", "/api/links", "/api/queue", "/api/tasks/7", "/api/queue/move",
		"/api/tasks?state=active", "/api/auth", "/api/instances", "/api/appearance",
		"/api/remote-access",
	} {
		if status := get(allowed); status == http.StatusForbidden {
			t.Errorf("%s = 403, but a group member needs it", allowed)
		}
	}

	// Each of these would hand a sibling something the phrase is not supposed
	// to buy: the stored hoster logins and download paths, the ability to lock
	// this instance out from under its owner, a standing credential, and the
	// phrase itself.
	for _, refused := range []string{
		"/api/settings", "/api/accounts", "/api/auth/password", "/api/tokens",
		"/api/connect", "/api/connect/reveal", "/api/scripts", "/api/relay/config",
	} {
		if status := get(refused); status != http.StatusForbidden {
			t.Errorf("%s = %d, want 403 - a group sibling must not reach this", refused, status)
		}
	}

	// The read-only three are read-only. POST /api/instances registers a peer
	// and POST /api/auth/logout is somebody else's session; being in the group
	// is permission to look at these, never to write them.
	for _, path := range []string{"/api/auth", "/api/instances", "/api/appearance", "/api/remote-access"} {
		status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodPost, Path: path})
		if status != http.StatusForbidden {
			t.Errorf("POST %s = %d, want 403 - these are readable, not writable", path, status)
		}
	}
}

// ---- the relay served from inside an instance -------------------------------

// fixedSibling is a second relay client standing in for another instance, so a
// test can prove a relay actually carried something rather than only that a
// socket opened.
func fixedSibling(t *testing.T, url, key, id string) *relay.Client {
	t.Helper()
	c, err := relay.NewClient(relay.ClientOptions{
		URL:      url,
		Key:      key,
		FrameKey: relay.FrameKeyFromRelayKey(key),
		Self:     relay.Announce{InstanceID: id, Name: "Sibling", Deployment: "desktop"},
		Serve: func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
			return http.StatusOK, []byte(`{"from":"sibling"}`)
		},
	})
	if err != nil {
		t.Fatalf("build sibling client: %v", err)
	}
	c.Start()
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestServingARelayFromInsideAnInstance is jdp's own framing of the feature:
// "Können wir nicht das relay in KL integrieren? Also wenn jemand zb zwei
// desktop instanzen hat und die koppeln will, dass er dann in einer instanz
// das relay aktiveren kann?"
//
// The whole loop in one test, because every part of it is new and any one of
// them failing quietly would look like the others working: the switch turns
// the socket on, this instance's own relay client dials its own relay, a
// second instance dials the same address with the same key, they see each
// other on the Instances page, and a call actually crosses.
func TestServingARelayFromInsideAnInstance(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	const key = "a-relay-served-from-inside-an-instance"
	code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"`+srv.URL+`","key":"`+key+`","serve":true}`)
	if code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v, want serve=true", code, put)
	}

	fixedSibling(t, srv.URL, key, "sibling-1")

	var list []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		RelayID     string `json:"relayId"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(srv.URL + "/api/instances"); err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&list)
			resp.Body.Close()
		}
		if len(list) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(list) != 1 || list[0].RelayID != "sibling-1" {
		t.Fatalf("GET /api/instances = %+v, want the sibling visible through the relay this instance is serving", list)
	}

	body, status, err := a.Federation.Proxy(context.Background(), "sibling-1", http.MethodGet, "/api/tasks", nil)
	if err != nil {
		t.Fatalf("proxy to the sibling through our own relay: %v", err)
	}
	if status != http.StatusOK || string(body) != `{"from":"sibling"}` {
		t.Errorf("proxy = %d %s, want the sibling's own fixed answer", status, body)
	}

	// Two connections: this instance's own client and the sibling's. The count
	// is what the settings card shows, so it has to be the real registry rather
	// than something derived from the switch being on.
	_, cfg := getRelayConfig(t, srv.URL)
	if !cfg.Serve || cfg.ServeClients != 2 {
		t.Errorf("config = %+v, want serve=true and 2 connected clients", cfg)
	}
}

// TestAServedRelayAdmitsOnlyTheKeyTheInstanceStores is the reason Admit exists
// at all. The standalone relay accepts every key and merely groups by it,
// which is right for a rendezvous point nobody's downloads pass through. This
// one rides on the address somebody published so their own instances could
// reach them, so admitting every key would quietly turn their server into a
// meeting place for whoever finds it.
func TestAServedRelayAdmitsOnlyTheKeyTheInstanceStores(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","key":"the-key-this-instance-serves-0123456789","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	stranger := fixedSibling(t, srv.URL, "some-other-relay-key-entirely-0123456789", "stranger-1")

	// Long enough that a connection which was going to succeed has, and that
	// the client has had time for a reconnect attempt or two after being
	// refused. Connected() is the client's own view; ServeClients is the
	// relay's. Both have to say no, because either one alone could be a
	// timing artefact.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stranger.Connected() {
			t.Fatal("a client carrying a key this instance does not serve was admitted to its relay")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, cfg := getRelayConfig(t, srv.URL); cfg.ServeClients != 0 {
		t.Errorf("serveClients = %d, want the stranger never registered", cfg.ServeClients)
	}
}

// TestWithTheSwitchOffTheRelaySocketIsNotThere: an instance that is not
// serving a relay answers the way one that never had the feature does. That
// is deliberate rather than incidental - a client told "no such endpoint" can
// treat every version of KnightLoader alike, while a 403 would have it
// reporting a refusal to somebody who never asked for anything.
func TestWithTheSwitchOffTheRelaySocketIsNotThere(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/relay/connect")
	if err != nil {
		t.Fatalf("GET /relay/connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /relay/connect answered %d with the switch off, want 404", resp.StatusCode)
	}

	// And it is the switch doing it, not a route that was never registered:
	// with the switch on the same plain GET gets as far as the WebSocket
	// handshake, which refuses it for not being one.
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","key":"a-key-long-enough-to-pass","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}
	on, err := http.Get(srv.URL + "/relay/connect")
	if err != nil {
		t.Fatalf("GET /relay/connect with the switch on: %v", err)
	}
	defer on.Body.Close()
	if on.StatusCode == http.StatusNotFound {
		t.Error("GET /relay/connect still answers 404 with the switch on")
	}
}

// TestTheServeSwitchIsLeftAloneWhenTheRequestOmitsIt: the address form and the
// switch are two controls on one card, and PUT carries both. Serve is a
// pointer for the same reason Key is - a save from a form that only edited the
// address must not carry the switch back to whatever it was when that form was
// drawn - and a pointer that is only DECLARED optional is not optional.
func TestTheServeSwitchIsLeftAloneWhenTheRequestOmitsIt(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","key":"a-key-long-enough-to-pass","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("turning it on = %d %+v", code, put)
	}
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com"}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("saving an address turned the switch off: %d %+v", code, put)
	}
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com","serve":false}`); code != http.StatusOK || put.Serve {
		t.Fatalf("turning it off = %d %+v", code, put)
	}
	if _, cfg := getRelayConfig(t, srv.URL); cfg.Serve || cfg.ServeClients != 0 {
		t.Errorf("config = %+v, want serve=false and no clients reported", cfg)
	}
}
