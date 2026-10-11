package settings

// The reverse proxies whose X-Forwarded-For the sign-in throttle believes
// (internal/api's loginClientKey). Behind a proxy every caller arrives from
// the proxy's address, so without this list one person guessing at the
// password makes everybody wait. A proxy left off it costs only that; an
// address on it that is not a proxy lets whoever sends from there name any
// address it likes, so the list starts empty.

import (
	"fmt"
	"net/netip"
	"strings"
)

// maxTrustedProxies bounds the list. A setup has one proxy, or a few in a row.
const maxTrustedProxies = 32

// parseTrustedProxy reads one entry: an IP address, which stands for itself,
// or a CIDR range.
func parseTrustedProxy(entry string) (netip.Prefix, error) {
	entry = strings.TrimSpace(entry)
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
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
		if _, err := parseTrustedProxy(e); err != nil {
			return &FieldError{Field: "trustedProxies",
				Err: fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR range such as 172.17.0.0/16", strings.TrimSpace(e))}
		}
	}
	if n > maxTrustedProxies {
		return &FieldError{Field: "trustedProxies",
			Err: fmt.Errorf("there are %d trusted proxies; the limit is %d", n, maxTrustedProxies)}
	}
	return nil
}

// TrustedProxyPrefixes is the list as ranges, a single address as a range of
// its own. An entry that does not parse is left out.
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
