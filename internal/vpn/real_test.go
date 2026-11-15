package vpn_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/hideme"
)

func publicIP(t *testing.T, dial func(ctx context.Context, network, addr string) (net.Conn, error)) string {
	t.Helper()
	tr := &http.Transport{DialContext: dial}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	resp, err := c.Get("https://api.ipify.org")
	if err != nil {
		t.Fatalf("ipify: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// TestRealTunnel connects to the WireGuard server of the wg-quick file named
// by KL_VPN_REAL_CONFIG and checks what goes through it. KL_VPN_REAL_LAN is
// host:port of a service that answers with the address a connection came
// from, to see the tunnel leave elsewhere, and KL_VPN_REAL_GATEWAY adds a
// round of NAT-PMP with that gateway.
func TestRealTunnel(t *testing.T) {
	path := os.Getenv("KL_VPN_REAL_CONFIG")
	if path == "" {
		t.Skip("KL_VPN_REAL_CONFIG is not set")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vpn.ParseConfig(string(text))
	if err != nil {
		t.Fatal(err)
	}
	tun := vpn.New(vpn.Options{Logf: t.Logf})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tun.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer tun.Disconnect()
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	direct := publicIP(t, d.DialContext)
	tunneled := publicIP(t, tun.DialContext)
	t.Logf("public address directly %s, through the tunnel %s", direct, tunneled)
	if direct == tunneled && os.Getenv("KL_VPN_REAL_SAME_EXIT") == "" {
		t.Error("the tunnel leaves from the same public address")
	}

	if lan := os.Getenv("KL_VPN_REAL_LAN"); lan != "" {
		seenFrom(t, tun, lan)
	}

	if gw := os.Getenv("KL_VPN_REAL_GATEWAY"); gw != "" {
		pf := &vpn.PortForward{Client: &vpn.NATPMP{Gateway: netip.MustParseAddr(gw), Dial: tun.DialContext}, Logf: t.Logf}
		pctx, pcancel := context.WithTimeout(context.Background(), 20*time.Second)
		go pf.Run(pctx)
		for pf.Port() == 0 && pctx.Err() == nil {
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("forwarded port %d", pf.Port())
		pcancel()
	}
}

// seenFrom reads what the service at addr, which answers with the address a
// connection came from, says of a direct connection and of one through the
// tunnel.
func seenFrom(t *testing.T, tun *vpn.Tunnel, addr string) {
	t.Helper()
	read := func(dial func(ctx context.Context, network, addr string) (net.Conn, error)) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := dial(ctx, "tcp4", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		defer c.Close()
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		b, _ := io.ReadAll(c)
		return strings.TrimSpace(string(b))
	}
	var d net.Dialer
	direct, tunneled := read(d.DialContext), read(tun.DialContext)
	t.Logf("%s saw a direct connection come from %s and one through the tunnel from %s", addr, direct, tunneled)
	if direct == tunneled {
		t.Error("the tunnel's connection came from this machine")
	}
}

// TestRealHideMe opens a hide.me session with the account in the JSON file
// KL_VPN_HIDEME_CRED ({"u": user, "p": password}) at the server
// KL_VPN_HIDEME_HOST, nl when unset, and runs what TestRealTunnel runs
// through it, with hide.me's port forwarding.
func TestRealHideMe(t *testing.T) {
	path := os.Getenv("KL_VPN_HIDEME_CRED")
	if path == "" {
		t.Skip("KL_VPN_HIDEME_CRED is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cred struct{ U, P string }
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("KL_VPN_HIDEME_HOST")
	if host == "" {
		host = "nl"
	}
	hm := &hideme.Client{Host: host, Username: cred.U, Password: cred.P}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, session, err := hm.Connect(ctx)
	if err != nil {
		t.Fatalf("hide.me connect: %v", err)
	}
	defer hm.Disconnect(context.Background(), session)
	t.Logf("session at %s, addresses %v, gateway %v, keepalive %s, stale token %v",
		cfg.Peer.Endpoint, cfg.Addresses, session.Gateway, cfg.Peer.Keepalive, session.StaleAccessToken)

	tun := vpn.New(vpn.Options{Logf: t.Logf})
	if err := tun.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer tun.Disconnect()
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	direct := publicIP(t, d.DialContext)
	tunneled := publicIP(t, tun.DialContext)
	t.Logf("public address directly %s, through hide.me %s", direct, tunneled)
	if direct == tunneled {
		t.Error("the tunnel leaves from the same public address")
	}

	var forwarded uint16
	if err := hm.EnablePortForwarding(ctx, tun.DialContext); err != nil {
		t.Logf("hide.me port forwarding: %v", err)
	} else if len(session.Gateway) > 0 {
		pf := &vpn.PortForward{Client: &vpn.NATPMP{Gateway: session.Gateway[0], Dial: tun.DialContext}, Prefer: 51413, Logf: t.Logf}
		pctx, pcancel := context.WithCancel(context.Background())
		defer pcancel()
		go pf.Run(pctx)
		for i := 0; pf.Port() == 0 && i < 100; i++ {
			time.Sleep(200 * time.Millisecond)
		}
		forwarded = pf.Port()
		t.Logf("hide.me forwarded port %d", forwarded)
		if forwarded != 0 {
			a, err := pf.Client.ExternalAddress(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("gateway's public address %s", a)
			inbound(t, tun, cfg.Addresses[0].Addr(), forwarded)
		}
	}
}

// inbound listens on the forwarded port inside the tunnel for a while and
// counts the connections that arrive there from outside.
func inbound(t *testing.T, tun *vpn.Tunnel, inside netip.Addr, port uint16) {
	t.Helper()
	ln, err := tun.Listen("tcp4", netip.AddrPortFrom(inside, port))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	from := map[string]bool{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			mu.Lock()
			from[host] = true
			mu.Unlock()
			c.Close()
		}
	}()
	time.Sleep(15 * time.Second)
	ln.Close()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("%d hosts reached port %d inside the tunnel from outside in 15 s", len(from), port)
}
