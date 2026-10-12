package settings

// The reverse proxies whose X-Forwarded-For the sign-in throttle believes
// (internal/api's loginClientKey). Behind a proxy every caller arrives from
// the proxy's address, so without this list one person guessing at the
// password makes everybody wait. A proxy left off it costs only that; an
// address on it that is not a proxy lets whoever sends from there name any
// address it likes, so the list starts empty.

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// maxTrustedProxies bounds the list. A setup has one proxy, or a few in a row.
const maxTrustedProxies = 32

// A range may be no wider than these. Every address inside one can name any
// caller it likes, so a range must hold the proxies alone, and one as wide as
// a home or office network, a /24 or more, is sure to hold the visitors too.
// Both allow sixteen addresses, since an IPv6 network hands out a /64 or more
// to every household on it. A container network's proxy is named by its own
// address.
const (
	maxProxyRangeV4 = 28
	maxProxyRangeV6 = 124
)

// ProxyRangeTooWide refuses a trusted range wider than maxProxyRangeV4 or
// maxProxyRangeV6. Bits is the widest the entry's family may be.
type ProxyRangeTooWide struct {
	Entry string
	Bits  int
}

func (e *ProxyRangeTooWide) Error() string {
	return fmt.Sprintf("trusted proxy %s is too wide: a range can be at most /%d and must cover your proxies and no one who signs in", e.Entry, e.Bits)
}

// parseTrustedProxy reads one entry: an IP address, which stands for itself,
// or a CIDR range.
func parseTrustedProxy(entry string) (netip.Prefix, error) {
	entry = strings.TrimSpace(entry)
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		widest := maxProxyRangeV6
		if p.Addr().Is4() {
			widest = maxProxyRangeV4
		}
		if p.Bits() < widest {
			return netip.Prefix{}, &ProxyRangeTooWide{Entry: entry, Bits: widest}
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// ValidateTrustedProxies refuses an entry that is neither an IP address nor a
// CIDR range, which the throttle would otherwise skip without a word.
func (s Settings) ValidateTrustedProxies() error {
	n := 0
	for _, e := range s.TrustedProxies {
		if strings.TrimSpace(e) == "" {
			continue
		}
		n++
		_, err := parseTrustedProxy(e)
		var wide *ProxyRangeTooWide
		if errors.As(err, &wide) {
			return &FieldError{Field: "trustedProxies", Err: err}
		}
		if err != nil {
			return &FieldError{Field: "trustedProxies",
				Err: fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR range such as 10.10.0.0/28", strings.TrimSpace(e))}
		}
	}
	if n > maxTrustedProxies {
		return &FieldError{Field: "trustedProxies",
			Err: fmt.Errorf("there are %d trusted proxies; the limit is %d", n, maxTrustedProxies)}
	}
	return nil
}

// TrustedProxyPrefixes is the list as ranges, a single address as a range of
// its own. An entry that does not parse, or a range wider than the limit that
// an earlier build saved, is left out; the Security page shows it as unused.
func (s Settings) TrustedProxyPrefixes() []netip.Prefix {
	var out []netip.Prefix
	for _, e := range s.TrustedProxies {
		if p, err := parseTrustedProxy(e); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func sanitizeTrustedProxies(n Settings) Settings {
	seen := make(map[string]bool, len(n.TrustedProxies))
	out := make([]string, 0, len(n.TrustedProxies))
	for _, e := range n.TrustedProxies {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
		if len(out) >= maxTrustedProxies {
			break
		}
	}
	n.TrustedProxies = out
	return n
}
