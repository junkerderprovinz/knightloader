package mullvad

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

const relayList = `{
  "locations": {
    "se-got": {"country": "Sweden", "city": "Gothenburg", "latitude": 57.7, "longitude": 11.97},
    "se-sto": {"country": "Sweden", "city": "Stockholm", "latitude": 59.33, "longitude": 18.06},
    "at-vie": {"country": "Austria", "city": "Vienna", "latitude": 48.21, "longitude": 16.37}
  },
  "wireguard": {
    "port_ranges": [[53, 53], [4000, 33433], [33565, 51820]],
    "ipv4_gateway": "10.64.0.1",
    "ipv6_gateway": "fc00:bbbb:bbbb:bb01::1",
    "relays": [
      {"hostname": "se-got-wg-001", "location": "se-got", "active": true, "ipv4_addr_in": "185.213.154.66", "weight": 100, "include_in_country": true, "public_key": "5JMPeO7gXIbR5CnUa/NPNK4L5GqUnreF0/Bozai4pl4="},
      {"hostname": "se-sto-wg-001", "location": "se-sto", "active": true, "ipv4_addr_in": "193.138.218.220", "weight": 100, "include_in_country": false, "public_key": "lCh7MRqSnHzOHnGGKKbhKfI7bDLTfiOzhfuE0Lq4qAI="},
      {"hostname": "at-vie-wg-001", "location": "at-vie", "active": true, "ipv4_addr_in": "146.70.116.98", "weight": 100, "include_in_country": true, "public_key": "TNrdH73p6h2EfeXxUiLOCOWHcjmjoslLxZptZpIPQXU="},
      {"hostname": "at-vie-wg-002", "location": "at-vie", "active": false, "ipv4_addr_in": "146.70.116.130", "weight": 100, "include_in_country": true, "public_key": "ehXBc726YX1N6Dm7fDAVMG5cIaYAFqCA4Lbpl4VWcWE="}
    ]
  }
}`

func relays(t *testing.T) Relays {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public/relays/wireguard/v2" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(relayList))
	}))
	defer srv.Close()
	l, err := (&Client{API: srv.URL}).Relays(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestTheRelayListKeepsTheRelaysThatAreUp(t *testing.T) {
	l := relays(t)
	if len(l.Relays) != 3 {
		t.Fatalf("%d relays, want the 3 that are up", len(l.Relays))
	}
	if l.Gateway != netip.MustParseAddr("10.64.0.1") {
		t.Errorf("gateway %v", l.Gateway)
	}
}

func TestPickFollowsTheLocation(t *testing.T) {
	l := relays(t)
	vienna := Place{48.2, 16.4}
	cases := []struct {
		location string
		near     *Place
		want     string
	}{
		{"at", nil, "at-vie-wg-001"},
		{"SE-STO", nil, "se-sto-wg-001"},
		{"se-sto-wg-001", nil, "se-sto-wg-001"},
		{"se", nil, "se-got-wg-001"},
		{"", &vienna, "at-vie-wg-001"},
	}
	for _, c := range cases {
		for range 20 {
			r, err := l.Pick(c.location, c.near)
			if err != nil || r.Hostname != c.want {
				t.Fatalf("pick %q = %s %v, want %s", c.location, r.Hostname, err, c.want)
			}
		}
	}
	if _, err := l.Pick("xx", nil); !errors.Is(err, ErrNoRelay) {
		t.Errorf("pick of a country without relays = %v", err)
	}
	if _, err := l.Pick("at-vie-wg-002", nil); !errors.Is(err, ErrNoRelayUp) {
		t.Errorf("pick of a relay that is down = %v, want ErrNoRelayUp", err)
	}
	if r, err := l.Pick("", nil); err != nil || r.Hostname == "" {
		t.Errorf("pick of anywhere = %v %v", r, err)
	}
}

func TestTheConfigTakesTheDeviceToTheRelay(t *testing.T) {
	l := relays(t)
	key, _ := vpn.GeneratePrivateKey()
	r, _ := l.Pick("at-vie", nil)
	dev := Device{ID: "d", PublicKey: key.PublicKey(),
		IPv4: netip.MustParsePrefix("10.130.1.2/32"), IPv6: netip.MustParsePrefix("fc00:bbbb:bbbb:bb01::1:2/128")}
	cfg, err := l.Config(key, dev, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Peer.Endpoint != "146.70.116.98:51820" || cfg.Peer.PublicKey != r.PublicKey {
		t.Errorf("peer %s %s", cfg.Peer.Endpoint, cfg.Peer.PublicKey)
	}
	if len(cfg.Addresses) != 2 || len(cfg.Peer.AllowedIPs) != 2 || len(cfg.DNS) != 1 || cfg.DNS[0] != l.Gateway {
		t.Errorf("addresses %v, allowed %v, DNS %v", cfg.Addresses, cfg.Peer.AllowedIPs, cfg.DNS)
	}
}

func TestAPIErrorsCarryMullvadsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"INVALID_ACCOUNT","error":"Bad account number"}`))
	}))
	defer srv.Close()
	_, err := (&Client{API: srv.URL}).Token(context.Background(), "1234")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != InvalidAccount || ae.Status != http.StatusBadRequest {
		t.Errorf("token for a wrong account = %v", err)
	}
}
