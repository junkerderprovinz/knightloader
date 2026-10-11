package settings

import (
	"errors"
	"testing"
)

// TestATrustedRangeHoldsProxiesNotVisitors: anybody inside a trusted range
// names their own bucket in X-Forwarded-For, so a range as wide as a network
// of visitors is refused when saved, and passed over when an older file
// holds one.
func TestATrustedRangeHoldsProxiesNotVisitors(t *testing.T) {
	for _, c := range []struct {
		entry string
		ok    bool
	}{
		{"172.17.0.0/16", true},
		{"172.16.0.0/15", false},
		{"10.0.0.0/8", false},
		{"0.0.0.0/0", false},
		{"2001:db8:1::/48", true},
		{"2001:db8::/47", false},
		{"::/0", false},
		{"192.0.2.10", true},
		{"2001:db8::1", true},
	} {
		err := Settings{TrustedProxies: []string{c.entry}}.ValidateTrustedProxies()
		var wide *ProxyRangeTooWide
		if c.ok && err != nil {
			t.Errorf("%s was refused: %v", c.entry, err)
		}
		if !c.ok && !errors.As(err, &wide) {
			t.Errorf("%s = %v, want ProxyRangeTooWide", c.entry, err)
		}
		trusted := Settings{TrustedProxies: []string{c.entry}}.TrustedProxyPrefixes()
		if c.ok != (len(trusted) == 1) {
			t.Errorf("%s is believed: %v, want %v", c.entry, len(trusted) == 1, c.ok)
		}
	}
}
