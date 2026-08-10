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
		case GroupDebrid, GroupHoster, GroupCaptchaSolver:
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

// TestCaptchaSolverEntries guards the two facts internal/captcha's solver
// clients and internal/settings' sanitizeCaptcha both depend on by the
// literal id string, with no shared Go constant tying them together (see
// catalogue.go's own doc comment: ids are coordinated by convention, the
// same way "torbox"/"alldebrid"/"realdebrid" already are) - a rename here
// with no matching update there would silently orphan every stored solver
// key.
func TestCaptchaSolverEntries(t *testing.T) {
	for _, id := range []string{"2captcha", "anticaptcha"} {
		svc, ok := Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", id)
		}
		if svc.Kind != KindAPIKey {
			t.Errorf("%s: kind = %q, want apiKey - both services issue a single key", id, svc.Kind)
		}
		if svc.Group != GroupCaptchaSolver {
			t.Errorf("%s: group = %q, want captchaSolver", id, svc.Group)
		}
	}
}
