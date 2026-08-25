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

	const secret = "s3cret-relay-key-abc123"
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

	if code, _ := putRelayConfig(t, srv.URL, `{"relayUrl":"https://relay.example.com","key":"keep-me-relay-test-key"}`); code != http.StatusOK {
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
	if stored, _ := a.Accounts.Get(relay.AccountService); stored != "keep-me-relay-test-key" {
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

	const key = "end-to-end-relay-test-key"
	if code, put := putRelayConfig(t, srv.URL, `{"relayUrl":"`+relaySrv.URL+`","key":"`+key+`"}`); code != http.StatusOK || !put.KeySet {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, put)
	}

	// A second, independent relay client standing in for a sibling instance -
	// answers every call with a fixed body, so the app's own outbound Proxy
	// can be checked against something known rather than another real API.
	sibling, err := relay.NewClient(relay.ClientOptions{
		URL:  relaySrv.URL,
		Key:  key,
		Self: relay.Announce{InstanceID: "sibling-1", Name: "Sibling", Deployment: "container"},
		Serve: func(ctx context.Context, req relay.ProxyRequest) (int, []byte) {
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
	inBody, inStatus, err := sibling.Proxy(context.Background(), selfID, http.MethodGet, "/api/tasks", nil)
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
		URL:  relaySrv.URL,
		Key:  key,
		Self: relay.Announce{InstanceID: "observer-1", Name: "Observer", Deployment: "container"},
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
