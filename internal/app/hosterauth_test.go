package app

// The hoster/debrid split, at the one place it is decided: which hosts the
// "add a login" picker offers.

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// A catalogue link with www. must still match JD's plugin id without it, as for
// premiumize.me.
func TestDebridServicesAreFilteredDespiteWWW(t *testing.T) {
	skip := debridServiceDomains()

	// Only meaningful while the catalogue link really carries www.
	var whereURL string
	for _, svc := range accounts.Catalogue {
		if svc.ID == "premiumize" {
			whereURL = svc.WhereURL
		}
	}
	if !strings.Contains(whereURL, "www.") {
		t.Skipf("premiumize's WhereURL no longer carries www. (%q)", whereURL)
	}

	for _, host := range []string{"premiumize.me", "www.premiumize.me", "PREMIUMIZE.ME"} {
		if !skip[serviceKey(host)] {
			t.Errorf("serviceKey(%q) is not in the debrid skip set; it would be offered as a hoster login as well", host)
		}
	}
	// A real hoster still comes through.
	if skip[serviceKey("ddownload.com")] {
		t.Error("ddownload.com is being filtered out as a debrid service")
	}
}

// A hand-kept list fails silently when it stops matching, since a missing mark
// looks like an ordinary host. This checks a sample of JD's spellings rather
// than the exact set, which changes with JD's plugins.
func TestMultihosterListStillMatchesJD(t *testing.T) {
	// Names as JD's listPremiumHoster returns them, plus two ordinary hosts.
	fromJD := []string{
		"mega-debrid.eu", "zevera.com", "put.io", "simply-debrid.com", "deepbrid.com",
		"ddownload.com", "rapidgator.net",
	}
	if got := multihosterCount(fromJD); got != 5 {
		t.Errorf("%d of the sample marked as multihosters, want 5; the list has drifted from the names JD uses", got)
	}
	if IsMultihoster("ddownload.com") || IsMultihoster("rapidgator.net") {
		t.Error("an ordinary file host is being marked as a multihoster")
	}
	// The normalisation through serviceKey.
	for _, spelling := range []string{"www.zevera.com", "https://zevera.com/", "ZEVERA.COM"} {
		if !IsMultihoster(spelling) {
			t.Errorf("IsMultihoster(%q) is false; serviceKey is not normalising this spelling", spelling)
		}
	}
}
