package accounts

import "testing"

// TestCatalogueIntegrity guards the properties every consumer of Catalogue is
// entitled to assume without re-checking: no entry is missing the fields the
// accounts page needs to render it, no id repeats (a duplicate id would make
// Store.Get ambiguous about which entry it belongs to), and Kind/Group only
// ever hold one of the values this package defines.
func TestCatalogueIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, svc := range Catalogue {
		if svc.ID == "" {
			t.Fatalf("catalogue entry %+v has no id", svc)
		}
		if seen[svc.ID] {
			t.Fatalf("duplicate catalogue id %q", svc.ID)
		}
		seen[svc.ID] = true

		if svc.Label == "" {
			t.Errorf("%s: no label", svc.ID)
		}
		if svc.WhereURL == "" {
			t.Errorf("%s: no whereUrl", svc.ID)
		}
		switch svc.Kind {
		case KindAPIKey, KindUsernamePassword:
		default:
			t.Errorf("%s: unknown kind %q", svc.ID, svc.Kind)
		}
		switch svc.Group {
		case GroupDebrid, GroupHoster:
		default:
			t.Errorf("%s: unknown group %q", svc.ID, svc.Group)
		}
	}
}

func TestLookup(t *testing.T) {
	svc, ok := Lookup("torbox")
	if !ok || svc.Label != "TorBox" {
		t.Fatalf("Lookup(torbox) = %+v, %v; want the TorBox entry", svc, ok)
	}
	if _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("Lookup(does-not-exist) reported found")
	}
}
