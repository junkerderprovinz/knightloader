package ytdlp

import (
	"slices"
	"strings"
	"testing"
)

// A preset for YouTube offers what YouTube serves, in the order and under the
// names a probed YouTube row's pickers use, and nothing a YouTube link never
// has.
func TestAKnownSiteOffersOnlyWhatItServes(t *testing.T) {
	got := HostFormats("youtube.com", nil, nil, false)
	if !got.Known {
		t.Fatal("youtube.com reads as unknown")
	}
	if want := []string{"best", "mp4 avc1", "mp4 vp9", "mp4 av1", "webm vp9"}; !slices.Equal(got.VideoFormats, want) {
		t.Errorf("VideoFormats = %v, want %v", got.VideoFormats, want)
	}
	if want := []string{"best", "aac", "opus"}; !slices.Equal(got.AudioFormats, want) {
		t.Errorf("AudioFormats = %v, want %v", got.AudioFormats, want)
	}
}

// The menus a probe of a YouTube-like source builds are ones the table
// already has, so a preset and a row of the same site name the same formats.
func TestTheTableNamesFormatsAsAProbeDoes(t *testing.T) {
	yt := HostFormats("youtube.com", nil, nil, false)
	for _, v := range VideoContainers(youtubeLike) {
		if !slices.Contains(yt.VideoFormats, v) {
			t.Errorf("a probe lists video format %q, which the table's %v lacks", v, yt.VideoFormats)
		}
	}
	for _, a := range AudioFormatsOf(youtubeLike) {
		if !slices.Contains(yt.AudioFormats, a) {
			t.Errorf("a probe lists audio format %q, which the table's %v lacks", a, yt.AudioFormats)
		}
	}
}

func TestASiteIsFoundUnderItsOtherAddresses(t *testing.T) {
	yt := HostFormats("youtube.com", nil, nil, false)
	for _, host := range []string{"www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "YouTube.com"} {
		if got := HostFormats(host, nil, nil, false); !got.Known || !slices.Equal(got.AudioFormats, yt.AudioFormats) {
			t.Errorf("HostFormats(%q) = %+v, want YouTube's", host, got)
		}
	}
	if got := HostFormats("artist.bandcamp.com", nil, nil, false); !got.Known {
		t.Error("an artist's page on bandcamp.com reads as unknown")
	}
	for _, host := range []string{"notyoutube.com", "youtube.com.example.org", "com", ""} {
		if got := HostFormats(host, nil, nil, false); got.Known {
			t.Errorf("HostFormats(%q) = %+v, want it unknown", host, got)
		}
	}
}

// A preset is looked up under every address the site table gives the link's
// site, the one the host lies under first, so a youtu.be link finds a preset
// saved for youtube.com.
func TestAPresetIsLookedUpUnderItsSitesOtherAddresses(t *testing.T) {
	cases := map[string][]string{
		"youtube.com":         {"youtube.com", "youtu.be", "youtube-nocookie.com"},
		"www.YouTube.com":     {"youtube.com", "youtu.be", "youtube-nocookie.com"},
		"youtu.be":            {"youtu.be", "youtube.com", "youtube-nocookie.com"},
		"m.youtube.com":       {"m.youtube.com", "youtube.com", "youtu.be", "youtube-nocookie.com"},
		"artist.bandcamp.com": {"artist.bandcamp.com", "bandcamp.com"},
		"dai.ly":              {"dai.ly", "dailymotion.com"},
		"example.org":         {"example.org"},
		"notyoutube.com":      {"notyoutube.com"},
		"":                    nil,
	}
	for host, want := range cases {
		if got := PresetKeys(host); !slices.Equal(got, want) {
			t.Errorf("PresetKeys(%q) = %v, want %v", host, got, want)
		}
	}
}

// A new preset goes under the site's first address, which every other address
// of the site looks up, so one set from a subdomain covers the short links too.
func TestANewPresetIsSavedForTheWholeSite(t *testing.T) {
	cases := map[string]string{
		"youtube.com":         "youtube.com",
		"m.youtube.com":       "youtube.com",
		"www.youtu.be":        "youtube.com",
		"artist.bandcamp.com": "bandcamp.com",
		"dai.ly":              "dailymotion.com",
		"www.Example.org":     "example.org",
		"":                    "",
	}
	for host, want := range cases {
		if got := SitePresetKey(host); got != want {
			t.Errorf("SitePresetKey(%q) = %q, want %q", host, got, want)
		}
	}
	for _, other := range []string{"youtu.be", "music.youtube.com", "youtube-nocookie.com"} {
		if !slices.Contains(PresetKeys(other), SitePresetKey("m.youtube.com")) {
			t.Errorf("a link from %s does not look up the preset saved from m.youtube.com", other)
		}
	}
}

// What a probe found is offered beside the table, since the table only knows
// what yt-dlp's extractor usually lists.
func TestAProbeAddsWhatTheTableLacks(t *testing.T) {
	got := HostFormats("vimeo.com", []string{"best", "mp4 avc1", "webm vp9"}, []string{"best", "opus"}, true)
	if want := []string{"best", "mp4 avc1", "mp4 hevc", "webm vp9"}; !slices.Equal(got.VideoFormats, want) {
		t.Errorf("VideoFormats = %v, want %v", got.VideoFormats, want)
	}
	if want := []string{"best", "aac", "opus"}; !slices.Equal(got.AudioFormats, want) {
		t.Errorf("AudioFormats = %v, want %v", got.AudioFormats, want)
	}
}

// A host the table does not list is known by its probes alone. What a preset
// cannot name is left out, a container without a codec and a track's key from
// a row's one-menu shape, and "m4a" reads as AAC.
func TestAnUnlistedHostIsKnownByItsProbes(t *testing.T) {
	got := HostFormats("example.org",
		[]string{"best", "mp4 avc1", "avi", "1080p mp4 avc1", "mp4 avc1"},
		[]string{"best", "m4a", "m4a 129k", "best"}, true)
	if !got.Known {
		t.Fatal("a probed host reads as unknown")
	}
	if want := []string{"best", "mp4 avc1"}; !slices.Equal(got.VideoFormats, want) {
		t.Errorf("VideoFormats = %v, want %v", got.VideoFormats, want)
	}
	if want := []string{"best", "aac"}; !slices.Equal(got.AudioFormats, want) {
		t.Errorf("AudioFormats = %v, want %v", got.AudioFormats, want)
	}
}

// A probe that found nothing a preset can name still says so: the host has
// no such format, which is not the same as nothing being known.
func TestAProbeThatFoundNothingToNameIsStillKnowledge(t *testing.T) {
	got := HostFormats("example.org", []string{"best", "avi"}, []string{"best"}, true)
	if !got.Known || !slices.Equal(got.VideoFormats, []string{"best"}) || !slices.Equal(got.AudioFormats, []string{"best"}) {
		t.Errorf("HostFormats = %+v, want a known host that offers best alone", got)
	}
}

func TestAHostNothingIsKnownOfIsOfferedEveryFormat(t *testing.T) {
	got := HostFormats("example.org", nil, nil, false)
	if got.Known {
		t.Error("an unprobed, unlisted host reads as known")
	}
	if !slices.Equal(got.VideoFormats, PresetVideoFormats()) || !slices.Equal(got.AudioFormats, AudioFormats()) {
		t.Errorf("HostFormats = %+v, want the full lists", got)
	}
}

// Every entry has to be something a preset can store and a probe can find,
// or the menu would offer a format that never matches.
func TestEveryKnownSiteNamesFormatsAPresetCanStore(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range knownSites {
		if len(s.domains) == 0 || len(s.video)+len(s.audio) == 0 {
			t.Errorf("site %+v names no domain or no format", s)
		}
		for _, d := range s.domains {
			if d != strings.ToLower(d) || strings.HasPrefix(d, "www.") || !strings.Contains(d, ".") {
				t.Errorf("domain %q is not in the form a task's host takes", d)
			}
			if seen[d] {
				t.Errorf("domain %q is listed twice", d)
			}
			seen[d] = true
		}
		for _, v := range s.video {
			if !IsVideoContainer(v) || (HosterPreset{VideoFormat: v}).Sanitize().VideoFormat != v {
				t.Errorf("%v: video format %q is not one a preset keeps", s.domains, v)
			}
		}
		for _, a := range s.audio {
			if !listedAudioFamily(familyNamed(a, audioFamilies)) || (HosterPreset{AudioFormat: a}).Sanitize().AudioFormat != a {
				t.Errorf("%v: audio format %q is not one a preset keeps", s.domains, a)
			}
		}
	}
}
