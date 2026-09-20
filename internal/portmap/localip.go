package portmap

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
)

// localIPFor is the real LocalIP: it opens a UDP socket toward the gateway's
// control host and reads back the local address the kernel chose to reach it
// from. Nothing is written to the socket, so no packet goes out; a UDP connect
// only asks the routing table which interface and address would be used, which
// is what AddPortMapping's NewInternalClient argument needs.
//
// Dialling the router rather than a fixed public address is what makes the
// answer correct on a multi-homed box: the mapping has to carry the address of
// the interface facing this gateway, not of whichever one reaches the internet
// by default. A wrong guess here, loopback or a container bridge address,
// produces a mapping the router confirms and every peer finds useless, which
// is worse than "could not confirm" because nothing looks wrong until a
// torrent stays unreachable.
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
