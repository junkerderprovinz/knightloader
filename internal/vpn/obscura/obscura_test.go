package obscura

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{API: srv.URL + "/api/"}
}

func TestTheTunnelListKeepsThePlainWireGuardOnes(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s %s with %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`[
		  {"id": "a", "config": {"type": "obfuscated", "client_pubkey": "wjaiHUEOJ8k3X+U3b6H6yTcipqFipIbFQSB0CwZDNlQ="}, "exit": {"id": "x"}},
		  {"id": "b", "config": {"type": "udp_port",
		    "client": {"wg_pubkey": "wjaiHUEOJ8k3X+U3b6H6yTcipqFipIbFQSB0CwZDNlQ=", "addresses": ["10.150.177.7/32", "fc00:bbbb:bbbb:bb01:d:0:16:b107/128"]},
		    "server": {"wg_pubkey": "4s9JIhxC/D02tosXYYcgrD+pHI+C7oTAFsXzVisKjRs=", "endpoints": ["[2001:db8::1]:51820", "198.51.100.3:51820"], "dnses": ["10.64.0.1"]}},
		   "exit": {"id": "mullvad-se-got-wg-001", "country_code": "se", "city_code": "got", "provider_id": "se-got-wg-001"}}
		]`))
	})
	tunnels, err := c.Tunnels(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) != 1 || tunnels[0].ID != "b" {
		t.Fatalf("tunnels %+v", tunnels)
	}
	cfg := tunnels[0].Config
	cfg.PrivateKey, _ = vpn.GeneratePrivateKey()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Peer.Endpoint != "198.51.100.3:51820" || len(cfg.Addresses) != 2 || len(cfg.Peer.AllowedIPs) != 2 || len(cfg.DNS) != 1 {
		t.Errorf("config %+v", cfg)
	}
}

func TestAPIErrorsCarryTheirKind(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"InvalidAccountId":{}},"msg":"Malformed account ID"}`))
	})
	_, err := c.Token(context.Background(), "nope")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Kind != InvalidAccountID || ae.Msg != "Malformed account ID" {
		t.Errorf("token for a wrong account = %v", err)
	}
}

func TestPickExitFollowsTheLocation(t *testing.T) {
	exits := []Exit{
		{ID: "mullvad-se-got-wg-001", CountryCode: "se", CityCode: "got", ProviderID: "se-got-wg-001"},
		{ID: "mullvad-at-vie-wg-001", CountryCode: "at", CityCode: "vie", ProviderID: "at-vie-wg-001"},
	}
	for loc, want := range map[string]string{
		"at": "mullvad-at-vie-wg-001", "SE-GOT": "mullvad-se-got-wg-001",
		"at-vie-wg-001": "mullvad-at-vie-wg-001", "mullvad-se-got-wg-001": "mullvad-se-got-wg-001",
	} {
		if e, err := PickExit(exits, loc); err != nil || e.ID != want {
			t.Errorf("exit for %q = %s %v, want %s", loc, e.ID, err, want)
		}
	}
	if _, err := PickExit(exits, "us"); !errors.Is(err, ErrNoExit) {
		t.Errorf("exit for a country without one = %v", err)
	}
}
