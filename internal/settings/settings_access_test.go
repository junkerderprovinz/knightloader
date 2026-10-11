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
		{"10.10.0.0/28", true},
		{"10.10.0.0/27", false},
		{"192.168.1.0/24", false},
		{"192.168.0.0/16", false},
		{"172.17.0.0/16", false},
		{"172.16.0.0/15", false},
		{"192.168.1.10/32", true},
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

// TestAWideRangeSavedEarlierIsKeptForTheOwnerToSee: a range wider than the
// limit is not believed, but it stays in the settings, where the Security
// page shows it as unused, rather than going without a word.
func TestAWideRangeSavedEarlierIsKeptForTheOwnerToSee(t *testing.T) {
	kept := sanitizeTrustedProxies(Settings{TrustedProxies: []string{"192.168.0.0/16", "10.0.0.2"}})
	if len(kept.TrustedProxies) != 2 || kept.TrustedProxies[0] != "192.168.0.0/16" {
		t.Fatalf("the saved list reads back as %v, want both entries kept", kept.TrustedProxies)
	}
	if got := kept.TrustedProxyPrefixes(); len(got) != 1 || got[0].String() != "10.0.0.2/32" {
		t.Errorf("believed %v, want only the single proxy address", got)
	}
}
