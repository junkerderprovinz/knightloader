package reconnect

import (
	"net/netip"
	"strings"
)

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
