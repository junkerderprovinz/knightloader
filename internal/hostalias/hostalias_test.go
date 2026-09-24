package hostalias

import "testing"

func TestCanonical(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"rg.to", "rapidgator.net"},
		{"RG.to", "rapidgator.net"},
		{"www.rg.to", "rapidgator.net"},
		{"rapidgator.net", "rapidgator.net"},
		{"ul.to", "uploaded.net"},
		{"k2s.cc", "keep2share.cc"},
		{"desfichiers.com", "1fichier.com"},
		{"www.example.com", "example.com"},
		{"cdn.rg.to", "cdn.rg.to"},
		{"notrg.to", "notrg.to"},
		{"", ""},
	} {
		if got := Canonical(c.in); got != c.want {
			t.Errorf("Canonical(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// An alias whose main domain is itself an alias would need two lookups to
// resolve.
func TestEveryAliasPointsAtAMainDomain(t *testing.T) {
	for alias, main := range canonical {
		if _, chained := canonical[main]; chained {
			t.Errorf("%s points at %s, which is an alias itself", alias, main)
		}
	}
}
