package reconnect

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrNotPublic means the check response held an address that cannot be this
// box's address on the internet.
//
// It is its own sentinel rather than a flavour of ErrNoAddress because the two
// send the user to different places: ErrNoAddress means the page has no address
// on it at all, so the check URL is wrong, while this one means the page did
// answer with an address and it came from the wrong side of the router. A
// router's own status page and a captive portal both do exactly that.
//
// Treating one as the public address is worse than it first looks. The LAN
// address usually holds still, so every run reports "the address did not
// change" and the reconnect is blamed for a router that did as it was told -
// and when it does move, because the reboot handed the box a different DHCP
// lease, the run reports a success for a public address that never went
// anywhere. Both readings are wrong, and neither is visible from the outside.
var ErrNotPublic = errors.New("reconnect: the check response holds a non-public address")

// cgnat is RFC 6598's shared address space, the range a carrier hands out when
// the customer is behind the ISP's own NAT. netip has no predicate for it and it
// is not RFC 1918, so it would otherwise pass for a public address - on the one
// kind of line where a reconnect can never change anything, because the address
// that moves belongs to the carrier.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// PublicIP reads this box's public address out of a check response.
//
// It is FindIP plus the one question FindIP cannot answer: whether what was
// found could be a public address at all. The caller adds the check URL to the
// error - this function has never seen it.
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

// nonPublicReason names the range addr falls in, or "" when it could be a public
// address.
//
// It returns the name rather than a bool because "192.168.1.1 is a private
// address" tells somebody their check URL is pointed at something on their own
// network, while "not a public address" sends them to look at the router. The
// order matters where the ranges overlap in netip's predicates: fc00::/7 is what
// IsPrivate means for IPv6, so unique-local has to be named before RFC 1918 or
// an IPv6 address would be reported under the name of an IPv4 rule.
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

// maxLiteral is the longest an address can be written out: an IPv4-mapped IPv6
// literal ("::ffff:255.255.255.255") plus room to spare for a ":port" suffix.
// Runs longer than this are skipped, which keeps a page full of hex from being
// handed to the parser one window at a time.
const maxLiteral = 46

// FindIP returns the first IP address literal in s.
//
// The whole package rests on this answer, so it errs towards finding nothing
// rather than finding the wrong thing: a wrong address read out of an IP-check
// page is indistinguishable from a successful reconnect, and that is the one
// outcome this package exists to prevent. It reads plain text, HTML and JSON
// bodies alike because IP-check endpoints disagree about which they serve.
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
// outside a-f are excluded, which is what keeps the "b" and "d" of "<body>" from
// joining the digits around them into one candidate.
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

// parseLiteral reads one candidate run.
//
// It tries the run whole, then as host:port, then without trailing full stops -
// and nothing else. In particular it never gives back digits until something
// parses: that would read a build number like "1.2.3.4444" as the address
// 1.2.3.4, and comparing against a made-up address is how a reconnect that did
// nothing gets reported as a success.
func parseLiteral(run string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(run); err == nil {
		return addr.Unmap(), true
	}
	// Router status pages routinely print the WAN address with a port.
	if ap, err := netip.ParseAddrPort(run); err == nil {
		return ap.Addr().Unmap(), true
	}
	// A trailing full stop is sentence punctuation. No valid literal ends in
	// one, so trimming them can never destroy a real address.
	if trimmed := strings.TrimRight(run, "."); trimmed != run {
		if addr, err := netip.ParseAddr(trimmed); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// dropPartialTail removes the last, possibly incomplete literal from a body that
// was cut off at the read limit.
//
// The bug it prevents: a cut in the middle of "203.0.113.99" leaves
// "203.0.113.9", which parses perfectly and is a different address than the one
// on the page. Reading that as the "before" value means every later check
// differs from it, and the reconnect reports success without the address having
// moved at all.
func dropPartialTail(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if !isAddrByte(b[i]) {
			return b[:i+1]
		}
	}
	// The whole body is one unterminated run, so none of it can be trusted.
	return nil
}
