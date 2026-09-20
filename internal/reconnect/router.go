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
// settings form has to ask. The wrapped message says whether the platform
// cannot be read or the machine has no default route at all.
var ErrGatewayUnavailable = errors.New("reconnect: the default gateway cannot be read here")

// RouterAddress is a default gateway and the interface it is reached through.
// The interface shows at a glance when the answer is wrong: in a container on
// a bridge network the gateway is the bridge, and "172.17.0.1 via eth0" says
// so.
type RouterAddress struct {
	Address   netip.Addr `json:"address"`
	Interface string     `json:"interface,omitempty"`
}

// procNetRoute is Linux's IPv4 routing table. The parser takes an io.Reader
// and is tested on its own.
const procNetRoute = "/proc/net/route"

// DefaultGateway reports the box's IPv4 default gateway, so the reconnect form
// can offer an address.
//
// It answers only on Linux and never guesses 192.168.1.1 elsewhere: a
// plausible pre-filled address would get the router password posted to
// whatever lives there. IPv6 routes are not read, since router admin pages are
// reached over IPv4 and a link-local v6 gateway with a zone cannot be typed
// into a browser.
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

// maxRouteLines caps how much of the routing table is read, for machines with
// a full BGP table in the kernel.
const maxRouteLines = 4096

// parseProcNetRoute picks the default route with the lowest metric out of
// Linux's /proc/net/route: a header line and then whitespace-separated columns
//
//	Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
//
// with the addresses in little-endian hexadecimal ("0101A8C0" is 192.168.1.1).
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
			// The header line and trailing blank lines.
			continue
		}
		// Only a route to 0.0.0.0 is a default route; any route with a next hop
		// has a gateway column.
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
		// Lowest metric wins, as in the kernel, for a box on wifi and ethernet
		// at once.
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

// parseHexAddr reads one little-endian hexadecimal address column. It refuses
// addresses that can never be a router: unspecified (an on-link route),
// loopback, multicast and 0.0.0.0/8.
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
