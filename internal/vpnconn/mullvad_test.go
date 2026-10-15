package vpnconn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn/mullvad"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
)

const mullvadAccount = "1234567812345678"

// fakeMullvad is Mullvad's API for one account, with its relay list pointing
// at a test server when there is one.
type fakeMullvad struct {
	mu      sync.Mutex
	expiry  time.Time
	max     int
	devices map[string]string
	next    int
	added   int
	removed []string
	relay   string
	port    uint16
	pub     string
	// down has Mullvad list the relay as inactive.
	down bool
}

func newFakeMullvad(t *testing.T) *fakeMullvad {
	f := &fakeMullvad{expiry: time.Now().Add(24 * time.Hour), max: 5, devices: map[string]string{},
		relay: "198.51.100.1", port: 51820, pub: "5JMPeO7gXIbR5CnUa/NPNK4L5GqUnreF0/Bozai4pl4="}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := mullvadClient
	mullvadClient = func() *mullvad.Client { return &mullvad.Client{API: srv.URL, WhereURL: srv.URL + "/where"} }
	t.Cleanup(func() { mullvadClient = old })
	return f
}

func (f *fakeMullvad) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	code := func(status int, c string) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"code": c})
	}
	device := func(id string) map[string]any {
		return map[string]any{"id": id, "name": "able cat", "pubkey": f.devices[id],
			"ipv4_address": vpntest.ClientAddr.String() + "/32", "ipv6_address": "fc00:bbbb:bbbb:bb01::2/128"}
	}
	switch {
	case r.URL.Path == "/auth/v1/token":
		var body struct {
			AccountNumber string `json:"account_number"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.AccountNumber != mullvadAccount {
			code(http.StatusBadRequest, mullvad.InvalidAccount)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok", "expiry": time.Now().Add(time.Hour).Format(time.RFC3339)})
	case r.URL.Path == "/public/relays/wireguard/v2":
		fmt.Fprintf(w, `{"locations": {"at-vie": {"latitude": 48.2, "longitude": 16.4}},
		  "wireguard": {"port_ranges": [[%d, %d]], "ipv4_gateway": "10.2.0.1", "relays": [
		    {"hostname": "at-vie-wg-001", "location": "at-vie", "active": %t, "ipv4_addr_in": %q, "weight": 100, "include_in_country": true, "public_key": %q}]}}`,
			f.port, f.port, !f.down, f.relay, f.pub)
	case r.URL.Path == "/where":
		w.Write([]byte(`{"latitude": 48.3, "longitude": 16.5}`))
	case r.Header.Get("Authorization") != "Bearer tok":
		code(http.StatusUnauthorized, mullvad.InvalidAccessToken)
	case r.URL.Path == "/accounts/v1/accounts/me":
		json.NewEncoder(w).Encode(map[string]any{"id": "a", "expiry": f.expiry.Format(time.RFC3339)})
	case r.URL.Path == "/accounts/v1/devices" && r.Method == http.MethodPost:
		if len(f.devices) >= f.max {
			code(http.StatusBadRequest, mullvad.MaxDevicesReached)
			return
		}
		var body struct{ Pubkey string }
		json.NewDecoder(r.Body).Decode(&body)
		f.next++
		f.added++
		id := fmt.Sprintf("dev-%d", f.next)
		f.devices[id] = body.Pubkey
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(device(id))
	case strings.HasPrefix(r.URL.Path, "/accounts/v1/devices/"):
		id := strings.TrimPrefix(r.URL.Path, "/accounts/v1/devices/")
		if _, ok := f.devices[id]; !ok {
			code(http.StatusNotFound, mullvad.DeviceNotFound)
			return
		}
		if r.Method == http.MethodDelete {
			delete(f.devices, id)
			f.removed = append(f.removed, id)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(device(id))
	default:
		http.NotFound(w, r)
	}
}

func codeOf(err error) string {
	return classify(err).code
}

func TestMullvadKeepsUsingTheDeviceItRegistered(t *testing.T) {
	f := newFakeMullvad(t)
	ctx := context.Background()
	first, err := openMullvad(ctx, Login{"accountNumber": "1234 5678 1234 5678"})
	if err != nil {
		t.Fatal(err)
	}
	if first.cfg.Peer.Endpoint != "198.51.100.1:51820" || len(first.cfg.DNS) != 1 {
		t.Errorf("config %+v", first.cfg)
	}
	again, err := openMullvad(ctx, first.login)
	if err != nil {
		t.Fatal(err)
	}
	if f.added != 1 || again.login[deviceField] != first.login[deviceField] || again.cfg.PrivateKey != first.cfg.PrivateKey {
		t.Errorf("%d devices added, device %s then %s", f.added, first.login[deviceField], again.login[deviceField])
	}

	delete(f.devices, first.login[deviceField])
	third, err := openMullvad(ctx, again.login)
	if err != nil {
		t.Fatal(err)
	}
	if f.added != 2 || third.login[deviceField] == first.login[deviceField] {
		t.Errorf("a device removed in the account was not replaced: %d added", f.added)
	}

	other := Login{}
	for k, v := range third.login {
		other[k] = v
	}
	other["accountNumber"] = "8765432187654321"
	if _, err := openMullvad(ctx, other); codeOf(err) != CodeLoginRefused {
		t.Errorf("another account = %v", err)
	}
	if owned(other, accountNumber(other["accountNumber"]))[deviceField] != "" {
		t.Error("another account would take up this account's device")
	}
}

func TestMullvadFailuresCarryTheirCode(t *testing.T) {
	f := newFakeMullvad(t)
	ctx := context.Background()
	login := Login{"accountNumber": mullvadAccount}
	if _, err := openMullvad(ctx, Login{"accountNumber": "0000000000000000"}); codeOf(err) != CodeLoginRefused {
		t.Errorf("unknown account = %v", err)
	}
	if _, err := openMullvad(ctx, Login{"accountNumber": mullvadAccount, "location": "xx"}); codeOf(err) != CodeLocationUnknown {
		t.Errorf("unknown location = %v", err)
	}
	f.down = true
	_, err := openMullvad(ctx, Login{"accountNumber": mullvadAccount, "location": "at-vie"})
	if err == nil || final(codeOf(err)) {
		t.Errorf("a location whose relays are all down = %v, want an error worth trying again", err)
	}
	f.down = false
	f.max = 0
	if _, err := openMullvad(ctx, login); codeOf(err) != CodeDeviceLimit {
		t.Errorf("full device list = %v", err)
	}
	f.expiry = time.Now().Add(-time.Hour)
	if _, err := openMullvad(ctx, login); codeOf(err) != CodeAccountExpired {
		t.Errorf("expired account = %v", err)
	}
}

func TestMullvadConnectsThroughTheManagerAndForgetFreesTheDevice(t *testing.T) {
	srv := vpntest.Start(t)
	f := newFakeMullvad(t)
	f.relay, f.port, f.pub = "127.0.0.1", srv.Port, srv.Key.PublicKey().String()
	f.devices["kept"] = srv.ClientKey.PublicKey().String()
	login := owned(Login{"accountNumber": mullvadAccount}, mullvadAccount)
	login[keyField], login[deviceField] = srv.ClientKey.String(), "kept"
	store := &memStore{}
	h := newHarness(t, store, "")
	h.m.seal("mullvad", login)

	if _, err := h.m.Connect("mullvad", map[string]string{"accountNumber": mullvadAccount}); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "mullvad", "connected")
	if f.added != 0 {
		t.Errorf("%d devices added while one was kept", f.added)
	}
	if _, err := h.m.Forget("mullvad"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		gone := len(f.removed) == 1 && f.removed[0] == "kept"
		f.mu.Unlock()
		if gone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("forget removed %v", f.removed)
}

func TestAFullDeviceListStopsTheConnect(t *testing.T) {
	f := newFakeMullvad(t)
	f.max = 0
	h := newHarness(t, &memStore{}, "")
	h.m.Connect("mullvad", map[string]string{"accountNumber": mullvadAccount})
	if p := h.waitFor(t, "mullvad", "failed"); p.ErrorCode != CodeDeviceLimit {
		t.Errorf("failed with %q %s", p.ErrorCode, p.Error)
	}
}
