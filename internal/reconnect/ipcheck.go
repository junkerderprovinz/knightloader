package reconnect

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrNotPublic means the check response held an address that cannot be this
// box's address on the internet, as a router status page or a captive portal
// would. Unlike ErrNoAddress it points at the wrong side of the router rather
// than a wrong check URL. Taking a LAN address as the public one would report
// "unchanged" on every run, or a false success when a reboot moves the DHCP
// lease.
var ErrNotPublic = errors.New("reconnect: the check response holds a non-public address")

// cgnat is RFC 6598's shared address space, used behind a carrier's NAT. netip
// has no predicate for it and it is not RFC 1918, and on such a line a
// reconnect cannot change the address that matters.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// PublicIP reads this box's public address out of a check response: FindIP
// plus the check that the address could be public. The caller adds the check
// URL to the error.
func PublicIP(s string) (netip.Addr, error) {
	addr, ok := FindIP(s)
	if !ok {
		return netip.Addr{}, ErrNoAddress
	}
	if why := nonPublicReason(addr); why != "" {
		return netip.Addr{}, fmt.Errorf("%w: %s is %s", ErrNotPublic, addr, why)
	}
	return addr, nil
}

// nonPublicReason names the range addr falls in, or "" when it could be a
// public address, so the message says which network the check URL reached.
// Unique-local comes before RFC 1918 because IsPrivate covers fc00::/7 too.
func nonPublicReason(addr netip.Addr) string {
	switch {
	case !addr.IsValid():
		return "not an address"
	case addr.IsUnspecified():
		return "the unspecified address"
	case addr.IsLoopback():
		return "a loopback address"
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return "a link-local address"
	case addr.IsMulticast():
		return "a multicast address"
	case cgnat.Contains(addr):
		return "inside carrier-grade NAT (100.64.0.0/10), so it belongs to the provider rather than to this line"
	case addr.Is6() && addr.IsPrivate():
		return "a unique-local address (fc00::/7)"
	case addr.IsPrivate():
		return "a private address (RFC 1918)"
	}
	return ""
}

// maxLiteral is the longest an address can be written out
// ("::ffff:255.255.255.255") plus room for a ":port" suffix. Longer runs are
// skipped rather than handed to the parser.
const maxLiteral = 46

// FindIP returns the first IP address literal in s. It errs towards finding
// nothing, since a wrong address read from an IP-check page looks like a
// successful reconnect. It reads plain text, HTML and JSON alike.
func FindIP(s string) (netip.Addr, bool) {
	for i := 0; i < len(s); {
		if !isAddrByte(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isAddrByte(s[j]) {
			j++
		}
		if j-i <= maxLiteral {
			if addr, ok := parseLiteral(s[i:j]); ok {
				return addr, true
			}
		}
		i = j
	}
	return netip.Addr{}, false
}

// isAddrByte reports whether a byte can be part of an address literal. Letters
// outside a-f are excluded, so the "b" and "d" of "<body>" do not join the
// digits around them.
func isAddrByte(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		return true
	case c == '.' || c == ':':
		return true
	}
	return false
}

// parseLiteral reads one candidate run: whole, as host:port, or without
// trailing full stops. It never drops digits until something parses, which
// would read a build number like "1.2.3.4444" as 1.2.3.4.
func parseLiteral(run string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(run); err == nil {
		return addr.Unmap(), true
	}
	// Router status pages often print the WAN address with a port.
	if ap, err := netip.ParseAddrPort(run); err == nil {
		return ap.Addr().Unmap(), true
	}
	// No valid literal ends in a full stop, so one there is punctuation.
	if trimmed := strings.TrimRight(run, "."); trimmed != run {
		if addr, err := netip.ParseAddr(trimmed); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// dropPartialTail removes the last, possibly incomplete literal from a body cut
// off at the read limit. A cut inside "203.0.113.99" leaves "203.0.113.9",
// which parses and would make every later check look like a change.
func dropPartialTail(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if !isAddrByte(b[i]) {
			return b[:i+1]
		}
	}
	// The whole body is one unterminated run, so none of it can be trusted.
	return nil
}
