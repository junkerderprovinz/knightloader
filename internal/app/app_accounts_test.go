package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
)

// TestTorboxHosterDomainsSeparatesStreamFromHoster: TorBox's /hosters list mixes
// file hosters with streaming sites, and feeding the stream domains into
// ytdlp's ExcludeHosts would route them away from yt-dlp and its title probe.
func TestTorboxHosterDomainsSeparatesStreamFromHoster(t *testing.T) {
	hosters := []torbox.Hoster{
		{Name: "Rapidgator", Domain: "rapidgator.net", Type: "hoster"},
		{Name: "YouTube", Domains: []string{"youtube.com", "youtu.be"}, Type: "stream"},
		{Name: "Mega", Domain: "www.mega.nz", Type: "hoster"},
	}

	full := torboxHosterDomains(hosters, false)
	for _, want := range []string{"rapidgator.net", "youtube.com", "youtu.be", "mega.nz"} {
		if !full[want] {
			t.Errorf("unfiltered set missing %q, want every hoster and stream domain (torbox.Resolver's own Hosts needs both)", want)
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

// TestTorboxLeavesMediaSitesToYtdlp: TorBox outranks yt-dlp, and a media link
// routed to TorBox gets no variant rows and no title. TorBox keeps those sites
// only when yt-dlp is not available.
func TestTorboxLeavesMediaSitesToYtdlp(t *testing.T) {
	hosters := []torbox.Hoster{
		{Name: "Rapidgator", Domain: "rapidgator.net", Type: "hoster"},
		{Name: "YouTube", Domains: []string{"youtube.com", "youtu.be"}, Type: "stream"},
	}
	all := torboxHosterDomains(hosters, false)
	fileOnly := torboxHosterDomains(hosters, true)

	withYtdlp := torboxRoutingHosts(all, fileOnly, true)
	if withYtdlp["youtube.com"] {
		t.Error("TorBox still claims youtube.com while yt-dlp is running, so the link never reaches the variant expansion")
	}
	if !withYtdlp["rapidgator.net"] {
		t.Error("TorBox stopped claiming a real file hoster")
	}

	withoutYtdlp := torboxRoutingHosts(all, fileOnly, false)
	for _, want := range []string{"youtube.com", "rapidgator.net"} {
		if !withoutYtdlp[want] {
			t.Errorf("without yt-dlp, TorBox must still claim %q since nothing else can fetch it", want)
		}
	}

	// An unreadable host list must not read as "TorBox supports nothing".
	if got := torboxRoutingHosts(all, nil, true); !got["rapidgator.net"] {
		t.Error("an empty file-hoster list dropped the whole routing set instead of falling back to it")
	}
}
