package vpn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"syscall"

	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	gvlog "gvisor.dev/gvisor/pkg/log"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const nicID = 1

// netStack is a userspace TCP/IP stack that serves as a WireGuard device's
// tun: the device writes it the packets it decrypts and reads from it the
// packets its sockets send.
type netStack struct {
	ep       *channel.Endpoint
	stack    *stack.Stack
	mtu      int
	addrs    []netip.Addr
	dns      []netip.Addr
	events   chan tun.Event
	done     context.Context
	stop     context.CancelFunc
	once     sync.Once
	resolver *net.Resolver
}

// gVisor logs every packet it drops, such as one for a peer that gave a
// loopback address, to stderr.
var quietGVisor = sync.OnceFunc(func() { gvlog.SetLevel(gvlog.Warning) })

// newNetStack builds a stack with addrs. With handleLocal it delivers a packet
// for one of its own addresses without sending it; a stack that takes every
// address, as a VPN server's does, must not, or it takes every sender for
// itself and drops the packet.
func newNetStack(addrs, dns []netip.Addr, mtu int, handleLocal bool) (*netStack, error) {
	quietGVisor()
	ns := &netStack{
		ep: channel.New(1024, uint32(mtu), ""),
		stack: stack.New(stack.Options{
			NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
			TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
			HandleLocal:        handleLocal,
		}),
		mtu:    mtu,
		addrs:  addrs,
		dns:    dns,
		events: make(chan tun.Event, 1),
	}
	ns.done, ns.stop = context.WithCancel(context.Background())
	// gVisor's defaults suit a loopback; a torrent over a tunnel wants SACK
	// and windows that grow with the path.
	sack := tcpip.TCPSACKEnabled(true)
	moderate := tcpip.TCPModerateReceiveBufferOption(true)
	rcv := tcpip.TCPReceiveBufferSizeRangeOption{Min: 4 << 10, Default: 1 << 20, Max: 8 << 20}
	snd := tcpip.TCPSendBufferSizeRangeOption{Min: 4 << 10, Default: 1 << 20, Max: 8 << 20}
	for _, opt := range []tcpip.SettableTransportProtocolOption{&sack, &moderate, &rcv, &snd} {
		if err := ns.stack.SetTransportProtocolOption(tcp.ProtocolNumber, opt); err != nil {
			return nil, fmt.Errorf("vpn: tcp option %T: %v", opt, err)
		}
	}
	if err := ns.stack.CreateNIC(nicID, ns.ep); err != nil {
		return nil, fmt.Errorf("vpn: create NIC: %v", err)
	}
	var v4, v6 bool
	for _, a := range addrs {
		proto := ipv4.ProtocolNumber
		if a.Is6() {
			proto, v6 = ipv6.ProtocolNumber, true
		} else {
			v4 = true
		}
		pa := tcpip.ProtocolAddress{Protocol: proto, AddressWithPrefix: tcpip.AddrFromSlice(a.AsSlice()).WithPrefix()}
		if err := ns.stack.AddProtocolAddress(nicID, pa, stack.AddressProperties{}); err != nil {
			return nil, fmt.Errorf("vpn: add address %s: %v", a, err)
		}
	}
	if v4 {
		ns.stack.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: nicID})
	}
	if v6 {
		ns.stack.AddRoute(tcpip.Route{Destination: header.IPv6EmptySubnet, NIC: nicID})
	}
	ns.resolver = &net.Resolver{PreferGo: true, Dial: ns.dialDNS}
	ns.events <- tun.EventUp
	return ns, nil
}

func (ns *netStack) File() *os.File           { return nil }
func (ns *netStack) MTU() (int, error)        { return ns.mtu, nil }
func (ns *netStack) Name() (string, error)    { return "knightloader", nil }
func (ns *netStack) Events() <-chan tun.Event { return ns.events }
func (ns *netStack) BatchSize() int           { return 1 }

// Read hands the device the next packet a socket of the stack sent.
func (ns *netStack) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	pkt := ns.ep.ReadContext(ns.done)
	if pkt == nil {
		return 0, os.ErrClosed
	}
	view := pkt.ToView()
	pkt.DecRef()
	n, err := view.Read(bufs[0][offset:])
	view.Release()
	if err != nil {
		return 0, err
	}
	sizes[0] = n
	return 1, nil
}

// Write gives the stack the packets the device decrypted.
func (ns *netStack) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		p := b[offset:]
		if len(p) == 0 {
			continue
		}
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(p)})
		switch p[0] >> 4 {
		case 4:
			ns.ep.InjectInbound(header.IPv4ProtocolNumber, pkt)
		case 6:
			ns.ep.InjectInbound(header.IPv6ProtocolNumber, pkt)
		default:
			pkt.DecRef()
			return 0, syscall.EAFNOSUPPORT
		}
		pkt.DecRef()
	}
	return len(bufs), nil
}

func (ns *netStack) Close() error {
	ns.once.Do(func() {
		ns.stop()
		ns.stack.RemoveNIC(nicID)
		ns.stack.Close()
		ns.ep.Close()
		close(ns.events)
	})
	return nil
}

// has reports whether the stack has an address of a's family, which it
// needs to route a.
func (ns *netStack) has(a netip.Addr) bool {
	for _, own := range ns.addrs {
		if own.Is4() == a.Is4() {
			return true
		}
	}
	return false
}

func fullAddr(ap netip.AddrPort) (tcpip.FullAddress, tcpip.NetworkProtocolNumber) {
	proto := ipv4.ProtocolNumber
	if ap.Addr().Is6() {
		proto = ipv6.ProtocolNumber
	}
	var a tcpip.Address
	if ap.Addr().IsValid() && !ap.Addr().IsUnspecified() {
		a = tcpip.AddrFromSlice(ap.Addr().AsSlice())
	}
	return tcpip.FullAddress{NIC: nicID, Addr: a, Port: ap.Port()}, proto
}

// dial dials a literal address. A stack without an address of its family
// cannot route it.
func (ns *netStack) dial(ctx context.Context, network string, to netip.AddrPort) (net.Conn, error) {
	to = netip.AddrPortFrom(to.Addr().Unmap(), to.Port())
	if !ns.has(to.Addr()) {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("the tunnel has no address of this family")}
	}
	fa, proto := fullAddr(to)
	switch network {
	case "tcp", "tcp4", "tcp6":
		return gonet.DialContextTCP(ctx, ns.stack, fa, proto)
	case "udp", "udp4", "udp6":
		return gonet.DialUDP(ns.stack, nil, &fa, proto)
	}
	return nil, &net.OpError{Op: "dial", Net: network, Err: net.UnknownNetworkError(network)}
}

// DialContext dials through the tunnel, resolving a host name with the
// tunnel's name servers and trying each address in turn.
func (ns *netStack) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("vpn: bad port in %q", address)
	}
	ips, err := ns.lookup(ctx, network, host)
	if err != nil {
		return nil, err
	}
	err = fmt.Errorf("vpn: no address of %s to dial", host)
	for _, ip := range ips {
		c, dialErr := ns.dial(ctx, network, netip.AddrPortFrom(ip, uint16(port)))
		if dialErr == nil {
			return c, nil
		}
		err = dialErr
	}
	return nil, err
}

func (ns *netStack) lookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	family := "ip"
	switch network[len(network)-1] {
	case '4':
		family = "ip4"
	case '6':
		family = "ip6"
	}
	return ns.LookupNetIP(ctx, family, host)
}

// LookupNetIP resolves host with the tunnel's name servers, keeping the
// addresses the tunnel can reach.
func (ns *netStack) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if len(ns.dns) == 0 {
		return nil, &net.DNSError{Err: "the tunnel has no name server", Name: host}
	}
	ips, err := ns.resolver.LookupNetIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	out := ips[:0]
	for _, ip := range ips {
		if ip = ip.Unmap(); ns.has(ip) {
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no address the tunnel can reach", Name: host, IsNotFound: true}
	}
	return out, nil
}

// dialDNS sends the Go resolver's queries to the tunnel's name servers in
// turn, whatever server the system has configured.
func (ns *netStack) dialDNS(ctx context.Context, network, _ string) (net.Conn, error) {
	var err error
	for _, server := range ns.dns {
		var c net.Conn
		if c, err = ns.dial(ctx, network, netip.AddrPortFrom(server, 53)); err == nil {
			return c, nil
		}
	}
	return nil, err
}

// ListenPacket opens a UDP socket of the stack at at.
func (ns *netStack) ListenPacket(network string, at netip.AddrPort) (net.PacketConn, error) {
	fa, proto := fullAddr(at)
	return gonet.DialUDP(ns.stack, &fa, nil, proto)
}

// Listen opens a TCP listener of the stack at at.
func (ns *netStack) Listen(network string, at netip.AddrPort) (net.Listener, error) {
	fa, proto := fullAddr(at)
	return gonet.ListenTCP(ns.stack, fa, proto)
}
