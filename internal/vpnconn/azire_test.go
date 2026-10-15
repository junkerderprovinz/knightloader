package vpnconn

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn/azire"
)

// fakeAzire is AzireVPN's API for the account knight/secret.
type fakeAzire struct {
	mu       sync.Mutex
	tokens   map[string]bool
	issued   int
	inactive bool
	max      int
	ips      map[string]string
	next     int
	added    int
	ports    map[string]int
	asked    int
}

func newFakeAzire(t *testing.T) *fakeAzire {
	f := &fakeAzire{tokens: map[string]bool{}, max: 10, ips: map[string]string{}, ports: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := azireClient
	azireClient = func() *azire.Client { return &azire.Client{API: srv.URL + "/v3"} }
	t.Cleanup(func() { azireClient = old })
	return f
}

func (f *fakeAzire) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(status int, msg string) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": msg})
	}
	ok := func(data any) { json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}) }
	ip := func(id string) map[string]any {
		return map[string]any{"id": id, "ipv4_address": "10.0.16.181", "ipv4_netmask": 32,
			"ipv6_address": "2a0e:1c80:1337:1:10:0:16:181", "ipv6_netmask": 128, "dns": []string{"10.0.0.1"},
			"keys": []map[string]any{{"key": f.ips[id], "created_at": 1}}}
	}
	path := strings.TrimPrefix(r.URL.Path, "/v3")
	switch {
	case path == "/tokens" && r.Method == http.MethodPost:
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "knight" || body["password"] != "secret" || body["device_name"] != azireDevice {
			fail(http.StatusUnauthorized, "Unauthenticated.")
			return
		}
		f.issued++
		t := fmt.Sprintf("tok-%d", f.issued)
		f.tokens[t] = true
		ok(map[string]any{"id": t, "key": t})
		return
	case path == "/locations":
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "locations": []map[string]any{
			{"name": "ar-bue", "iso": "ar", "pool": "ar-bue.azirevpn.net", "pubkey": "HjziGvLqDPeFpsmU6aOT6DIEUleQ2bkppPmWeAy8fT0="},
			{"name": "se-sto", "iso": "se", "pool": "se-sto.azirevpn.net", "pubkey": "VYRJwI6n2Rpvh/gmYnUoyMJQDrUSdxls0JX9/6JlOEw="},
		}})
		return
	}
	if !f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		fail(http.StatusUnauthorized, "Unauthenticated.")
		return
	}
	switch {
	case path == "/users/me":
		ok(map[string]any{"is_active": !f.inactive, "ips": map[string]int{"allocated": len(f.ips), "available": f.max}})
	case path == "/ips" && r.Method == http.MethodPost:
		if f.inactive || len(f.ips) >= f.max {
			fail(http.StatusUnprocessableEntity, "You cannot create more IPs.")
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		f.next++
		f.added++
		id := fmt.Sprintf("ip-%d", f.next)
		f.ips[id] = body["key"]
		json.NewEncoder(w).Encode(ip(id))
	case strings.HasPrefix(path, "/ips/"):
		id := strings.TrimPrefix(path, "/ips/")
		if _, ok := f.ips[id]; !ok {
			fail(http.StatusNotFound, "Not found.")
			return
		}
		if r.Method == http.MethodDelete {
			delete(f.ips, id)
			ok(nil)
			return
		}
		ok(ip(id))
	case path == "/tokens" && r.Method == http.MethodDelete:
		delete(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		ok(nil)
	case path == "/portforwardings" && r.Method == http.MethodGet:
		in := r.URL.Query().Get("internal_ipv4")
		ports := []map[string]any{}
		if p := f.ports[in]; p != 0 {
			ports = append(ports, map[string]any{"port": p})
		}
		ok(map[string]any{"internal_ipv4": in, "ports": ports})
	case path == "/portforwardings" && r.Method == http.MethodPost:
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		in, _ := body["internal_ipv4"].(string)
		f.asked++
		f.ports[in] = 58216
		ok(map[string]any{"internal_ipv4": in, "port": 58216})
	default:
		fail(http.StatusNotFound, "Not found.")
	}
}

func azireLogin() Login {
	return Login{"username": "knight", "password": "secret"}
}

func TestAzireKeepsItsTokenAndAddress(t *testing.T) {
	f := newFakeAzire(t)
	ctx := context.Background()
	first, err := openAzire(ctx, azireLogin())
	if err != nil {
		t.Fatal(err)
	}
	if first.cfg.Peer.Endpoint != "se-sto.azirevpn.net:51820" {
		t.Errorf("endpoint %s, want the default location", first.cfg.Peer.Endpoint)
	}
	again, err := openAzire(ctx, first.login)
	if err != nil {
		t.Fatal(err)
	}
	if f.issued != 1 || f.added != 1 || again.login[deviceField] != first.login[deviceField] {
		t.Errorf("%d tokens and %d addresses for two connections", f.issued, f.added)
	}

	delete(f.tokens, again.login[tokenField])
	moved := Login{}
	for k, v := range again.login {
		moved[k] = v
	}
	moved["location"] = "ar"
	third, err := openAzire(ctx, moved)
	if err != nil {
		t.Fatal(err)
	}
	if f.issued != 2 || f.added != 1 || third.cfg.Peer.Endpoint != "ar-bue.azirevpn.net:51820" {
		t.Errorf("after the token ran out: %d tokens, %d addresses, endpoint %s", f.issued, f.added, third.cfg.Peer.Endpoint)
	}

	var d net.Dialer
	for range 2 {
		if p, err := third.askPort(ctx, d.DialContext); err != nil || p != 58216 {
			t.Fatalf("port %d %v", p, err)
		}
	}
	if f.asked != 1 {
		t.Errorf("asked for a new port %d times, want once", f.asked)
	}

	forgetAzire(ctx, third.login)
	if len(f.ips) != 0 || f.tokens[third.login[tokenField]] {
		t.Errorf("after forget: addresses %v, token kept %v", f.ips, f.tokens[third.login[tokenField]])
	}
}

func TestAzireFailuresCarryTheirCode(t *testing.T) {
	f := newFakeAzire(t)
	ctx := context.Background()
	if _, err := openAzire(ctx, Login{"username": "knight", "password": "wrong"}); codeOf(err) != CodeLoginRefused {
		t.Errorf("wrong password = %v", err)
	}
	login := azireLogin()
	login["location"] = "xx"
	if _, err := openAzire(ctx, login); codeOf(err) != CodeLocationUnknown {
		t.Errorf("unknown location = %v", err)
	}
	f.max = 0
	if _, err := openAzire(ctx, azireLogin()); codeOf(err) != CodeDeviceLimit {
		t.Errorf("all addresses taken = %v", err)
	}
	f.inactive = true
	if _, err := openAzire(ctx, azireLogin()); codeOf(err) != CodeAccountExpired {
		t.Errorf("inactive account = %v", err)
	}
}
