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

// A multihoster login is listed with the debrid accounts, so the row says which
// it is; an ordinary hoster login stays unmarked.
func TestAMultihosterLoginIsMarkedAsOne(t *testing.T) {
	a := newQueueApp(t)
	for _, host := range []string{"leechall.io", "ddownload.com"} {
		if err := a.SetHosterLogin(host, "user", "secret"); err != nil {
			t.Fatal(err)
		}
	}
	marked := map[string]bool{}
	for _, l := range a.HosterLogins() {
		marked[l.Host] = l.Multihoster
	}
	if !marked["leechall.io"] || marked["ddownload.com"] {
		t.Errorf("multihoster marks = %v, want leechall.io only", marked)
	}
}

func TestClosedMultihostersAreNotOffered(t *testing.T) {
	for _, host := range []string{"debridplanet.com", "www.simply-debrid.com"} {
		if !closedMultihosters[serviceKey(host)] {
			t.Errorf("%s is offered although the service is closed", host)
		}
	}
}

// A debrid service in the catalogue without a client would take a key and then
// route nothing, and a client without a catalogue entry could never get one.
func TestEveryDebridServiceInTheCatalogueHasAClient(t *testing.T) {
	built := map[string]bool{}
	for _, s := range debridServices {
		built[s.id] = true
	}
	for _, svc := range accounts.Catalogue {
		if svc.Group != accounts.GroupDebrid || svc.ID == "torbox" {
			continue
		}
		if !built[svc.ID] {
			t.Errorf("catalogue service %q has no entry in debridServices", svc.ID)
		}
		delete(built, svc.ID)
	}
	for id := range built {
		t.Errorf("debridServices builds %q, which the catalogue does not offer", id)
	}
}

// The smaller multihosters KnightLoader speaks to itself leave the hoster
// picker, including CocoLeech, whose key page is on a subdomain.
func TestNativeMultihostersLeaveTheHosterPicker(t *testing.T) {
	skip := debridServiceDomains()
	for _, host := range []string{"cocoleech.com", "deepbrid.com", "mega-debrid.eu", "premium.rpnet.biz", "zevera.com"} {
		if !skip[serviceKey(host)] {
			t.Errorf("%s is still offered as a hoster login", host)
		}
	}
}

// A multihoster that lists YouTube must not take it from yt-dlp, which gives
// the link its variant rows and title; TorBox's own streaming entries count
// as media sites too.
func TestDebridHostListsLeaveMediaSitesToYtdlp(t *testing.T) {
	torboxAll := map[string]bool{"rapidgator.net": true, "vk.com": true}
	torboxFiles := map[string]bool{"rapidgator.net": true}
	media := knownMediaSites(torboxAll, torboxFiles)

	listed := map[string]bool{"youtube.com": true, "youtu.be": true, "soundcloud.com": true, "vk.com": true, "katfile.com": true}
	got := withoutHosts(listed, media)
	if len(got) != 1 || !got["katfile.com"] {
		t.Errorf("routed hosts = %v, want only katfile.com", got)
	}
	if !listed["youtube.com"] {
		t.Error("withoutHosts changed the list it was given")
	}
}
