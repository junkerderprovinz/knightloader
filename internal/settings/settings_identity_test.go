package settings

import (
	"reflect"
	"testing"
)

func TestSanitizeIdentityTrimsInstanceName(t *testing.T) {
	got := sanitizeIdentity(Settings{InstanceName: "  My Server  "}).InstanceName
	if got != "My Server" {
		t.Errorf("InstanceName = %q, want trimmed", got)
	}
}

func TestSanitizeIdentityDropsBlankAndDuplicateDomains(t *testing.T) {
	n := sanitizeIdentity(Settings{KnownDomains: []string{
		"https://a.example.com", "  ", "https://a.example.com", "https://b.example.com", "",
	}})
	want := []string{"https://a.example.com", "https://b.example.com"}
	if !reflect.DeepEqual(n.KnownDomains, want) {
		t.Errorf("KnownDomains = %v, want %v (blanks dropped, duplicate collapsed, order preserved)", n.KnownDomains, want)
	}
}

func TestSanitizeIdentityTrimsEachDomain(t *testing.T) {
	n := sanitizeIdentity(Settings{KnownDomains: []string{"  https://a.example.com  "}})
	want := []string{"https://a.example.com"}
	if !reflect.DeepEqual(n.KnownDomains, want) {
		t.Errorf("KnownDomains = %v, want %v (whitespace trimmed)", n.KnownDomains, want)
	}
}

// TestSanitizeIdentityCapsKnownDomains proves maxKnownDomains actually bounds
// the list, so a build behind a rotating set of throwaway subdomains cannot
// grow this field forever (see its own doc comment).
func TestSanitizeIdentityCapsKnownDomains(t *testing.T) {
	var in []string
	for i := 0; i < maxKnownDomains+5; i++ {
		in = append(in, "https://d"+string(rune('a'+i))+".example.com")
	}
	got := sanitizeIdentity(Settings{KnownDomains: in}).KnownDomains
	if len(got) != maxKnownDomains {
		t.Errorf("len(KnownDomains) = %d, want %d (capped)", len(got), maxKnownDomains)
	}
	if !reflect.DeepEqual(got, in[:maxKnownDomains]) {
		t.Errorf("KnownDomains = %v, want the first %d entries kept in order", got, maxKnownDomains)
	}
}

func TestSanitizeIdentityEmptyStaysNonNil(t *testing.T) {
	if got := sanitizeIdentity(Settings{}).KnownDomains; got == nil {
		t.Error("KnownDomains = nil, want a non-nil empty slice so it always serialises as [], never JSON null")
	}
}
