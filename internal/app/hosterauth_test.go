package app

// The hoster/debrid split, at the one place it is decided: which hosts the
// "add a login" picker offers.

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// TestDebridServicesAreFilteredDespiteWWW pins the comparison serviceKey does,
// against the exact shape that slipped through: a catalogue entry whose
// "where do I get a key" link carries www. and a JD plugin id that does not.
//
// Premiumize is not a stand-in here, it is the case that shipped broken: its
// card existed, its filter entry existed, and it still appeared in the hoster
// picker for a whole release because "www.premiumize.me" != "premiumize.me".
func TestDebridServicesAreFilteredDespiteWWW(t *testing.T) {
	skip := debridServiceDomains()

	// The fixture is only meaningful if the catalogue really does carry a www.
	// link for this service - otherwise the test would pass without exercising
	// anything (and would keep passing if someone "fixed" it by editing the URL).
	var whereURL string
	for _, svc := range accounts.Catalogue {
		if svc.ID == "premiumize" {
			whereURL = svc.WhereURL
		}
	}
	if !strings.Contains(whereURL, "www.") {
		t.Skipf("premiumize's WhereURL no longer carries www. (%q) - this trap is gone", whereURL)
	}

	for _, host := range []string{"premiumize.me", "www.premiumize.me", "PREMIUMIZE.ME"} {
		if !skip[serviceKey(host)] {
			t.Errorf("serviceKey(%q) is not in the debrid skip set - it would be offered as a hoster login as well", host)
		}
	}
	// And a real hoster must still come through, or the filter is just an
	// empty picker.
	if skip[serviceKey("ddownload.com")] {
		t.Error("ddownload.com is being filtered out as a debrid service")
	}
}

// TestMultihosterListStillMatchesJD guards the one way a hand-kept list fails
// silently: it stops matching anything and nobody notices, because a missing
// mark looks exactly like a host that is not a multihoster.
//
// It does not assert an exact set. JD's plugin list moves, entries come and go,
// and a test that pinned all nineteen would fail for the wrong reason on the
// first change. What it pins is that the list still describes reality: the
// services it names are spelled the way JD spells them, and the normalisation
// around it works.
func TestMultihosterListStillMatchesJD(t *testing.T) {
	// The exact strings JD's own listPremiumHoster returned on the preview
	// instance on 2026-09-07, plus two ordinary hosts as controls.
	fromJD := []string{
		"mega-debrid.eu", "zevera.com", "put.io", "simply-debrid.com", "deepbrid.com",
		"ddownload.com", "rapidgator.net",
	}
	if got := multihosterCount(fromJD); got != 5 {
		t.Errorf("%d of the sample marked as multihosters, want 5 - the list has drifted from the names JD uses", got)
	}
	if IsMultihoster("ddownload.com") || IsMultihoster("rapidgator.net") {
		t.Error("an ordinary file host is being marked as a multihoster")
	}
	// The normalisation, which is the half that broke for the debrid filter.
	for _, spelling := range []string{"www.zevera.com", "https://zevera.com/", "ZEVERA.COM"} {
		if !IsMultihoster(spelling) {
			t.Errorf("IsMultihoster(%q) is false - serviceKey is not normalising this spelling", spelling)
		}
	}
}
