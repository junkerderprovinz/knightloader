package socks5

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

// maxDatagram is the largest UDP payload there is.
const maxDatagram = 65535

// association relays one client's datagrams through the tunnel. It lives as
// long as the client's connection that asked for it.
type association struct {
	s     *Server
	relay *net.UDPConn
	// want is where the client said it would send from; a zero port takes
	// whatever port the first datagram comes from, on the address the
	// client's connection came from.
	want netip.AddrPort

	mu     sync.Mutex
	client netip.AddrPort
	out    map[string]net.PacketConn
	names  map[string]resolved
	wg     sync.WaitGroup
}

type resolved struct {
	ip netip.Addr
	at time.Time
}

func (s *Server) associate(c net.Conn, host string, port uint16) {
	from, err := netip.ParseAddrPort(c.RemoteAddr().String())
	if err != nil {
		reply(c, replyFailure, netip.AddrPort{})
		return
	}
	at, err := netip.ParseAddrPort(c.LocalAddr().String())
	if err != nil {
		reply(c, replyFailure, netip.AddrPort{})
		return
	}
	relay, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.AddrPortFrom(at.Addr(), 0)))
	if err != nil {
		s.logf("socks5: no UDP relay: %v", err)
		reply(c, replyFailure, netip.AddrPort{})
		return
	}
	a := &association{s: s, relay: relay, out: map[string]net.PacketConn{}, names: map[string]resolved{}}
	a.want = netip.AddrPortFrom(from.Addr().Unmap(), 0)
	if ip, err := netip.ParseAddr(host); err == nil && !ip.IsUnspecified() {
		a.want = netip.AddrPortFrom(ip.Unmap(), port)
	} else if err == nil {
		a.want = netip.AddrPortFrom(a.want.Addr(), port)
	}
	if !reply(c, replyOK, netip.MustParseAddrPort(relay.LocalAddr().String())) {
		relay.Close()
		return
	}
	_ = c.SetDeadline(time.Time{})
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.run()
	}()
	// The association ends when its connection does.
	_, _ = io.Copy(io.Discard, c)
	a.close()
}

func (a *association) close() {
	_ = a.relay.Close()
	a.mu.Lock()
	for _, pc := range a.out {
		_ = pc.Close()
	}
	a.mu.Unlock()
	a.wg.Wait()
}

// run takes the client's datagrams and sends each through the tunnel.
func (a *association) run() {
	buf := make([]byte, maxDatagram)
	for {
		n, from, err := a.relay.ReadFromUDPAddrPort(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		// Windows reports a datagram the client's port refused on the next
		// read, which says nothing about the datagrams after it.
		if err != nil {
			continue
		}
		if !a.admit(netip.AddrPortFrom(from.Addr().Unmap(), from.Port())) {
			continue
		}
		dst, payload, ok := a.parse(buf[:n])
		if !ok {
			continue
		}
		a.send(dst, payload)
	}
}

// admit reports whether a datagram from from is the client's: from the
// address its connection came from, and from the port it named or the first
// one seen.
func (a *association) admit(from netip.AddrPort) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client.IsValid() {
		return from == a.client
	}
	if from.Addr() != a.want.Addr() || (a.want.Port() != 0 && from.Port() != a.want.Port()) {
		return false
	}
	a.client = from
	return true
}

func (a *association) clientAddr() netip.AddrPort {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

// parse reads a datagram's header and resolves its destination in the
// tunnel. A fragment is dropped, as the protocol allows a server to do.
func (a *association) parse(b []byte) (netip.AddrPort, []byte, bool) {
	if len(b) < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
		return netip.AddrPort{}, nil, false
	}
	r := bytes.NewReader(b[4:])
	host, port, ok := readAddr(r, b[3])
	if !ok {
		return netip.AddrPort{}, nil, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		if ip, ok = a.resolve(host); !ok {
			return netip.AddrPort{}, nil, false
		}
	}
	return netip.AddrPortFrom(ip.Unmap(), port), b[len(b)-r.Len():], true
}

func (a *association) resolve(host string) (netip.Addr, bool) {
	now := time.Now()
	a.mu.Lock()
	if r, ok := a.names[host]; ok && now.Sub(r.at) < nameTTL {
		a.mu.Unlock()
		return r.ip, true
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(a.s.ctx, lookupTimeout)
	defer cancel()
	ips, err := a.s.tun.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.names) >= maxNames {
		clear(a.names)
	}
	a.names[host] = resolved{ip: ips[0], at: now}
	return ips[0], true
}

// send sends payload to dst from the tunnel's socket of dst's family, opened
// on the first datagram of that family. A socket that fails is dropped, and
// the next datagram opens another, so a tunnel that came back is used again.
func (a *association) send(dst netip.AddrPort, payload []byte) {
	network := "udp4"
	if dst.Addr().Is6() {
		network = "udp6"
	}
	a.mu.Lock()
	pc := a.out[network]
	a.mu.Unlock()
	if pc == nil {
		ctx, cancel := context.WithTimeout(a.s.ctx, lookupTimeout)
		opened, err := a.s.tun.ListenPacket(ctx, network)
		cancel()
		if err != nil {
			return
		}
		pc = opened
		a.mu.Lock()
		a.out[network] = pc
		a.wg.Add(1)
		a.mu.Unlock()
		go func() {
			defer a.wg.Done()
			a.back(network, pc)
		}()
	}
	if _, err := pc.WriteTo(payload, net.UDPAddrFromAddrPort(dst)); err != nil {
		a.forget(network, pc)
	}
}

func (a *association) forget(network string, pc net.PacketConn) {
	_ = pc.Close()
	a.mu.Lock()
	if a.out[network] == pc {
		delete(a.out, network)
	}
	a.mu.Unlock()
}

// back hands the client every datagram that comes to the tunnel's socket pc,
// under the header that says where it came from.
func (a *association) back(network string, pc net.PacketConn) {
	defer a.forget(network, pc)
	buf := make([]byte, maxDatagram)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		ua, ok := from.(*net.UDPAddr)
		client := a.clientAddr()
		if !ok || !client.IsValid() {
			continue
		}
		msg := appendAddr([]byte{0, 0, 0}, ua.AddrPort())
		msg = append(msg, buf[:n]...)
		_, _ = a.relay.WriteToUDPAddrPort(msg, client)
	}
}
