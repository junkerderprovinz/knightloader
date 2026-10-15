package azire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{API: srv.URL + "/v3"}
}

const ipJSON = `{"id": "IJiaUdc2", "ipv4_address": "10.0.1.136", "ipv4_netmask": 32,
  "ipv6_address": "2a0e:1c80:1337:1:10:0:1:136", "ipv6_netmask": 128,
  "dns": ["10.0.0.1", "2a0e:1c80:1337:1:10::1"], "keys": [{"key": "%s", "created_at": 1691499313}]}`

func TestAnIPIsReadWrappedOrNot(t *testing.T) {
	key, _ := vpn.GeneratePrivateKey()
	pub := key.PublicKey().String()
	bodies := map[string]string{
		"/v3/ips":        fmt.Sprintf(ipJSON, pub),
		"/v3/ips/listed": `{"status": "success", "data": [` + fmt.Sprintf(ipJSON, pub) + `]}`,
		"/v3/ips/one":    `{"status": "success", "data": ` + fmt.Sprintf(ipJSON, pub) + `}`,
	}
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(bodies[r.URL.Path]))
	})
	ctx := context.Background()
	added, err := c.AddIP(ctx, "tok", key.PublicKey())
	if err != nil || added.ID != "IJiaUdc2" || !added.Has(key.PublicKey()) {
		t.Errorf("add = %+v %v", added, err)
	}
	for _, id := range []string{"listed", "one"} {
		if ip, err := c.IP(ctx, "tok", id); err != nil || ip.IPv4Address != "10.0.1.136" {
			t.Errorf("ip %s = %+v %v", id, ip, err)
		}
	}
}

func TestARefusedLoginIsA401(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthenticated."}`))
	})
	_, err := c.Token(context.Background(), "knight", "wrong", "KnightLoader")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized || ae.Message != "Unauthenticated." {
		t.Errorf("token with a wrong password = %v", err)
	}
}

func TestTheConfigTakesTheIPToTheLocation(t *testing.T) {
	key, _ := vpn.GeneratePrivateKey()
	var ip IP
	json.Unmarshal([]byte(fmt.Sprintf(ipJSON, key.PublicKey().String())), &ip)
	locs := []Location{
		{Name: "ar-bue", ISO: "ar", Pool: "ar-bue.azirevpn.net", PublicKey: "HjziGvLqDPeFpsmU6aOT6DIEUleQ2bkppPmWeAy8fT0="},
		{Name: "se-sto", ISO: "se", Pool: "se-sto.azirevpn.net", PublicKey: "VYRJwI6n2Rpvh/gmYnUoyMJQDrUSdxls0JX9/6JlOEw="},
	}
	l, err := PickLocation(locs, "SE")
	if err != nil || l.Name != "se-sto" {
		t.Fatalf("location for se = %v %v", l, err)
	}
	if _, err := PickLocation(locs, "xx"); !errors.Is(err, ErrNoLocation) {
		t.Errorf("location for xx = %v", err)
	}
	cfg, err := Config(key, ip, l)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Peer.Endpoint != "se-sto.azirevpn.net:51820" || len(cfg.Addresses) != 2 || len(cfg.DNS) != 2 {
		t.Errorf("config %+v", cfg)
	}
}

func TestForwardPortTakesTheOneThereOrAsksForOne(t *testing.T) {
	ports := []int{}
	posted := 0
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("internal_ipv4") != "10.0.1.136" {
				t.Errorf("asked for %q", r.URL.RawQuery)
			}
			list := []map[string]any{}
			for _, p := range ports {
				list = append(list, map[string]any{"port": p, "hidden": false, "expires_at": 1})
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"ports": list}})
		case http.MethodPost:
			posted++
			ports = append(ports, 58216)
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"port": 58216}})
		}
	})
	var d net.Dialer
	for range 2 {
		p, err := c.ForwardPort(context.Background(), "tok", netip.MustParseAddr("10.0.1.136"), d.DialContext)
		if err != nil || p != 58216 {
			t.Fatalf("port %d %v", p, err)
		}
	}
	if posted != 1 {
		t.Errorf("asked for a new port %d times, want once", posted)
	}
}
