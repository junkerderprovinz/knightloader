package socks5

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeTunnel stands in for the VPN: it records every destination it is asked
// for and reaches it over loopback, so a test sees both what went in and what
// came back.
type fakeTunnel struct {
	mu    sync.Mutex
	asked []string
	names map[string]netip.Addr
	down  bool
}

func (f *fakeTunnel) note(what string) {
	f.mu.Lock()
	f.asked = append(f.asked, what)
	f.mu.Unlock()
}

func (f *fakeTunnel) destinations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

func (f *fakeTunnel) isDown() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.down
}

var errTunnelDown = errors.New("the tunnel is down")

func (f *fakeTunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	f.note(network + " " + address)
	if f.isDown() {
		return nil, errTunnelDown
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if a, ok := f.names[host]; ok {
		address = net.JoinHostPort(a.String(), port)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func (f *fakeTunnel) ListenPacket(ctx context.Context, network string) (net.PacketConn, error) {
	if f.isDown() {
		return nil, errTunnelDown
	}
	pc, err := net.ListenPacket(network, "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &recordingPacketConn{PacketConn: pc, f: f}, nil
}

func (f *fakeTunnel) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	f.note("lookup " + host)
	if a, ok := f.names[host]; ok {
		return []netip.Addr{a}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

type recordingPacketConn struct {
	net.PacketConn
	f *fakeTunnel
}

func (c *recordingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.f.note("udp " + addr.String())
	return c.PacketConn.WriteTo(p, addr)
}

func startServer(t *testing.T, tun Tunnel) *Server {
	t.Helper()
	s, err := Start(tun, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// echoTCP answers every connection with what it sends.
func echoTCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// echoUDP answers every datagram with "echo:" and the datagram.
func echoUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteToUDPAddrPort(append([]byte("echo:"), buf[:n]...), from)
		}
	}()
	return pc
}

// client is the client side of the SOCKS5 handshake, written out byte by
// byte so the tests can also send what a well-behaved client would not.
type client struct {
	t *testing.T
	c net.Conn
}

func dialServer(t *testing.T, s *Server) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", s.Addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	return &client{t: t, c: c}
}

func (cl *client) write(b ...byte) {
	cl.t.Helper()
	if _, err := cl.c.Write(b); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *client) read(n int) []byte {
	cl.t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(cl.c, b); err != nil {
		cl.t.Fatalf("reading %d bytes: %v", n, err)
	}
	return b
}

// login offers the password method and sends user and pass, and returns the
// status byte of the answer.
func (cl *client) login(user, pass string) byte {
	cl.t.Helper()
	cl.write(5, 1, 2)
	if got := cl.read(2); !bytes.Equal(got, []byte{5, 2}) {
		cl.t.Fatalf("method answer %v, want the password method", got)
	}
	msg := []byte{1, byte(len(user))}
	msg = append(msg, user...)
	msg = append(msg, byte(len(pass)))
	msg = append(msg, pass...)
	cl.write(msg...)
	return cl.read(2)[1]
}

// request sends a request for cmd to addr, a host:port, and returns the reply
// code and the bound address.
func (cl *client) request(cmd byte, addr string) (byte, netip.AddrPort) {
	cl.t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		cl.t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	msg := []byte{5, cmd, 0}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is4() {
		msg = append(msg, 1)
		msg = append(msg, ip.AsSlice()...)
	} else if err == nil {
		msg = append(msg, 4)
		msg = append(msg, ip.AsSlice()...)
	} else {
		msg = append(msg, 3, byte(len(host)))
		msg = append(msg, host...)
	}
	msg = binary.BigEndian.AppendUint16(msg, uint16(port))
	cl.write(msg...)
	head := cl.read(4)
	var ip netip.Addr
	switch head[3] {
	case 1:
		ip, _ = netip.AddrFromSlice(cl.read(4))
	case 4:
		ip, _ = netip.AddrFromSlice(cl.read(16))
	default:
		cl.t.Fatalf("reply with address type %d", head[3])
	}
	p := binary.BigEndian.Uint16(cl.read(2))
	return head[1], netip.AddrPortFrom(ip, p)
}

func TestAConnectGoesThroughTheTunnelAndCarriesBothWays(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoTCP(t)
	user, pass := s.Credentials()

	cl := dialServer(t, s)
	if st := cl.login(user, pass); st != 0 {
		t.Fatalf("login answered %d", st)
	}
	if code, _ := cl.request(1, echo); code != 0 {
		t.Fatalf("connect answered %d", code)
	}
	cl.write([]byte("piece 7")...)
	if got := string(cl.read(7)); got != "piece 7" {
		t.Errorf("echo %q", got)
	}
	if got := tun.destinations(); !slices.Equal(got, []string{"tcp " + echo}) {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestAHostNameIsHandedToTheTunnelToResolve(t *testing.T) {
	echo := echoTCP(t)
	host, port, _ := net.SplitHostPort(echo)
	tun := &fakeTunnel{names: map[string]netip.Addr{"tracker.example": netip.MustParseAddr(host)}}
	s := startServer(t, tun)
	user, pass := s.Credentials()

	cl := dialServer(t, s)
	cl.login(user, pass)
	if code, _ := cl.request(1, net.JoinHostPort("tracker.example", port)); code != 0 {
		t.Fatalf("connect answered %d", code)
	}
	if got := tun.destinations(); !slices.Equal(got, []string{"tcp tracker.example:" + port}) {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestAClientWithoutThePasswordIsTurnedAway(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	cl := dialServer(t, s)
	cl.write(5, 1, 0)
	if got := cl.read(2); !bytes.Equal(got, []byte{5, 0xff}) {
		t.Errorf("an offer of no authentication answered %v, want no acceptable method", got)
	}
}

func TestAWrongPasswordReachesNothing(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	user, pass := s.Credentials()
	for _, try := range []struct{ user, pass string }{
		{user, pass + "x"},
		{user, pass[:len(pass)-1]},
		{user + "x", pass},
		{"", ""},
	} {
		cl := dialServer(t, s)
		if st := cl.login(try.user, try.pass); st == 0 {
			t.Errorf("%q/%q was let in", try.user, try.pass)
		}
		if _, err := cl.c.Read(make([]byte, 1)); err == nil {
			t.Errorf("the connection of %q/%q stayed open", try.user, try.pass)
		}
	}
	if got := tun.destinations(); len(got) != 0 {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestEveryServerHasCredentialsOfItsOwn(t *testing.T) {
	a := startServer(t, &fakeTunnel{})
	b := startServer(t, &fakeTunnel{})
	_, pa := a.Credentials()
	_, pb := b.Credentials()
	if pa == pb || len(pa) < 32 {
		t.Errorf("passwords %q and %q", pa, pb)
	}
}

func TestItListensOnLoopbackOnly(t *testing.T) {
	s := startServer(t, &fakeTunnel{})
	ap, err := netip.ParseAddrPort(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if !ap.Addr().IsLoopback() {
		t.Errorf("listens on %s", ap)
	}
}

func TestATunnelThatIsDownRefusesAndNothingGoesDirect(t *testing.T) {
	echo := echoTCP(t)
	tun := &fakeTunnel{down: true}
	s := startServer(t, tun)
	user, pass := s.Credentials()

	cl := dialServer(t, s)
	cl.login(user, pass)
	if code, _ := cl.request(1, echo); code == 0 {
		t.Fatal("a connect through a tunnel that is down succeeded")
	}
	if _, err := cl.c.Read(make([]byte, 1)); err == nil {
		t.Error("the connection stayed open after the refusal")
	}
}

func TestBindIsNotOffered(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	user, pass := s.Credentials()
	cl := dialServer(t, s)
	cl.login(user, pass)
	if code, _ := cl.request(2, "0.0.0.0:0"); code != 7 {
		t.Errorf("bind answered %d, want command not supported", code)
	}
}

// associate opens a UDP association and returns the client's UDP socket and
// where the relay listens.
func associate(t *testing.T, s *Server) (*client, *net.UDPConn, netip.AddrPort) {
	t.Helper()
	user, pass := s.Credentials()
	cl := dialServer(t, s)
	cl.login(user, pass)
	code, relay := cl.request(3, "0.0.0.0:0")
	if code != 0 {
		t.Fatalf("associate answered %d", code)
	}
	if !relay.Addr().IsLoopback() {
		t.Fatalf("the relay is at %s", relay)
	}
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { uc.Close() })
	return cl, uc, relay
}

// datagram wraps data for to, an address or host:port, in the UDP request
// header.
func datagram(to string, data []byte) []byte {
	host, portStr, _ := net.SplitHostPort(to)
	port, _ := strconv.Atoi(portStr)
	msg := []byte{0, 0, 0}
	if ip, err := netip.ParseAddr(host); err == nil {
		a4 := ip.As4()
		msg = append(msg, 1)
		msg = append(msg, a4[:]...)
	} else {
		msg = append(msg, 3, byte(len(host)))
		msg = append(msg, host...)
	}
	msg = binary.BigEndian.AppendUint16(msg, uint16(port))
	return append(msg, data...)
}

func readDatagram(t *testing.T, uc *net.UDPConn) (netip.AddrPort, []byte) {
	t.Helper()
	_ = uc.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 2048)
	n, err := uc.Read(buf)
	if err != nil {
		t.Fatalf("no answer through the relay: %v", err)
	}
	b := buf[:n]
	if len(b) < 10 || b[3] != 1 {
		t.Fatalf("answer %v", b)
	}
	ip, _ := netip.AddrFromSlice(b[4:8])
	return netip.AddrPortFrom(ip, binary.BigEndian.Uint16(b[8:10])), b[10:]
}

func TestADatagramGoesThroughTheTunnelAndItsAnswerComesBack(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoUDP(t)
	_, uc, relay := associate(t, s)

	to := echo.LocalAddr().String()
	if _, err := uc.WriteToUDPAddrPort(datagram(to, []byte("announce")), relay); err != nil {
		t.Fatal(err)
	}
	from, data := readDatagram(t, uc)
	if from.String() != to || string(data) != "echo:announce" {
		t.Errorf("answer from %s: %q", from, data)
	}
	if got := tun.destinations(); !slices.Equal(got, []string{"udp " + to}) {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestADatagramForAHostNameIsResolvedInTheTunnel(t *testing.T) {
	echo := echoUDP(t)
	ap := netip.MustParseAddrPort(echo.LocalAddr().String())
	tun := &fakeTunnel{names: map[string]netip.Addr{"udp.tracker.example": ap.Addr()}}
	s := startServer(t, tun)
	_, uc, relay := associate(t, s)

	to := net.JoinHostPort("udp.tracker.example", strconv.Itoa(int(ap.Port())))
	if _, err := uc.WriteToUDPAddrPort(datagram(to, []byte("connect")), relay); err != nil {
		t.Fatal(err)
	}
	if _, data := readDatagram(t, uc); string(data) != "echo:connect" {
		t.Errorf("answer %q", data)
	}
	if got := tun.destinations(); !slices.Equal(got, []string{"lookup udp.tracker.example", "udp " + ap.String()}) {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestOnlyTheFirstSenderMayUseAnAssociation(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoUDP(t)
	_, uc, relay := associate(t, s)
	to := echo.LocalAddr().String()
	if _, err := uc.WriteToUDPAddrPort(datagram(to, []byte("first")), relay); err != nil {
		t.Fatal(err)
	}
	readDatagram(t, uc)

	other, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.WriteToUDPAddrPort(datagram(to, []byte("second")), relay); err != nil {
		t.Fatal(err)
	}
	_ = other.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, err := other.Read(make([]byte, 64)); err == nil {
		t.Errorf("a second sender got an answer of %d bytes", n)
	}
	if got := tun.destinations(); len(got) != 1 {
		t.Errorf("the tunnel was asked for %v", got)
	}
}

func TestAFragmentedDatagramIsDropped(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoUDP(t)
	_, uc, relay := associate(t, s)
	d := datagram(echo.LocalAddr().String(), []byte("part"))
	d[2] = 1
	if _, err := uc.WriteToUDPAddrPort(d, relay); err != nil {
		t.Fatal(err)
	}
	_ = uc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := uc.Read(make([]byte, 64)); err == nil {
		t.Error("a fragment was passed on")
	}
}

func TestAnAssociationEndsWithItsConnection(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoUDP(t)
	cl, uc, relay := associate(t, s)
	cl.c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = uc.WriteToUDPAddrPort(datagram(echo.LocalAddr().String(), []byte("late")), relay)
		_ = uc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := uc.Read(make([]byte, 64)); err != nil {
			return
		}
	}
	t.Error("the relay went on after its connection closed")
}

func TestDropEndsEveryConnectionItCarries(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoTCP(t)
	user, pass := s.Credentials()
	cl := dialServer(t, s)
	cl.login(user, pass)
	if code, _ := cl.request(1, echo); code != 0 {
		t.Fatalf("connect answered %d", code)
	}
	s.Drop()
	if _, err := cl.c.Read(make([]byte, 1)); err == nil {
		t.Error("a connection outlived Drop")
	}
	cl2 := dialServer(t, s)
	if st := cl2.login(user, pass); st != 0 {
		t.Errorf("after Drop a new login answered %d", st)
	}
}

func TestCloseEndsConnectionsAndTheListener(t *testing.T) {
	tun := &fakeTunnel{}
	s, err := Start(tun, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	echo := echoTCP(t)
	user, pass := s.Credentials()
	cl := dialServer(t, s)
	cl.login(user, pass)
	cl.request(1, echo)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.c.Read(make([]byte, 1)); err == nil {
		t.Error("a connection outlived Close")
	}
	if c, err := net.DialTimeout("tcp", s.Addr(), time.Second); err == nil {
		c.Close()
		t.Error("the listener outlived Close")
	}
}

func TestADatagramToAClosedPortDoesNotEndTheAssociation(t *testing.T) {
	tun := &fakeTunnel{}
	s := startServer(t, tun)
	echo := echoUDP(t)
	_, uc, relay := associate(t, s)

	closed, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	nobody := closed.LocalAddr().String()
	closed.Close()
	for range 3 {
		if _, err := uc.WriteToUDPAddrPort(datagram(nobody, []byte("anyone?")), relay); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := uc.WriteToUDPAddrPort(datagram(echo.LocalAddr().String(), []byte("still")), relay); err != nil {
		t.Fatal(err)
	}
	if _, data := readDatagram(t, uc); string(data) != "echo:still" {
		t.Errorf("answer %q", data)
	}
}
