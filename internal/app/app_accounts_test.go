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
