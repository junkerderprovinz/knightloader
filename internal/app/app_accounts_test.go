package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
)

// TestTorboxHosterDomainsSeparatesStreamFromHoster pins the fix for a real,
// live-confirmed bug (2026-08-25, "Die ganzen links im linksammler zeigen
// noch immer nicht ihre namen richtig an"): TorBox's public /hosters list
// mixes real file hosters (type:"hoster", e.g. rapidgator) with media/social
// pages it also unlocks by scraping them (type:"stream", e.g. YouTube,
// Twitch). Feeding the unfiltered union into ytdlp.Resolver's ExcludeHosts
// silently routed every "stream" host around yt-dlp - the exact backend
// with an async title probe - and onto the nameless JD catch-all instead.
//
// internal/app/routing_test.go's own TestRouting cannot catch this: its
// fixture is a single hand-picked hoster-type domain that never includes a
// stream-type one, so it would keep passing whether this filtering existed,
// was correct, or was silently reversed.
func TestTorboxHosterDomainsSeparatesStreamFromHoster(t *testing.T) {
	hosters := []torbox.Hoster{
		{Name: "Rapidgator", Domain: "rapidgator.net", Type: "hoster"},
		{Name: "YouTube", Domains: []string{"youtube.com", "youtu.be"}, Type: "stream"},
		{Name: "Mega", Domain: "www.mega.nz", Type: "hoster"},
	}

	full := torboxHosterDomains(hosters, false)
	for _, want := range []string{"rapidgator.net", "youtube.com", "youtu.be", "mega.nz"} {
		if !full[want] {
			t.Errorf("unfiltered set missing %q, want every hoster AND stream domain (torbox.Resolver's own Hosts needs both)", want)
		}
	}

	hosterOnly := torboxHosterDomains(hosters, true)
	for _, want := range []string{"rapidgator.net", "mega.nz"} {
		if !hosterOnly[want] {
			t.Errorf("hoster-only set missing %q, a real type:%q hoster", want, "hoster")
		}
	}
	for _, dontWant := range []string{"youtube.com", "youtu.be"} {
		if hosterOnly[dontWant] {
			t.Errorf("hoster-only set contains %q, a type:%q entry ytdlp's ExcludeHosts must not carry", dontWant, "stream")
		}
	}
}

// TestTorboxLeavesMediaSitesToYtdlp pins the routing decision behind a
// complaint that looked like a naming bug (jdp, 2026-09-06: "wenn ich ein
// youtube link im sammler hinzufüge heißt der ordner wieder watch und es wird
// nur ein link angezeigt, nicht alle dateien").
//
// Measured on two live instances: the same YouTube link routes to ytdlp on the
// one with no TorBox key and to TORBOX on the one with a key, because TorBox's
// host list covers streaming sites and TorBox outranks yt-dlp. A TorBox-routed
// media link gets no variant rows and no title probe, so it stays one nameless
// row in a folder named after the URL's path - permanently.
//
// TorBox can genuinely fetch those sites, so it keeps them when yt-dlp is not
// there at all. What it must not do is take them AWAY from the tool that turns
// one link into five keepable rows with a quality to pick.
func TestTorboxLeavesMediaSitesToYtdlp(t *testing.T) {
	hosters := []torbox.Hoster{
		{Name: "Rapidgator", Domain: "rapidgator.net", Type: "hoster"},
		{Name: "YouTube", Domains: []string{"youtube.com", "youtu.be"}, Type: "stream"},
	}
	all := torboxHosterDomains(hosters, false)
	fileOnly := torboxHosterDomains(hosters, true)

	withYtdlp := torboxRoutingHosts(all, fileOnly, true)
	if withYtdlp["youtube.com"] {
		t.Error("TorBox still claims youtube.com while yt-dlp is running - the link never reaches the variant expansion")
	}
	if !withYtdlp["rapidgator.net"] {
		t.Error("TorBox stopped claiming a real file hoster, which is the one thing it is for")
	}

	withoutYtdlp := torboxRoutingHosts(all, fileOnly, false)
	for _, want := range []string{"youtube.com", "rapidgator.net"} {
		if !withoutYtdlp[want] {
			t.Errorf("without yt-dlp, TorBox must still claim %q - nothing else can fetch it", want)
		}
	}

	// An unreadable host list must not be read as "TorBox supports nothing":
	// that would silently stop routing through a working account.
	if got := torboxRoutingHosts(all, nil, true); !got["rapidgator.net"] {
		t.Error("an empty file-hoster list dropped the whole routing set instead of falling back to it")
	}
}
