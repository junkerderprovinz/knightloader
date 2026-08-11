package portmap

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
)

// localIPFor is the real LocalIP: it opens a UDP socket toward the
// gateway's own control host and reads back the local address the kernel
// chose to reach it from. A UDP "connect" never puts a packet on the wire -
// nothing is written to the socket - it only asks the routing table which
// interface and address would be used, which is exactly the question
// AddPortMapping's own NewInternalClient argument needs answered.
//
// Dialling the actual router, rather than some fixed public address, is
// what makes the answer correct on a multi-homed box: the interface facing
// this specific gateway is the one whose address the mapping has to carry,
// not whichever interface happens to reach the internet by whatever route
// the OS prefers by default. A wrong guess here - loopback, a stale address
// from another interface, a container bridge address that is not this
// box's LAN address at all - produces a mapping the router confirms and
// every real peer finds useless, which is a worse failure than an honest
// "could not confirm" because nothing about it looks wrong until a torrent
// stays unreachable for a reason nobody can see from the settings page.
func localIPFor(ctx context.Context, controlURL string) (netip.Addr, error) {
	u, err := url.Parse(controlURL)
	if err != nil || u.Hostname() == "" {
		return netip.Addr{}, fmt.Errorf("portmap: %q is not a usable control URL", controlURL)
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("portmap: could not learn this machine's own address facing %s: %w", u.Hostname(), err)
	}
	defer conn.Close()
	ap, err := netip.ParseAddrPort(conn.LocalAddr().String())
	if err != nil {
		return netip.Addr{}, fmt.Errorf("portmap: %s: %w", conn.LocalAddr(), err)
	}
	return ap.Addr(), nil
}
