package app

// The hoster/debrid split, at the one place it is decided: which hosts the
// "add a login" picker offers.

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// The picker takes JD's list and offers no multihoster: the closed ones and
// LeechAll have no client here, and the others are debrid services of
// KnightLoader's own. put.io, a storage service with its own files, is an
// ordinary hoster.
func TestHosterPickerOffersNoMultihoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/listPremiumHoster" {
			_, _ = w.Write([]byte(`{"data":["dailyleech.com","www.multivip.net","debridplanet.com",` +
				`"simply-debrid.com","put.io","leechall.io","mydebrid.com","zevera.com","ddownload.com"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	defer srv.Close()
	t.Setenv("KL_JD", srv.URL)
	a := newQueueApp(t)

	offered := map[string]bool{}
	for _, h := range a.HosterHosts(context.Background()) {
		offered[h.ID] = true
	}
	for _, multi := range []string{"dailyleech.com", "www.multivip.net", "debridplanet.com", "simply-debrid.com",
		"leechall.io", "mydebrid.com", "zevera.com"} {
		if offered[multi] {
			t.Errorf("%s is offered as a hoster login", multi)
		}
	}
	for _, host := range []string{"put.io", "ddownload.com"} {
		if !offered[host] {
			t.Errorf("%s is missing from the picker", host)
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
	for _, host := range []string{"cocoleech.com", "deepbrid.com", "mega-debrid.eu", "mydebrid.com", "premium.rpnet.biz", "zevera.com"} {
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
