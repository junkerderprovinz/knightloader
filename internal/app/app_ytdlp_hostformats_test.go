package app

import (
	"slices"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A host the site table does not know is offered what probes of its links
// found, and only once one has answered.
func TestAHostsPresetOffersWhatItsLinksWereFoundToHave(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const host = "media.example.org"
	if got := a.HosterFormats(host); got.Known || !slices.Equal(got.AudioFormats, ytdlp.AudioFormats()) {
		t.Fatalf("before any probe HosterFormats = %+v, want every format and nothing known", got)
	}

	a.applyProbeFormats("https://"+host+"/v/1", testProbeFormats)

	got := a.HosterFormats("www." + host)
	if !got.Known {
		t.Fatal("the probed host still reads as unknown")
	}
	if want := []string{"best", "mp4 avc1"}; !slices.Equal(got.VideoFormats, want) {
		t.Errorf("VideoFormats = %v, want %v", got.VideoFormats, want)
	}
	if want := []string{"best", "aac"}; !slices.Equal(got.AudioFormats, want) {
		t.Errorf("AudioFormats = %v, want %v", got.AudioFormats, want)
	}
	if other := a.HosterFormats("other.example.org"); other.Known {
		t.Errorf("another host took this one's probe: %+v", other)
	}
}

// The kept format lists do not survive a restart, but the rows' menus do, a
// finished row's included.
func TestAHostsRowsSpeakForItAfterARestart(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const host = "media.example.org"
	putTask(t, a, core.Task{
		URL: "https://" + host + "/v/2", Host: host, Status: core.StatusDone, Variant: "video:1080p webm vp9",
		AvailableVideoFormats: []string{"best", "webm vp9"},
	})
	putTask(t, a, core.Task{
		URL: "https://" + host + "/v/2", Host: host, Status: core.StatusDone, Variant: "audio:opus",
		AvailableAudioFormats: []string{"best", "opus"},
	})
	// A plain download from the same host says nothing about its media.
	putTask(t, a, core.Task{URL: "https://" + host + "/file.zip", Host: host, Status: core.StatusDone})

	got := a.HosterFormats(host)
	if !got.Known || !slices.Equal(got.VideoFormats, []string{"best", "webm vp9"}) || !slices.Equal(got.AudioFormats, []string{"best", "opus"}) {
		t.Errorf("HosterFormats = %+v, want webm vp9 and opus from the rows", got)
	}
}

// YouTube is known before any of its links is pasted, and a probe adds what
// the table lacks rather than replacing it.
func TestAKnownSiteIsOfferedItsFormatsWithoutAProbe(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	got := a.HosterFormats("youtube.com")
	if !got.Known || !slices.Equal(got.AudioFormats, []string{"best", "aac", "opus"}) {
		t.Fatalf("HosterFormats(youtube.com) = %+v, want YouTube's aac and opus", got)
	}
	a.applyProbeFormats("https://youtube.com/watch?v=hostfmt0001", []ytdlp.FormatEntry{
		{FormatID: "x", Ext: "mp4", Vcodec: "hvc1.1.6.L120.90", Acodec: "none", Height: 1080},
	})
	got = a.HosterFormats("youtube.com")
	if !slices.Contains(got.VideoFormats, "mp4 hevc") || !slices.Contains(got.VideoFormats, "webm vp9") {
		t.Errorf("VideoFormats = %v, want the probe's mp4 hevc beside the table's formats", got.VideoFormats)
	}
}

// A preset can name a format its host does not serve, one saved while the
// menus were the full lists. It is kept, and a new link still starts with it:
// the audio is converted to it, and the video takes the quality alone.
func TestAPresetNamingAFormatItsHostLacksKeepsWorking(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Some Video"
	fake.formats = youtubeProbe
	wireYtdlp(a, fake)
	if err := a.SetHosterPreset("youtube.com", ytdlp.HosterPreset{
		Variants:    ytdlp.Variants(),
		VideoFormat: "webm av1", Quality: ytdlp.Quality1080p,
		AudioFormat: "mp3", AudioBitrate: "192",
	}); err != nil {
		t.Fatal(err)
	}
	menus := a.HosterFormats("youtube.com")
	if slices.Contains(menus.VideoFormats, "webm av1") || slices.Contains(menus.AudioFormats, "mp3") {
		t.Fatalf("YouTube's menus %+v list the preset's formats, so this test proves nothing", menus)
	}

	const url = "https://youtube.com/watch?v=hostfmt0002"
	a.AddLinks([]string{url}, "")
	waitFor(t, "the probe to resolve the video row's pick", func() bool {
		return len(tasksSharingURL(a, url)) == 5 && rowByKind(t, a, url, ytdlp.VariantVideo).Variant == "video:1080p"
	})

	audio := rowByKind(t, a, url, ytdlp.VariantAudio)
	if audio.Variant != "audio:mp3" || audio.AudioBitrate != "192" || audio.Ext != "mp3" {
		t.Errorf("audio row = %q at %q, Ext %q; want the preset's mp3 at 192", audio.Variant, audio.AudioBitrate, audio.Ext)
	}
	if o := a.ytdlpOptionsForTask(audio.ID); o.AudioFormat != "mp3" || o.AudioBitrate != "192" {
		t.Errorf("yt-dlp is asked for %q at %q, want mp3 at 192", o.AudioFormat, o.AudioBitrate)
	}
	if p := a.HosterPresetFor("youtube.com"); p.VideoFormat != "webm av1" || p.AudioFormat != "mp3" || p.AudioBitrate != "192" {
		t.Errorf("the preset reads %+v, want its own formats kept", p)
	}
}

// A row whose menu calls AAC by its file's name is probed again at boot, so
// its pickers and tracks read "aac".
func TestBackfillRenamesAnM4aMenu(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=backfill04"
	family := putYtdlpFamily(t, a, url, nil)
	editTask(a, family[ytdlp.VariantVideo].ID, func(x *core.Task) { x.AvailableVideoFormats = []string{"best", "mp4 avc1"} })
	editTask(a, family[ytdlp.VariantAudio].ID, func(x *core.Task) {
		x.AvailableAudioFormats = []string{"best", "m4a"}
		x.AvailableAudioTracks = []string{"m4a 129k"}
	})

	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: testProbeFormats, mu: &sync.Mutex{}, asked: &asked})
	a.backfillYtdlpProbes()

	if len(asked) != 1 {
		t.Fatalf("the backfill asked about %v, want one probe for %q", asked, url)
	}
	audio := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if !slices.Equal(audio.AvailableAudioFormats, []string{"best", "aac"}) || !slices.Equal(audio.AvailableAudioTracks, []string{"aac 129k"}) {
		t.Errorf("audio menus = %v and %v, want aac and aac 129k", audio.AvailableAudioFormats, audio.AvailableAudioTracks)
	}
}
