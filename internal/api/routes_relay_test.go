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

// getRelayConfig and putRelayConfig are this file's request builders. The PUT
// body is a raw string so an absent key field and an empty one read plainly.
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

func TestRelayConfigStartsUnconfigured(t *testing.T) {
	t.Parallel()
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

// TestRelayConfigRoundTripsWithoutLeakingTheKey checks that a later GET
// returns the saved address and the key only as a boolean. The raw body is
// searched for the key, since a decoded struct would not see an extra field.
func TestRelayConfigRoundTripsWithoutLeakingTheKey(t *testing.T) {
	t.Parallel()
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

	// The key was stored, not dropped: the relay client dials with it.
	stored, err := a.Accounts.Get(relay.AccountService)
	if err != nil {
		t.Fatal(err)
	}
	if stored != secret {
		t.Errorf("stored key = %q, want the one that was PUT", stored)
	}
}

// TestRelayConfigWithoutAKeyLeavesTheStoredOne covers saving an edited address
// from a form that was never shown the key.
func TestRelayConfigWithoutAKeyLeavesTheStoredOne(t *testing.T) {
	t.Parallel()
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

// TestRelayConfigEmptyKeyClearsIt checks that an explicit empty key removes
// the stored one, as accounts.Store.Set does.
func TestRelayConfigEmptyKeyClearsIt(t *testing.T) {
	t.Parallel()
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

// TestRelayConfigAddressReachesSettings checks that the address lands in
// settings.json while the key goes to the sealed credential store.
func TestRelayConfigAddressReachesSettings(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"  ws://192.168.20.11:8760/  ","key":"ws-relay-test-key-xyz"}`); code != http.StatusOK {
		t.Fatalf("PUT answered %d", code)
	}
	if got := a.Settings.Get().RelayURL; got != "ws://192.168.20.11:8760" {
		t.Errorf("Settings.RelayURL = %q, want the sanitised address saved through the settings store", got)
	}
}

// TestRelayConnectsAndProxiesBothDirections checks end to end, against a real
// relay and a second client standing in for a sibling, that PUT
// /api/relay/config leaves a connected client in a.Federation and that calls
// cross in both directions.
func TestRelayConnectsAndProxiesBothDirections(t *testing.T) {
	t.Parallel()
	relaySrv := httptest.NewServer(relay.New())
	defer relaySrv.Close()

	srv, a := testServer(t)
	defer srv.Close()

	const key = "end-to-end-relay-test-key-0123456789"
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"`+relaySrv.URL+`","key":"`+key+`"}`); code != http.StatusOK || !put.KeySet {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	// The sibling answers every call with a fixed body.
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

	// Both sides connect and announce asynchronously.
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
	// A relay peer's Name is its InstanceID; the announced name is
	// DisplayName.
	if len(list) != 1 || list[0].Name != "sibling-1" || list[0].DisplayName != "Sibling" || list[0].RelayID != "sibling-1" {
		t.Fatalf("GET /api/instances = %+v, want the sibling visible through the relay", list)
	}

	// Outbound, addressed by the sibling's InstanceID.
	body, status, err := a.Federation.Proxy(context.Background(), "sibling-1", http.MethodGet, "/api/tasks", nil)
	if err != nil {
		t.Fatalf("proxy to sibling: %v", err)
	}
	if status != http.StatusOK || string(body) != `{"from":"sibling"}` {
		t.Errorf("proxy to sibling = %d %s, want the sibling's own fixed answer", status, body)
	}

	// Inbound: the sibling reaches the app's real API through
	// relayProxyHandler.
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

// TestChangingInstanceNameReconnectsTheRelayClient checks that a renamed
// instance reconnects, since the relay only learns the name from the hello
// frame.
func TestChangingInstanceNameReconnectsTheRelayClient(t *testing.T) {
	t.Parallel()
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

	// Without an InstanceName the app announces under its hostname.
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

// TestRelayProxyHonoursTheAuthorizationField runs relay calls against the real
// Handler, since what matters is whether the auth guard believes the
// forwarded credential, not only whether the header is copied.
func TestRelayProxyHonoursTheAuthorizationField(t *testing.T) {
	t.Parallel()
	_, a := testServer(t)
	serve := relayProxyHandler(Handler(a))

	if status, body := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
	}); status != http.StatusOK {
		t.Fatalf("unprotected instance answered %d (%s), want 200", status, body)
	}

	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	// A sibling without a token gets through, because reaching this handler
	// already took the group key.
	if status, body := serve(context.Background(), relay.ProxyCall{
		Method: http.MethodGet, Path: "/api/tasks",
	}); status != http.StatusOK {
		t.Errorf("a group sibling with no token = %d (%s), want 200; the relay socket is the credential", status, body)
	}

	// A stale token must not fail harder than no token.
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

// TestRelayProxyRefusesEverythingButTasksAndLinks checks that group membership
// reaches only the allowlisted routes rather than the whole API.
func TestRelayProxyRefusesEverythingButTasksAndLinks(t *testing.T) {
	t.Parallel()
	_, a := testServer(t)
	serve := relayProxyHandler(Handler(a))
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) int {
		status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodGet, Path: path})
		return status
	}

	for _, allowed := range []string{
		"/api/tasks", "/api/links", "/api/queue", "/api/tasks/7", "/api/queue/move",
		"/api/tasks?state=active", "/api/auth", "/api/instances", "/api/appearance",
		"/api/remote-access",
	} {
		if status := get(allowed); status == http.StatusForbidden {
			t.Errorf("%s = 403, but a group member needs it", allowed)
		}
	}

	// Hoster logins and paths, the password, standing credentials and the
	// phrase itself.
	for _, refused := range []string{
		"/api/settings", "/api/accounts", "/api/auth/password", "/api/tokens",
		"/api/connect", "/api/connect/reveal", "/api/scripts", "/api/relay/config",
	} {
		if status := get(refused); status != http.StatusForbidden {
			t.Errorf("%s = %d, want 403; a group sibling must not reach this", refused, status)
		}
	}

	for _, path := range []string{"/api/auth", "/api/instances", "/api/remote-access"} {
		status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodPost, Path: path})
		if status != http.StatusForbidden {
			t.Errorf("POST %s = %d, want 403; these are readable, not writable", path, status)
		}
	}

	// The one writable exception. The call is bodyless, as the relay builds it
	// for a frame without payload, which must not panic.
	if status, _ := serve(context.Background(), relay.ProxyCall{Method: http.MethodPost, Path: "/api/appearance"}); status == http.StatusForbidden {
		t.Error("POST /api/appearance = 403, but the app has to be able to set the palette")
	} else if status == http.StatusInternalServerError {
		t.Errorf("POST /api/appearance = 500; a bodyless relay call must not reach a panic")
	}

	for _, path := range []string{"/api/settings", "/api/accounts", "/api/relay/config"} {
		for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
			status, _ := serve(context.Background(), relay.ProxyCall{Method: method, Path: path})
			if status != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", method, path, status)
			}
		}
	}
}

// TestTheAppCanAnswerCaptchasOverTheRelay drives the captcha calls a phone
// joined with the phrase makes through the relay, against a JD holding one
// hCaptcha, and checks that no other route under /api/captcha is forwarded.
func TestTheAppCanAnswerCaptchasOverTheRelay(t *testing.T) {
	jd, solvedWith := fakeJDWithHCaptcha(t)
	t.Setenv("KL_JD", jd.URL)
	a := testApp(t)
	serve := relayProxyHandler(Handler(a))
	// With a password set, a call the relay did not vouch for is a 401.
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) (int, []byte) {
		return serve(context.Background(), relay.ProxyCall{Method: method, Path: path, Body: []byte(body)})
	}
	type listed struct {
		ID string `json:"id"`
	}

	status, body := call(http.MethodPost, "/api/captcha/refresh", `{}`)
	var refreshed []listed
	if status != http.StatusOK || json.Unmarshal(body, &refreshed) != nil || len(refreshed) != 1 {
		t.Fatalf("POST /api/captcha/refresh = %d (%s), want the one challenge JD holds", status, body)
	}
	id := refreshed[0].ID

	status, body = call(http.MethodGet, "/api/captcha", "")
	var pending []listed
	if status != http.StatusOK || json.Unmarshal(body, &pending) != nil || len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("GET /api/captcha = %d (%s), want challenge %s", status, body, id)
	}

	status, body = call(http.MethodPost, "/api/captcha/"+id+"/answer", `{"text":"a-token"}`)
	var answered struct {
		StillValid bool `json:"stillValid"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &answered) != nil || !answered.StillValid {
		t.Errorf("POST /api/captcha/%s/answer = %d (%s), want 200 and stillValid", id, status, body)
	}
	if len(solvedWith()) == 0 {
		t.Error("the answer never reached JD")
	}

	if status, body := call(http.MethodPost, "/api/captcha/"+id+"/skip", `{"scope":"skip-once"}`); status != http.StatusNoContent {
		t.Errorf("POST /api/captcha/%s/skip = %d (%s), want 204", id, status, body)
	}

	// An escaped slash keeps the id one segment, so the call reaches the skip
	// route whole and it is JD's id check that turns it down.
	if status, body := call(http.MethodPost, "/api/captcha/7%2F8/skip", `{"scope":"skip-once"}`); status != http.StatusBadRequest ||
		!bytes.Contains(body, []byte(`"7/8"`)) {
		t.Errorf("POST /api/captcha/7%%2F8/skip = %d (%s), want the skip route's 400 about id 7/8", status, body)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/captcha"},
		{http.MethodGet, "/api/captcha/refresh"},
		{http.MethodGet, "/api/captcha/c1/widget"},
		{http.MethodPost, "/api/captcha/c1/answer/again"},
		{http.MethodPost, "/api/captcha//answer"},
		{http.MethodPost, "/api/captcha/solvers"},
		{http.MethodPost, "/api/captchas/c1/answer"},
	} {
		if status, _ := call(c.method, c.path, `{}`); status != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403; only the calls the app makes are forwarded", c.method, c.path, status)
		}
	}
}

// fixedSibling is a second relay client standing in for another instance, so a
// test can prove a relay carried something rather than only that a socket
// opened.
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

// TestServingARelayFromInsideAnInstance runs the whole loop: the switch opens
// the socket, the instance dials its own relay, a second instance joins with
// the same key, they see each other, and a call crosses.
func TestServingARelayFromInsideAnInstance(t *testing.T) {
	t.Parallel()
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

	// This instance's own client and the sibling's.
	_, cfg := getRelayConfig(t, srv.URL)
	if !cfg.Serve || cfg.ServeClients != 2 {
		t.Errorf("config = %+v, want serve=true and 2 connected clients", cfg)
	}
}

// TestAServedRelayAdmitsOnlyTheKeyTheInstanceStores checks Admit: unlike the
// standalone relay, which groups any key, a relay served from an instance must
// not become a meeting place for whoever finds its address.
func TestAServedRelayAdmitsOnlyTheKeyTheInstanceStores(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"","key":"the-key-this-instance-serves-0123456789","serve":true}`); code != http.StatusOK || !put.Serve {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	stranger := fixedSibling(t, srv.URL, "some-other-relay-key-entirely-0123456789", "stranger-1")

	// Long enough for a connection or a retry to succeed. The relay's count is
	// what shows admission: the client counts itself connected as soon as its
	// hello is out, before the relay has read the key and closed the socket.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, cfg := getRelayConfig(t, srv.URL); cfg.ServeClients != 0 {
			t.Fatalf("serveClients = %d, want the stranger never registered", cfg.ServeClients)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(stranger.Siblings()) != 0 {
		t.Error("the stranger was shown siblings through a relay that refused its key")
	}
}

// TestWithTheSwitchOffTheRelaySocketIsNotThere checks that an instance not
// serving a relay answers 404, like a version without the feature.
func TestWithTheSwitchOffTheRelaySocketIsNotThere(t *testing.T) {
	t.Parallel()
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

	// With the switch on, the same GET reaches the WebSocket handshake, so
	// the 404 came from the switch.
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

// TestTheServeSwitchIsLeftAloneWhenTheRequestOmitsIt checks that saving only
// the address does not change the serve switch.
func TestTheServeSwitchIsLeftAloneWhenTheRequestOmitsIt(t *testing.T) {
	t.Parallel()
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
