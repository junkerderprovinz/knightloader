package hideme

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// fakeHide is a hide.me server: its REST API on port 432 and the control
// endpoint inside the tunnel, behind a certificate of a CA of its own.
type fakeHide struct {
	t         *testing.T
	token     []byte
	serverKey vpn.Key
	posted    []byte
	ended     []byte
}

func (f *fakeHide) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	b64 := func(k string) []byte {
		s, _ := body[k].(string)
		b, _ := base64.StdEncoding.DecodeString(s)
		return b
	}
	switch r.URL.Path {
	case "/v1.0.0/accessToken":
		if body["username"] != "knight" || body["password"] != "secret" || body["domain"] != "hide.me" || body["host"] != "nl" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(base64.StdEncoding.EncodeToString(f.token))
	case "/v1.0.0/connect":
		if !bytes.Equal(b64("accessToken"), f.token) || body["host"] != "nl" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.posted = b64("publicKey")
		pub := f.serverKey.PublicKey()
		json.NewEncoder(w).Encode(map[string]any{
			"publicKey":           pub[:],
			"endpoint":            map[string]any{"IP": "185.1.2.3", "Port": 432, "Zone": ""},
			"presharedKey":        bytes.Repeat([]byte{7}, 32),
			"persistentKeepalive": int64(20 * time.Second),
			"allowedIps":          []string{"10.128.0.5"},
			"DNS":                 []string{"10.128.0.1"},
			"gateway":             []string{"10.128.0.1"},
			"sessionToken":        []byte("session-1"),
		})
	case "/v1.0.0/disconnect":
		f.ended = b64("sessionToken")
		w.Write([]byte("true"))
	case "/upnp":
		if body["enabled"] != true || !bytes.Equal(b64("accessToken"), f.token) {
			w.Write([]byte("false"))
			return
		}
		w.Write([]byte("true"))
	default:
		http.NotFound(w, r)
	}
}

// testCA makes a CA and a certificate for hideservers.net that it signed.
func testCA(t *testing.T) (*x509.Certificate, tls.Certificate) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Hide CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "hideservers.net"},
		DNSNames:  []string{"hideservers.net"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return ca, tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}
}

func pinOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func newFake(t *testing.T) (*fakeHide, *Client, func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	sk, _ := vpn.GeneratePrivateKey()
	f := &fakeHide{t: t, token: []byte("access-token"), serverKey: sk}
	ca, cert := testCA(t)
	srv := httptest.NewUnstartedServer(f)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	to := srv.Listener.Addr().String()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, to)
	}
	c := &Client{
		Host: "nl.hideservers.net", Username: "knight", Password: "secret",
		LookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("185.1.2.3")}, nil
		},
		roots: roots,
		pins:  map[string]string{"Test Hide CA": pinOf(ca)},
		dial:  dial,
	}
	return f, c, dial
}

func TestConnectTradesThePasswordForATunnel(t *testing.T) {
	f, c, _ := newFake(t)
	cfg, s, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.AccessToken, f.token) {
		t.Errorf("access token %q, want %q", c.AccessToken, f.token)
	}
	pub := cfg.PrivateKey.PublicKey()
	if !bytes.Equal(f.posted, pub[:]) {
		t.Error("the server got another public key than the config's private key has")
	}
	if cfg.Peer.PublicKey != f.serverKey.PublicKey() || cfg.Peer.Endpoint != "185.1.2.3:432" ||
		cfg.Peer.Keepalive != 20*time.Second || cfg.MTU != 1392 || cfg.Peer.PresharedKey[0] != 7 {
		t.Errorf("peer %+v, MTU %d", cfg.Peer, cfg.MTU)
	}
	if len(cfg.Addresses) != 1 || cfg.Addresses[0] != netip.MustParsePrefix("10.128.0.5/32") ||
		len(cfg.DNS) != 1 || cfg.Peer.AllowedIPs[0] != netip.MustParsePrefix("0.0.0.0/0") {
		t.Errorf("addresses %v, DNS %v, allowed %v", cfg.Addresses, cfg.DNS, cfg.Peer.AllowedIPs)
	}
	if string(s.Token) != "session-1" || len(s.Gateway) != 1 || s.Gateway[0] != netip.MustParseAddr("10.128.0.1") {
		t.Errorf("session %+v", s)
	}

	if err := c.Disconnect(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if string(f.ended) != "session-1" {
		t.Errorf("disconnect ended %q", f.ended)
	}
}

func TestPortForwardingIsAskedForInsideTheTunnel(t *testing.T) {
	_, c, dial := newFake(t)
	if err := c.FetchAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	var asked string
	through := func(ctx context.Context, network, addr string) (net.Conn, error) {
		asked = addr
		return dial(ctx, network, addr)
	}
	if err := c.EnablePortForwarding(context.Background(), through); err != nil {
		t.Fatal(err)
	}
	if asked != "10.255.255.250:4321" {
		t.Errorf("dialled %q, want the control address inside the tunnel", asked)
	}
}

func TestAServerOutsideThePinnedCAsIsRefused(t *testing.T) {
	_, c, _ := newFake(t)
	c.pins = caPins
	if err := c.FetchAccessToken(context.Background()); err == nil {
		t.Fatal("a server whose CA hide.me does not pin was trusted")
	}
}

func TestTheEmbeddedRootIsTheOneHideMePins(t *testing.T) {
	block, _ := pem.Decode(rootPEM)
	if block == nil {
		t.Fatal("ca.pem holds no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if pin := pinOf(cert); caPins[cert.Subject.CommonName] != pin {
		t.Errorf("%s has pin %s, which caPins does not list", cert.Subject.CommonName, pin)
	}
}
