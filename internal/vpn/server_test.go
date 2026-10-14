package vpn

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

var (
	serverAddr = netip.MustParseAddr("10.2.0.1")
	clientAddr = netip.MustParseAddr("10.2.0.2")
	tunnelNet  = netip.MustParsePrefix("10.2.0.0/24")
)

// testServer is a provider's end of a tunnel: a WireGuard device on a stack
// of its own that serves echo, DNS and NAT-PMP at serverAddr, the way a
// provider's gateway sits at the first address of the tunnel's network. With
// exit set it also forwards everything else to the real network, as a VPN
// server does.
type testServer struct {
	key  Key
	ns   *netStack
	dev  *device.Device
	port uint16
	gw   *fakeGateway
	logf func(string, ...any)
}

type serverOptions struct {
	key        Key
	peer       Key
	listenPort uint16
	exit       bool
	names      map[string]netip.Addr
	logf       func(string, ...any)
}

func startServer(opts serverOptions) (*testServer, error) {
	ns, err := newNetStack([]netip.Addr{serverAddr}, nil, DefaultMTU, !opts.exit)
	if err != nil {
		return nil, err
	}
	logf := opts.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &testServer{key: opts.key, ns: ns, logf: logf}
	if opts.exit {
		s.forward()
	}
	s.dev = device.NewDevice(ns, conn.NewDefaultBind(), &device.Logger{
		Verbosef: func(string, ...any) {},
		Errorf:   func(f string, a ...any) { logf("server wireguard: "+f, a...) },
	})
	uapi := fmt.Sprintf("private_key=%s\nlisten_port=%d\npublic_key=%s\nallowed_ip=%s/32\n",
		opts.key.hex(), opts.listenPort, opts.peer.hex(), clientAddr)
	if err := s.dev.IpcSet(uapi); err != nil {
		s.dev.Close()
		return nil, err
	}
	if err := s.dev.Up(); err != nil {
		s.dev.Close()
		return nil, err
	}
	get, _ := s.dev.IpcGet()
	for _, line := range strings.Split(get, "\n") {
		if v, ok := strings.CutPrefix(line, "listen_port="); ok {
			fmt.Sscan(v, &s.port)
		}
	}
	s.gw = &fakeGateway{public: netip.MustParseAddr("203.0.113.7"), lifetime: 60,
		assign: func(byte, uint16, uint16) (uint16, uint16) { return 51234, 0 }}
	if err := s.serve(opts.names); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *testServer) close() { s.dev.Close() }

func (s *testServer) serve(names map[string]netip.Addr) error {
	at := func(port uint16) netip.AddrPort { return netip.AddrPortFrom(serverAddr, port) }
	ln, err := s.ns.Listen("tcp4", at(7))
	if err != nil {
		return err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	echo, err := s.ns.ListenPacket("udp4", at(7))
	if err != nil {
		return err
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			echo.WriteTo(buf[:n], from)
		}
	}()
	dns, err := s.ns.ListenPacket("udp4", at(53))
	if err != nil {
		return err
	}
	go serveDNS(dns, names)
	gw, err := s.ns.ListenPacket("udp4", at(NATPMPPort))
	if err != nil {
		return err
	}
	go s.gw.serve(gw)
	return nil
}

// dial dials from the server into the tunnel, the way a peer reaches a port
// the provider forwards.
func (s *testServer) dial(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
	return s.ns.dial(ctx, "tcp4", to)
}

func serveDNS(pc net.PacketConn, names map[string]netip.Addr) {
	buf := make([]byte, 1500)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil {
			continue
		}
		q, err := p.Question()
		if err != nil {
			continue
		}
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionAvailable: true})
		b.EnableCompression()
		a, ok := names[strings.TrimSuffix(q.Name.String(), ".")]
		if !ok {
			b = dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, RCode: dnsmessage.RCodeNameError})
		}
		b.StartQuestions()
		b.Question(q)
		if ok && q.Type == dnsmessage.TypeA && a.Is4() {
			b.StartAnswers()
			b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: a.As4()})
		}
		out, err := b.Finish()
		if err == nil {
			pc.WriteTo(out, from)
		}
	}
}

// forward makes the stack take traffic for any address and carry it on over
// the real network, the way a VPN server's NAT does. The tunnel's own
// network stays inside.
func (s *testServer) forward() {
	st := s.ns.stack
	st.SetPromiscuousMode(nicID, true)
	st.SetSpoofing(nicID, true)
	tcpFwd := tcp.NewForwarder(st, 0, 1024, func(r *tcp.ForwarderRequest) {
		dst := endpointAddr(r.ID())
		if tunnelNet.Contains(dst.Addr()) {
			r.Complete(true)
			return
		}
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			r.Complete(true)
			return
		}
		r.Complete(false)
		in := gonet.NewTCPConn(&wq, ep)
		go func() {
			out, err := net.DialTimeout("tcp", dst.String(), 10*time.Second)
			if err != nil {
				s.logf("exit: tcp %s: %v", dst, err)
				in.Close()
				return
			}
			pipe(in, out)
		}()
	})
	st.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)
	udpFwd := udp.NewForwarder(st, func(r *udp.ForwarderRequest) {
		dst := endpointAddr(r.ID())
		if tunnelNet.Contains(dst.Addr()) {
			return
		}
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			return
		}
		in := gonet.NewUDPConn(&wq, ep)
		go func() {
			out, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(dst))
			if err != nil {
				in.Close()
				return
			}
			pipeUDP(in, out, 2*time.Minute)
		}()
	})
	st.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)
}

func endpointAddr(id stack.TransportEndpointID) netip.AddrPort {
	a, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	return netip.AddrPortFrom(a.Unmap(), id.LocalPort)
}

func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	a.Close()
	b.Close()
}

// pipeUDP relays datagrams both ways until neither side has sent anything
// for idle.
func pipeUDP(a, b net.Conn, idle time.Duration) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 65535)
		for {
			src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)
			if err != nil {
				done <- struct{}{}
				return
			}
			dst.Write(buf[:n])
		}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}
