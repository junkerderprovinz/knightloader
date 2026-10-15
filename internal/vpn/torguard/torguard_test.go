package torguard

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
)

// fakeTorGuard is a TorGuard server's setup API.
type fakeTorGuard struct {
	serverKey vpn.Key

	mu   sync.Mutex
	keys []string
}

func (f *fakeTorGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if r.URL.Path != "/api/v1/setup" || !ok || user != "knight" || pass != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":401,"error":"Unauthorized","message":"Unauthorized","path":"/api/v1/setup"}`))
		return
	}
	f.mu.Lock()
	f.keys = append(f.keys, r.URL.Query().Get("public-key"))
	f.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{
		"server_public_key": f.serverKey.PublicKey().String(), "server_ipv4": "10.14.0.1/16",
		"client_ipv4": "10.14.102.149/32", "routes": "0.0.0.0/0", "dns": []string{"10.9.0.1", "10.8.0.1"},
		"vpn_server_address": "95.174.67.242", "vpn_server_port": 1443,
	})
}

func newFake(t *testing.T) (*fakeTorGuard, *Client, *[]string) {
	t.Helper()
	sk, _ := vpn.GeneratePrivateKey()
	f := &fakeTorGuard{serverKey: sk}
	roots, dial := vpntest.TLSServer(t, f, "95.174.67.242")
	var dialled []string
	c := &Client{
		Username: "knight", Password: "secret", Host: "nl.secureconnect.me",
		LookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("95.174.67.242")}, nil
		},
		roots: roots,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialled = append(dialled, addr)
			return dial(ctx, network, addr)
		},
	}
	return f, c, &dialled
}

func TestConnectRegistersAFreshKeyWithTheServer(t *testing.T) {
	f, c, dialled := newFake(t)
	cfg, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.keys) != 1 || f.keys[0] != cfg.PrivateKey.PublicKey().String() {
		t.Errorf("the server got %v, not the config's public key", f.keys)
	}
	if cfg.Peer.PublicKey != f.serverKey.PublicKey() || cfg.Peer.Endpoint != "95.174.67.242:1443" ||
		cfg.Peer.AllowedIPs[0] != netip.MustParsePrefix("0.0.0.0/0") {
		t.Errorf("peer %+v", cfg.Peer)
	}
	if cfg.Addresses[0] != netip.MustParsePrefix("10.14.102.149/32") || len(cfg.DNS) != 2 {
		t.Errorf("addresses %v, DNS %v", cfg.Addresses, cfg.DNS)
	}
	if (*dialled)[0] != "95.174.67.242:1443" {
		t.Errorf("dialled %v, want the resolved address", *dialled)
	}

	if err := c.Renew(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if len(f.keys) != 2 || f.keys[1] != f.keys[0] {
		t.Errorf("a renewal sent %v, want the same key again", f.keys)
	}
}

func TestAWrongPasswordIsRefused(t *testing.T) {
	_, c, _ := newFake(t)
	c.Password = "wrong"
	if _, err := c.Connect(context.Background()); !errors.Is(err, ErrRefused) {
		t.Errorf("connect = %v, want ErrRefused", err)
	}
}

func TestACertificateFromAnotherCAIsRefused(t *testing.T) {
	_, c, _ := newFake(t)
	c.roots = nil
	if _, err := c.Connect(context.Background()); err == nil || errors.Is(err, ErrRefused) {
		t.Errorf("connect = %v, want a TLS failure", err)
	}
}

func TestTheEmbeddedCAIsTorGuards(t *testing.T) {
	block, _ := pem.Decode(caPEM)
	if block == nil {
		t.Fatal("ca.pem holds no certificate")
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.IsCA || ca.Subject.CommonName != "TorGuard VPN Server CA 2" {
		t.Errorf("ca.pem is %q", ca.Subject.CommonName)
	}
}
