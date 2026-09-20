package accounts

import "testing"

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
		case GroupDebrid, GroupHoster, GroupCaptchaSolver, GroupRemoteServer:
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

// TestCaptchaSolverEntries pins the solver ids, which internal/captcha and
// internal/settings match by literal string.
func TestCaptchaSolverEntries(t *testing.T) {
	for _, id := range []string{"2captcha", "anticaptcha"} {
		svc, ok := Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", id)
		}
		if svc.Kind != KindAPIKey {
			t.Errorf("%s: kind = %q, want apiKey", id, svc.Kind)
		}
		if svc.Group != GroupCaptchaSolver {
			t.Errorf("%s: group = %q, want captchaSolver", id, svc.Group)
		}
	}
}
