package vpn

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
)

type pair struct {
	tun    *Tunnel
	srv    *testServer
	cfg    Config
	srvKey Key
}

func clientConfig(t *testing.T, srvKey Key, port uint16) Config {
	t.Helper()
	ck, err := GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		PrivateKey: ck,
		Addresses:  []netip.Prefix{netip.PrefixFrom(clientAddr, 32)},
		DNS:        []netip.Addr{serverAddr},
		Peer: Peer{
			PublicKey:  srvKey.PublicKey(),
			Endpoint:   "127.0.0.1:" + strconv.Itoa(int(port)),
			AllowedIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")},
		},
	}
}

func startTestServer(t *testing.T, key, peer Key, port uint16) *testServer {
	t.Helper()
	srv, err := startServer(serverOptions{
		key: key, peer: peer, listenPort: port,
		names: map[string]netip.Addr{"gateway.tunnel.test": serverAddr},
		logf:  t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.close)
	return srv
}

// connectedPair brings up a server and a tunnel to it and waits for the
// handshake.
func connectedPair(t *testing.T, opts Options) *pair {
	t.Helper()
	sk, err := GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	p := &pair{srvKey: sk}
	p.cfg = clientConfig(t, sk, 0)
	p.srv = startTestServer(t, sk, p.cfg.PrivateKey.PublicKey(), 0)
	p.cfg.Peer.Endpoint = "127.0.0.1:" + strconv.Itoa(int(p.srv.port))
	if opts.Poll == 0 {
		opts.Poll = 50 * time.Millisecond
	}
	opts.Logf = t.Logf
	p.tun = New(opts)
	t.Cleanup(p.tun.Disconnect)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.tun.Connect(ctx, p.cfg); err != nil {
		t.Fatal(err)
	}
	if err := p.tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	return p
}

func dialCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestTwoPeersMoveDataThroughTheTunnel(t *testing.T) {
	p := connectedPair(t, Options{})

	c, err := p.tun.DialContext(dialCtx(t), "tcp4", "10.2.0.1:7")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sent := make([]byte, 8<<20)
	rand.Read(sent)
	go func() { c.Write(sent) }()
	got := make([]byte, len(sent))
	c.SetReadDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("read back %v", err)
	}
	if !bytes.Equal(got, sent) {
		t.Fatal("the data came back changed")
	}

	u, err := p.tun.DialContext(dialCtx(t), "udp4", "10.2.0.1:7")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(5 * time.Second))
	u.Write([]byte("datagram"))
	buf := make([]byte, 16)
	if n, err := u.Read(buf); err != nil || string(buf[:n]) != "datagram" {
		t.Fatalf("udp echo = %q, %v", buf[:n], err)
	}
	if st := p.tun.State(); st.RxBytes < uint64(len(sent)) || st.TxBytes < uint64(len(sent)) {
		t.Errorf("state counts rx %d tx %d, want at least %d each", st.RxBytes, st.TxBytes, len(sent))
	}
}

func TestAPortInsideTheTunnelTakesConnectionsFromTheServerSide(t *testing.T) {
	p := connectedPair(t, Options{})
	ln, err := p.tun.Listen("tcp4", netip.AddrPortFrom(clientAddr, 51234))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()
	c, err := p.srv.dial(dialCtx(t), netip.AddrPortFrom(clientAddr, 51234))
	if err != nil {
		t.Fatalf("the server cannot reach the forwarded port: %v", err)
	}
	defer c.Close()
	select {
	case in := <-accepted:
		in.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("nothing accepted")
	}
}

func TestNamesResolveWithTheTunnelsNameServer(t *testing.T) {
	p := connectedPair(t, Options{})
	ips, err := p.tun.LookupNetIP(dialCtx(t), "ip4", "gateway.tunnel.test")
	if err != nil || len(ips) != 1 || ips[0] != serverAddr {
		t.Fatalf("LookupNetIP = %v, %v; want %v", ips, err, serverAddr)
	}
	c, err := p.tun.DialContext(dialCtx(t), "tcp4", "gateway.tunnel.test:7")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := p.tun.LookupNetIP(dialCtx(t), "ip4", "elsewhere.tunnel.test"); err == nil {
		t.Error("a name the tunnel's server does not know resolved")
	}
}

func TestPortForwardingRunsThroughTheTunnel(t *testing.T) {
	p := connectedPair(t, Options{})
	client := &NATPMP{Gateway: serverAddr, Dial: p.tun.DialContext}
	if a, err := client.ExternalAddress(dialCtx(t)); err != nil || a != p.srv.gw.public {
		t.Fatalf("ExternalAddress = %v, %v", a, err)
	}
	pf := &PortForward{Client: client}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pf.Run(ctx) }()
	waitUntil(t, "the forwarded port", 10*time.Second, func() bool { return pf.Port() == 51234 })
	cancel()
	<-done
}

// bindTorrents binds a Binder to the tunnel the way the torrent client's is.
func bindTorrents(t *testing.T, tun *Tunnel, name string) *netbind.Binder {
	t.Helper()
	if err := netbind.Register(name, tun); err != nil {
		t.Fatal(err)
	}
	b := netbind.New()
	b.SetInterface(name)
	tun.Watch(func(State) { b.Refresh() })
	t.Cleanup(func() {
		b.SetInterface("")
		netbind.Register(name, nil)
	})
	return b
}

func udpEcho(t *testing.T, pc net.PacketConn) error {
	t.Helper()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	defer pc.SetDeadline(time.Time{})
	if _, err := pc.WriteTo([]byte("dht"), net.UDPAddrFromAddrPort(netip.AddrPortFrom(serverAddr, 7))); err != nil {
		return err
	}
	buf := make([]byte, 8)
	n, _, err := pc.ReadFrom(buf)
	if err == nil && string(buf[:n]) != "dht" {
		err = errors.New("echo came back changed")
	}
	return err
}

func TestTheTorrentBinderSendsNothingOnceTheTunnelIsDown(t *testing.T) {
	p := connectedPair(t, Options{})
	b := bindTorrents(t, p.tun, "kl-vpn-killswitch")
	if st := b.State(); !st.Up || len(st.Addrs) != 1 || st.Addrs[0] != clientAddr {
		t.Fatalf("binder state %+v, want the tunnel's address", st)
	}

	open, err := b.DialContext(dialCtx(t), "tcp4", "10.2.0.1:7")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	pc, err := b.PacketConn("udp4", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if err := udpEcho(t, pc); err != nil {
		t.Fatalf("udp through the binder: %v", err)
	}

	// Loopback is one hop away on the system's network; through the tunnel
	// nothing answers there.
	local, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	short, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, err := b.DialContext(short, "tcp4", local.Addr().String()); err == nil {
		c.Close()
		t.Error("the binder reached a system socket past the tunnel")
	}

	p.tun.Disconnect()

	if _, err := b.DialContext(dialCtx(t), "tcp4", "10.2.0.1:7"); !errors.Is(err, netbind.ErrDown) {
		t.Errorf("dial with the tunnel down = %v, want ErrDown", err)
	}
	if _, err := pc.WriteTo([]byte("x"), net.UDPAddrFromAddrPort(netip.AddrPortFrom(serverAddr, 7))); !errors.Is(err, netbind.ErrDown) {
		t.Errorf("send with the tunnel down = %v, want ErrDown", err)
	}
	if _, err := b.LookupHost(dialCtx(t), "gateway.tunnel.test"); !errors.Is(err, netbind.ErrDown) {
		t.Errorf("lookup with the tunnel down = %v, want ErrDown", err)
	}
	open.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := open.Read(make([]byte, 1)); err == nil {
		t.Error("a connection from before is still open with the tunnel down")
	}
}

func TestATunnelReconnectsOnceItsServerIsBack(t *testing.T) {
	sk, _ := GeneratePrivateKey()
	first := startTestServer(t, sk, Key{}, 0)
	port := first.port
	first.close()

	cfg := clientConfig(t, sk, port)
	tun := New(Options{Poll: 50 * time.Millisecond, HandshakeTimeout: 500 * time.Millisecond, MaxBackoff: time.Second, Logf: t.Logf})
	t.Cleanup(tun.Disconnect)
	if err := tun.Connect(dialCtx(t), cfg); err != nil {
		t.Fatal(err)
	}
	b := bindTorrents(t, tun, "kl-vpn-reconnect")
	pc, err := b.PacketConn("udp4", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	waitUntil(t, "a reconnect attempt", 5*time.Second, func() bool { return tun.State().Reconnects > 0 })
	if st := tun.State(); st.Status != Connecting {
		t.Fatalf("status %s without a server, want connecting", st.Status)
	}
	if _, err := pc.WriteTo([]byte("x"), net.UDPAddrFromAddrPort(netip.AddrPortFrom(serverAddr, 7))); !errors.Is(err, netbind.ErrDown) {
		t.Errorf("send before the first handshake = %v, want ErrDown", err)
	}

	startTestServer(t, sk, cfg.PrivateKey.PublicKey(), port)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the binder to come up", 5*time.Second, func() bool { return b.State().Up })
	if err := udpEcho(t, pc); err != nil {
		t.Errorf("the socket from before did not come back: %v", err)
	}
}

func TestANewConnectRetriesOnTheHandshakeTimeoutAlone(t *testing.T) {
	sk, _ := GeneratePrivateKey()
	gone := startTestServer(t, sk, Key{}, 0)
	port := gone.port
	gone.close()

	cfg := clientConfig(t, sk, port)
	tun := New(Options{Poll: 20 * time.Millisecond, HandshakeTimeout: 300 * time.Millisecond, MaxBackoff: time.Minute, Logf: t.Logf})
	t.Cleanup(tun.Disconnect)
	if err := tun.Connect(dialCtx(t), cfg); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a few reconnects", 10*time.Second, func() bool { return tun.State().Reconnects >= 3 })
	if err := tun.Connect(dialCtx(t), cfg); err != nil {
		t.Fatal(err)
	}
	tun.mu.Lock()
	attempt, backoff := tun.attempt, tun.backoff
	tun.mu.Unlock()
	if _, due := tun.opts.judge(Connecting, time.Time{}, attempt, backoff, attempt.Add(tun.opts.HandshakeTimeout)); !due {
		t.Errorf("the first retry of a new connection waits %v past the handshake timeout", backoff)
	}
}

func TestAnEndpointThatDoesNotResolveSaysSo(t *testing.T) {
	sk, _ := GeneratePrivateKey()
	cfg := clientConfig(t, sk, 51820)
	cfg.Peer.Endpoint = "vpn.example.test:51820"
	tun := New(Options{Logf: t.Logf, LookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, &net.DNSError{Err: "server misbehaving", Name: "vpn.example.test", IsTemporary: true}
	}})
	t.Cleanup(tun.Disconnect)
	if err := tun.Connect(dialCtx(t), cfg); !errors.Is(err, ErrResolve) {
		t.Errorf("connect = %v, want ErrResolve", err)
	}
	if err := tun.Connect(dialCtx(t), Config{}); errors.Is(err, ErrResolve) {
		t.Errorf("a broken config failed as %v", err)
	}
}

func TestRefreshHandsTheReconnectANewServer(t *testing.T) {
	sk, _ := GeneratePrivateKey()
	gone := startTestServer(t, sk, Key{}, 0)
	deadPort := gone.port
	gone.close()

	cfg := clientConfig(t, sk, deadPort)
	live := startTestServer(t, sk, cfg.PrivateKey.PublicKey(), 0)
	refreshed := cfg
	refreshed.Peer.Endpoint = "127.0.0.1:" + strconv.Itoa(int(live.port))
	tun := New(Options{
		Poll: 50 * time.Millisecond, HandshakeTimeout: 300 * time.Millisecond, Logf: t.Logf,
		Refresh: func(context.Context) (Config, error) { return refreshed, nil },
	})
	t.Cleanup(tun.Disconnect)
	if err := tun.Connect(dialCtx(t), cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	if got := tun.State().Endpoint.Port(); got != live.port {
		t.Errorf("connected through port %d, want the refreshed %d", got, live.port)
	}
}

func TestAStaleHandshakeTurnsTheTunnelDown(t *testing.T) {
	o := Options{}.withDefaults()
	now := time.Now()
	built := now.Add(-time.Hour)
	cases := []struct {
		name      string
		status    Status
		handshake time.Time
		attempt   time.Time
		want      Status
		due       bool
	}{
		{"first handshake", Connecting, now.Add(-time.Second), built, Connected, false},
		{"no handshake yet", Connecting, time.Time{}, now.Add(-time.Second), Connecting, false},
		{"no handshake in time", Connecting, time.Time{}, now.Add(-time.Minute), Connecting, true},
		{"renewed handshake", Connected, now.Add(-2 * time.Minute), built, Connected, false},
		{"handshake gone stale", Connected, now.Add(-4 * time.Minute), built, Reconnecting, true},
		{"handshake from before the attempt", Reconnecting, now.Add(-10 * time.Second), now.Add(-5 * time.Second), Reconnecting, false},
		{"handshake after the attempt", Reconnecting, now.Add(-time.Second), now.Add(-5 * time.Second), Connected, false},
	}
	for _, c := range cases {
		got, due := o.judge(c.status, c.handshake, c.attempt, 0, now)
		if got != c.want || due != c.due {
			t.Errorf("%s: %s, due %v; want %s, due %v", c.name, got, due, c.want, c.due)
		}
	}
}

// lanAddr is an address of this machine beyond loopback, which a stack
// takes for an address elsewhere.
func lanAddr(t *testing.T) netip.Addr {
	t.Helper()
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(n.IP); ok {
				ip = ip.Unmap()
				if ip.Is4() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
					return ip
				}
			}
		}
	}
	t.Skip("no IPv4 address beyond loopback")
	return netip.Addr{}
}

func TestAnExitServerCarriesTrafficOnToTheRealNetwork(t *testing.T) {
	lan := lanAddr(t)
	ln, err := net.Listen("tcp4", netip.AddrPortFrom(lan, 0).String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte(c.RemoteAddr().String()))
			c.Close()
		}
	}()

	sk, _ := GeneratePrivateKey()
	cfg := clientConfig(t, sk, 0)
	srv, err := startServer(serverOptions{key: sk, peer: cfg.PrivateKey.PublicKey(), exit: true, logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()
	cfg.Peer.Endpoint = "127.0.0.1:" + strconv.Itoa(int(srv.port))
	tun := New(Options{Poll: 50 * time.Millisecond, Logf: t.Logf})
	defer tun.Disconnect()
	if err := tun.Connect(dialCtx(t), cfg); err != nil {
		t.Fatal(err)
	}
	if err := tun.WaitConnected(dialCtx(t)); err != nil {
		t.Fatal(err)
	}
	c, err := tun.DialContext(dialCtx(t), "tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	seen, err := io.ReadAll(c)
	if err != nil || len(seen) == 0 {
		t.Fatalf("read %q, %v", seen, err)
	}
	t.Logf("the server on %s saw the tunnel come from %s", ln.Addr(), seen)
}
