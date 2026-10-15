package pia

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
)

const regionsJSON = `{"groups":{},"regions":[
{"id":"de_berlin","name":"DE Berlin","country":"DE","port_forward":true,"offline":false,
 "servers":{"wg":[{"ip":"194.5.49.113","cn":"Server-1"}],"meta":[{"ip":"194.5.49.129","cn":"Server-1"}]}},
{"id":"nl_amsterdam","name":"Netherlands","country":"NL","port_forward":true,"offline":false,
 "servers":{"wg":[{"ip":"212.102.35.1","cn":"Server-1"}],"meta":[{"ip":"212.102.35.2","cn":"Server-1"}]}},
{"id":"us_east","name":"US East","country":"US","port_forward":false,"offline":false,
 "servers":{"wg":[{"ip":"198.51.100.1","cn":"Server-1"}],"meta":[{"ip":"198.51.100.2","cn":"Server-1"}]}},
{"id":"dz","name":"Algeria","country":"DZ","port_forward":true,"geo":true,"offline":false,
 "servers":{"wg":[{"ip":"203.0.113.1","cn":"Server-1"}],"meta":[{"ip":"203.0.113.2","cn":"Server-1"}]}},
{"id":"fr","name":"France","country":"FR","port_forward":true,"offline":true,
 "servers":{"wg":[{"ip":"192.0.2.9","cn":"Server-1"}],"meta":[{"ip":"192.0.2.10","cn":"Server-1"}]}}
]}`

// fakePIA is PIA's web API and one WireGuard server's API.
type fakePIA struct {
	serverKey vpn.Key

	mu     sync.Mutex
	posted string
	binds  int
	tokens []string
	// fail answers the bindPort calls it numbers, counted from 1, with a
	// server that is down or a payload it rejects.
	fail  map[int]string
	calls int
}

func (f *fakePIA) web(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/token":
		r.ParseForm()
		if r.PostForm.Get("username") != "p1234567" || r.PostForm.Get("password") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "HTTP Token: Access denied.")
			return
		}
		fmt.Fprint(w, `{"token":"tok-1"}`)
	case "/servers":
		// One line of JSON, then its signature.
		fmt.Fprint(w, strings.ReplaceAll(regionsJSON, "\n", "")+"\nc2lnbmF0dXJl")
	default:
		http.NotFound(w, r)
	}
}

func (f *fakePIA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/addKey":
		if q.Get("pt") != "tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"status":"ERROR","message":"Login failed!"}`)
			return
		}
		f.posted = q.Get("pubkey")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "OK", "server_key": f.serverKey.PublicKey().String(), "server_port": 1337,
			"server_ip": "194.5.49.113", "server_vip": "10.6.128.1", "peer_ip": "10.6.140.7",
			"peer_pubkey": q.Get("pubkey"), "dns_servers": []string{"10.0.0.243", "10.0.0.242"},
		})
	case "/getSignature":
		f.tokens = append(f.tokens, q.Get("token"))
		payload := base64.StdEncoding.EncodeToString([]byte(`{"token":"x","port":47123,"expires_at":"2026-12-24T00:00:00Z"}`))
		json.NewEncoder(w).Encode(map[string]string{"status": "OK", "payload": payload, "signature": "sig"})
	case "/bindPort":
		f.calls++
		switch f.fail[f.calls] {
		case "down":
			w.WriteHeader(http.StatusBadGateway)
			return
		case "rejected":
			json.NewEncoder(w).Encode(map[string]string{"status": "ERROR"})
			return
		}
		if q.Get("signature") != "sig" || q.Get("payload") == "" {
			json.NewEncoder(w).Encode(map[string]string{"status": "ERROR"})
			return
		}
		f.binds++
		json.NewEncoder(w).Encode(map[string]string{"status": "OK", "message": "port scheduled for add"})
	default:
		http.NotFound(w, r)
	}
}

func newFake(t *testing.T) (*fakePIA, *Client, func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	sk, _ := vpn.GeneratePrivateKey()
	f := &fakePIA{serverKey: sk}
	web := httptest.NewServer(http.HandlerFunc(f.web))
	t.Cleanup(web.Close)
	roots, dial := vpntest.TLSServer(t, f, "Server-1")
	c := &Client{
		Username: "p1234567", Password: "secret", Location: "de",
		tokenURL: web.URL + "/token", listURL: web.URL + "/servers", web: web.Client(),
		roots: roots, dial: dial,
	}
	return f, c, dial
}

func TestConnectRegistersAFreshKeyWithTheRegionsServer(t *testing.T) {
	f, c, _ := newFake(t)
	cfg, s, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "tok-1" {
		t.Errorf("token %q", c.Token)
	}
	if f.posted != cfg.PrivateKey.PublicKey().String() {
		t.Error("the server got another public key than the config's private key has")
	}
	if cfg.Peer.PublicKey != f.serverKey.PublicKey() || cfg.Peer.Endpoint != "194.5.49.113:1337" {
		t.Errorf("peer %+v", cfg.Peer)
	}
	if len(cfg.Addresses) != 1 || cfg.Addresses[0] != netip.MustParsePrefix("10.6.140.7/32") || len(cfg.DNS) != 2 {
		t.Errorf("addresses %v, DNS %v", cfg.Addresses, cfg.DNS)
	}
	if s.Region.ID != "de_berlin" || s.Gateway != netip.MustParseAddr("10.6.128.1") || s.Server.CN != "Server-1" {
		t.Errorf("session %+v", s)
	}
}

func TestAWrongPasswordIsRefused(t *testing.T) {
	_, c, _ := newFake(t)
	c.Password = "wrong"
	if _, _, err := c.Connect(context.Background()); !errors.Is(err, ErrRefused) {
		t.Errorf("connect = %v, want ErrRefused", err)
	}
}

func TestAStaleTokenIsRefused(t *testing.T) {
	_, c, _ := newFake(t)
	c.Token = "yesterday"
	if _, _, err := c.Connect(context.Background()); !errors.Is(err, ErrRefused) {
		t.Errorf("connect = %v, want ErrRefused", err)
	}
}

func TestLocations(t *testing.T) {
	_, c, _ := newFake(t)
	regions, err := c.Regions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for loc, want := range map[string]string{"nl": "nl_amsterdam", "DE Berlin": "de_berlin", "us_east": "us_east"} {
		c.Location = loc
		r, err := c.pick(context.Background(), regions)
		if err != nil || r.ID != want {
			t.Errorf("%q picked %q, %v; want %s", loc, r.ID, err, want)
		}
	}
	for _, loc := range []string{"fr", "atlantis"} {
		c.Location = loc
		if _, err := c.pick(context.Background(), regions); !errors.Is(err, vpn.ErrUnknownLocation) {
			t.Errorf("%q = %v, want ErrUnknownLocation", loc, err)
		}
	}
}

func TestWithoutALocationTheFastestForwardingRegionWins(t *testing.T) {
	_, c, _ := newFake(t)
	c.Location = ""
	took := map[string]time.Duration{"194.5.49.129": 40 * time.Millisecond, "212.102.35.2": 10 * time.Millisecond, "198.51.100.2": time.Millisecond, "203.0.113.2": time.Millisecond}
	c.ping = func(_ context.Context, ip netip.Addr) (time.Duration, error) {
		d, ok := took[ip.String()]
		if !ok {
			return 0, errors.New("no answer")
		}
		return d, nil
	}
	regions, _ := c.Regions(context.Background())
	r, err := c.pick(context.Background(), regions)
	if err != nil || r.ID != "nl_amsterdam" {
		t.Errorf("picked %q, %v; want the fastest region that forwards ports and is no geo region", r.ID, err)
	}
}

func TestTheForwardedPortIsBoundAgainUntilTheTunnelEnds(t *testing.T) {
	defer func(d time.Duration) { BindEvery = d }(BindEvery)
	BindEvery = 10 * time.Millisecond
	f, c, dial := newFake(t)
	_, s, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var dialled []string
	through := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialled = append(dialled, addr)
		mu.Unlock()
		if strings.HasPrefix(addr, "10.6.128.1:") {
			return nil, errors.New("no route")
		}
		return dial(ctx, network, addr)
	}
	ports := make(chan uint16, 100)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.KeepPort(ctx, s, through, func(p uint16) { ports <- p }, func(err error) { t.Errorf("a call failed: %v", err) })
	}()
	for range 3 {
		select {
		case p := <-ports:
			if p != 47123 {
				t.Fatalf("port %d, want the payload's 47123", p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the port was not bound three times")
		}
	}
	cancel()
	<-done
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tokens) != 1 || f.tokens[0] != "tok-1" || f.binds < 3 {
		t.Errorf("signatures for %v, %d binds", f.tokens, f.binds)
	}
	mu.Lock()
	defer mu.Unlock()
	if dialled[0] != "10.6.128.1:19999" || dialled[1] != "194.5.49.113:19999" {
		t.Errorf("dialled %v, want the gateway first and the server after it", dialled)
	}
}

func TestAFailedBindDropsThePortUntilABindWorksAgain(t *testing.T) {
	defer func(b, r time.Duration) { BindEvery, RetryEvery = b, r }(BindEvery, RetryEvery)
	BindEvery, RetryEvery = 10*time.Millisecond, 10*time.Millisecond
	f, c, dial := newFake(t)
	_, s, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.fail = map[int]string{2: "down", 4: "rejected"}
	f.mu.Unlock()
	ports := make(chan uint16, 100)
	var failures sync.WaitGroup
	failures.Add(2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.KeepPort(ctx, s, dial, func(p uint16) { ports <- p }, func(error) { failures.Done() })
	}()
	var got []uint16
	for len(got) < 6 {
		select {
		case p := <-ports:
			got = append(got, p)
		case <-time.After(5 * time.Second):
			t.Fatalf("ports %v, and no more", got)
		}
	}
	cancel()
	<-done
	failures.Wait()
	if want := []uint16{47123, 0, 47123, 0, 47123, 47123}; !slices.Equal(got, want) {
		t.Errorf("ports %v, want %v", got, want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tokens) != 2 {
		t.Errorf("%d signatures, want a new one only after the rejected bind", len(f.tokens))
	}
}

func TestARegionWithoutPortForwardingAsksForNone(t *testing.T) {
	_, c, _ := newFake(t)
	c.KeepPort(context.Background(), Session{Region: Region{ID: "us_east"}}, nil, func(uint16) {
		t.Error("a port was set")
	}, func(err error) { t.Error(err) })
}

func TestTheEmbeddedCAIsPIAs(t *testing.T) {
	block, _ := pem.Decode(caPEM)
	if block == nil {
		t.Fatal("ca.pem holds no certificate")
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.IsCA || ca.Subject.CommonName != "Private Internet Access" {
		t.Errorf("ca.pem is %q", ca.Subject.CommonName)
	}
}
