package reconnect

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ErrGatewayUnavailable means the default gateway could not be read, so the
// settings form has nothing to offer and must ask.
//
// It is a distinct error from "there is no gateway" on purpose: on a platform
// whose routing table this package cannot read, the answer is "ask the user",
// while on Linux with no default route the answer is "this box has no way out
// and the reconnect is not the problem".
var ErrGatewayUnavailable = errors.New("reconnect: the default gateway cannot be read here")

// RouterAddress is a default gateway and the interface it is reached through.
//
// The interface name is carried because it is the only cheap way to see the
// answer is wrong. In a container on a bridge network the default gateway is the
// bridge, not the router, and "172.17.0.1 via eth0" says that at a glance where
// a bare address would be typed straight into the router field and then quietly
// fail to log in.
type RouterAddress struct {
	Address   netip.Addr `json:"address"`
	Interface string     `json:"interface,omitempty"`
}

// procNetRoute is Linux's IPv4 routing table. It is a constant rather than an
// argument because the parser, which is the part with decisions in it, takes an
// io.Reader and is tested on its own.
const procNetRoute = "/proc/net/route"

// DefaultGateway reports the box's IPv4 default gateway, so the reconnect form
// can offer an address instead of asking somebody to go and find one.
//
// It answers only on Linux, and says so plainly everywhere else rather than
// falling back to 192.168.1.1. A guessed gateway is worse than no answer: the
// form would arrive pre-filled with a plausible address, the user would accept
// it, and the first reconnect would post their router password to whatever
// happens to live at that address on their network.
//
// IPv6 default routes are not read. A router administration page is reached over
// IPv4 in every case this has to serve, and a v6 gateway that is a link-local
// address with a zone would be offered to the user as something they cannot type
// into a browser either.
func DefaultGateway() (RouterAddress, error) {
	if runtime.GOOS != "linux" {
		return RouterAddress{}, fmt.Errorf("%w: %s has no routing table this package can read", ErrGatewayUnavailable, runtime.GOOS)
	}
	f, err := os.Open(procNetRoute)
	if err != nil {
		return RouterAddress{}, fmt.Errorf("%w: %v", ErrGatewayUnavailable, err)
	}
	defer f.Close()
	return parseProcNetRoute(f)
}

// maxRouteLines caps how much of the routing table is read. A machine with a
// full BGP table in the kernel would otherwise have every one of its routes
// parsed to answer a question about one of them.
const maxRouteLines = 4096

// parseProcNetRoute picks the default route with the lowest metric out of
// Linux's /proc/net/route.
//
// The format is a header line and then whitespace-separated columns:
//
//	Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
//
// with the addresses written as little-endian hexadecimal - "0101A8C0" is
// 192.168.1.1, not 1.1.168.192.
func parseProcNetRoute(r io.Reader) (RouterAddress, error) {
	const (
		colIface = iota
		colDest
		colGateway
		colFlags
		_ // RefCnt
		_ // Use
		colMetric
		columns // everything past the metric is ignored, but must be present
	)
	// The kernel flags this parser cares about, from linux/route.h.
	const (
		rtfUp      = 0x0001
		rtfGateway = 0x0002
	)

	best := RouterAddress{}
	bestMetric := 0
	found := false

	sc := bufio.NewScanner(r)
	for line := 0; sc.Scan() && line < maxRouteLines; line++ {
		f := strings.Fields(sc.Text())
		if len(f) < columns {
			// The header line and any trailing blank line land here, which is
			// why an unparsable row is skipped rather than failing the read.
			continue
		}
		// Only the route to 0.0.0.0/anything is a default route. Matching on the
		// gateway column alone would pick up the first on-link route with a
		// next hop and offer a neighbour's address as the router.
		if f[colDest] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[colFlags], 16, 32)
		if err != nil || flags&rtfUp == 0 || flags&rtfGateway == 0 {
			continue
		}
		addr, ok := parseHexAddr(f[colGateway])
		if !ok {
			continue
		}
		metric, err := strconv.Atoi(f[colMetric])
		if err != nil {
			continue
		}
		// Lowest metric wins, which is the same route the kernel would use. A
		// box on wifi and ethernet at once has two default routes, and offering
		// the gateway of the one that is not carrying traffic sends the login
		// request out of the wrong interface.
		if !found || metric < bestMetric {
			best = RouterAddress{Address: addr, Interface: f[colIface]}
			bestMetric = metric
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		return RouterAddress{}, fmt.Errorf("%w: %v", ErrGatewayUnavailable, err)
	}
	if !found {
		return RouterAddress{}, fmt.Errorf("%w: this machine has no default route", ErrGatewayUnavailable)
	}
	return best, nil
}

// parseHexAddr reads one little-endian hexadecimal address column.
//
// It refuses the addresses that can never be a usable router: the unspecified
// address, which is what an on-link route writes in the gateway column, and
// loopback, multicast and the unassigned 0.0.0.0/8 range, none of which anyone
// can log in to. Handing one of those back would put an address in the form that
// looks like an answer and is not one.
func parseHexAddr(s string) (netip.Addr, bool) {
	if len(s) != 8 {
		return netip.Addr{}, false
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return netip.Addr{}, false
	}
	v := binary.LittleEndian.Uint32(raw)
	addr := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() || addr.As4()[0] == 0 {
		return netip.Addr{}, false
	}
	return addr, true
}
