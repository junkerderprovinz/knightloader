package nordvpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// fakeNord is NordVPN's API: the credentials behind a token, the countries,
// and a recommendation per country or city.
type fakeNord struct {
	key, serverKey vpn.Key
	asked          string
}

func (f *fakeNord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/users/services/credentials":
		user, pass, ok := r.BasicAuth()
		if !ok || user != "token" || pass != "nord-token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"errors":{"message":"Unauthorized","code":101301}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"username": "u", "password": "p", "nordlynx_private_key": f.key.String()})
	case "/v1/servers/countries":
		fmt.Fprint(w, `[{"id":81,"name":"Germany","code":"DE","cities":[{"id":2215709,"identifier":"de_frankfurt","name":"Frankfurt","dns_name":"frankfurt"}]},
			{"id":14,"name":"Austria","code":"AT","cities":[{"id":448799,"identifier":"at_vienna","name":"Vienna","dns_name":"vienna"}]}]`)
	case "/v1/servers/recommendations":
		q := r.URL.Query()
		if q.Get("filters[servers_technologies][identifier]") != "wireguard_udp" {
			fmt.Fprint(w, `[]`)
			return
		}
		f.asked = q.Get("filters[country_id]") + "/" + q.Get("filters[country_city_id]")
		json.NewEncoder(w).Encode([]map[string]any{{
			"hostname": "de1607.nordvpn.com", "station": "187.40.49.24",
			"technologies": []map[string]any{
				{"identifier": "openvpn_udp", "metadata": []any{}},
				{"identifier": "wireguard_udp", "metadata": []map[string]string{{"name": "public_key", "value": f.serverKey.PublicKey().String()}}},
			},
		}})
	default:
		http.NotFound(w, r)
	}
}

func newFake(t *testing.T) (*fakeNord, *Client) {
	t.Helper()
	k, _ := vpn.GeneratePrivateKey()
	sk, _ := vpn.GeneratePrivateKey()
	f := &fakeNord{key: k, serverKey: sk}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &Client{Token: "nord-token", api: srv.URL, web: srv.Client()}
}

func TestConnectUsesTheAccountsKeyAndARecommendedServer(t *testing.T) {
	f, c := newFake(t)
	cfg, srv, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != f.key || cfg.Peer.PublicKey != f.serverKey.PublicKey() {
		t.Error("the keys are not the account's and the server's")
	}
	if cfg.Peer.Endpoint != "187.40.49.24:51820" || srv.Hostname != "de1607.nordvpn.com" {
		t.Errorf("endpoint %s on %s", cfg.Peer.Endpoint, srv.Hostname)
	}
	if cfg.Addresses[0] != netip.MustParsePrefix("10.5.0.2/32") || len(cfg.DNS) != 2 {
		t.Errorf("addresses %v, DNS %v", cfg.Addresses, cfg.DNS)
	}
	if f.asked != "/" {
		t.Errorf("no location asked for %q", f.asked)
	}
}

func TestALocationNarrowsTheRecommendation(t *testing.T) {
	f, c := newFake(t)
	for loc, want := range map[string]string{"at": "14/", "Germany": "81/", "frankfurt": "/2215709", "at_vienna": "/448799"} {
		c.Location = loc
		if _, err := c.Server(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.asked != want {
			t.Errorf("%q asked for %q, want %q", loc, f.asked, want)
		}
	}
	c.Location = "atlantis"
	if _, err := c.Server(context.Background()); !errors.Is(err, vpn.ErrUnknownLocation) {
		t.Errorf("an unknown location = %v, want ErrUnknownLocation", err)
	}
}

func TestAWrongTokenIsRefused(t *testing.T) {
	_, c := newFake(t)
	c.Token = "not-it"
	if _, _, err := c.Connect(context.Background()); !errors.Is(err, ErrRefused) {
		t.Errorf("connect = %v, want ErrRefused", err)
	}
}
