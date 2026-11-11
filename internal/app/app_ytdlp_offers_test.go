package app

import (
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// probeWithOffers is a source with two containers at 1080p, audio in two
// languages, subtitles by hand and generated, pictures and a description.
var probeWithOffers = ytdlp.ProbeResult{
	Formats: []ytdlp.FormatEntry{
		{FormatID: "303", Ext: "webm", Vcodec: "vp9", Acodec: "none", Width: 1920, Height: 1080, FPS: 60},
		{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Width: 1920, Height: 1080, FPS: 30},
		{FormatID: "136", Ext: "mp4", Vcodec: "avc1.4d401f", Acodec: "none", Width: 1280, Height: 720, FPS: 30},
		{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 160, Language: "en", LanguagePreference: 10},
		{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 128, Language: "en", LanguagePreference: 10},
		{FormatID: "251-1", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 130, Language: "de", LanguagePreference: -1},
	},
	Subtitles:       []string{"de"},
	AutoCaptions:    []string{"fr", "en-orig"},
	SubtitleFormats: []string{"srv3", "vtt"},
	Thumbnails:      []ytdlp.Thumbnail{{ID: "0", Width: 120, Height: 90, Ext: "jpg"}, {ID: "41", Width: 1280, Height: 720, Ext: "webp"}},
	Description:     true,
}

func offersApp(t *testing.T) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Offers"
	fake.offers = probeWithOffers
	wireYtdlp(a, fake)
	return a
}

// The extension's menu lists, for every part, what this video has and
// nothing else.
func TestTheExtensionsMenuListsWhatTheHostOffersForEveryPart(t *testing.T) {
	a := offersApp(t)
	m, err := a.LinkQualities(t.Context(), "https://youtube.com/watch?v=offers00001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1080p60 webm vp9", "1080p mp4 avc1", "720p mp4 avc1"}; !slices.Equal(m.VideoTracks, want) {
		t.Errorf("video tracks = %v, want %v", m.VideoTracks, want)
	}
	if want := []string{"opus 160k", "opus 130k", "aac 128k"}; !slices.Equal(m.AudioTracks, want) {
		t.Errorf("audio tracks = %v, want %v", m.AudioTracks, want)
	}
	if want := []LinkLang{{Code: "en"}, {Code: "de", Dubbed: true}}; !slices.Equal(m.AudioLangs, want) {
		t.Errorf("audio languages = %+v, want %+v", m.AudioLangs, want)
	}
	if !slices.Equal(m.Subtitles, []string{"de"}) || !slices.Equal(m.AutoCaptions, []string{"en-orig", "fr"}) {
		t.Errorf("subtitles = %v by hand and %v generated, want de and en-orig, fr", m.Subtitles, m.AutoCaptions)
	}
	if !slices.Equal(m.SubtitleFormats, []string{"srv3", "vtt"}) {
		t.Errorf("subtitle formats = %v", m.SubtitleFormats)
	}
	if m.Thumbnail == nil || m.Thumbnail.Width != 1280 || m.Thumbnail.Height != 720 || m.Thumbnail.Ext != "webp" {
		t.Fatalf("thumbnail = %+v, want the 1280x720 webp yt-dlp fetches", m.Thumbnail)
	}
	if want := []string{"jpg", "png", "webp"}; !slices.Equal(m.Thumbnail.Formats, want) {
		t.Errorf("thumbnail formats = %v, want %v", m.Thumbnail.Formats, want)
	}
	if !m.Description {
		t.Error("the description is missing from the menu")
	}
}

// The menu says how large each pick downloads, a video track with the audio
// merged into it, and says nothing where the host gave no size.
func TestTheExtensionsMenuSaysHowLargeEachPickIs(t *testing.T) {
	sized := probeWithOffers
	sized.Formats = slices.Clone(probeWithOffers.Formats)
	bytes := map[string]int64{"303": 400_000_000, "137": 300_000_000, "251": 4_000_000, "140": 3_000_000}
	for i := range sized.Formats {
		sized.Formats[i].Filesize = bytes[sized.Formats[i].FormatID]
	}
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Offers"
	fake.offers = sized
	wireYtdlp(a, fake)

	m, err := a.LinkQualities(t.Context(), "https://youtube.com/watch?v=offers00004")
	if err != nil {
		t.Fatal(err)
	}
	for pick, want := range map[string]int64{"1080p60 webm vp9": 404_000_000, "1080p mp4 avc1": 303_000_000} {
		if got := m.VideoSizes[pick]; got != want {
			t.Errorf("the video pick %q is %d bytes, want %d", pick, got, want)
		}
	}
	for pick, want := range map[string]int64{"opus 160k": 4_000_000, "aac 128k": 3_000_000} {
		if got := m.AudioSizes[pick]; got != want {
			t.Errorf("the audio pick %q is %d bytes, want %d", pick, got, want)
		}
	}
	if _, ok := m.VideoSizes["720p mp4 avc1"]; ok {
		t.Error("a track the host gave no size for has one in the menu")
	}
	if _, ok := m.AudioSizes["opus 130k"]; ok {
		t.Error("an audio track the host gave no size for has one in the menu")
	}
}

// An HDR track is offered under a key of its own beside the plain track of
// its height, each with the size of what it downloads.
func TestTheExtensionsMenuOffersAnHDRTrackApart(t *testing.T) {
	hdr := ytdlp.ProbeResult{Formats: []ytdlp.FormatEntry{
		{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 135, Filesize: 4_000_000},
		{FormatID: "303", Ext: "webm", Vcodec: "vp9", Acodec: "none", Width: 1920, Height: 1080, FPS: 60, DynamicRange: "SDR", Filesize: 300_000_000},
		{FormatID: "335", Ext: "webm", Vcodec: "vp9.2", Acodec: "none", Width: 1920, Height: 1080, FPS: 60, DynamicRange: "HDR10", Filesize: 900_000_000},
	}}
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "HDR"
	fake.offers = hdr
	wireYtdlp(a, fake)

	m, err := a.LinkQualities(t.Context(), "https://youtube.com/watch?v=offershdr01")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1080p60hdr webm vp9", "1080p60 webm vp9"}; !slices.Equal(m.VideoTracks, want) {
		t.Errorf("video tracks = %v, want %v", m.VideoTracks, want)
	}
	for pick, want := range map[string]int64{"1080p60hdr webm vp9": 904_000_000, "1080p60 webm vp9": 304_000_000} {
		if got := m.VideoSizes[pick]; got != want {
			t.Errorf("the video pick %q is %d bytes, want %d", pick, got, want)
		}
	}
}

// The best quality and the highest resolution download the same tracks on a
// YouTube video, so the menu gives them the same size.
func TestTheBestQualityAndTheHighestResolutionWeighTheSame(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Offers"
	fake.offers = ytdlp.ProbeResult{Formats: youtubeProbe}
	wireYtdlp(a, fake)
	m, err := a.LinkQualities(t.Context(), "https://youtube.com/watch?v=offers00005")
	if err != nil {
		t.Fatal(err)
	}
	top := m.Qualities[0]
	if m.VideoSizes["best"] == 0 || m.VideoSizes["best"] != m.VideoSizes[top] {
		t.Errorf("best weighs %d bytes and %s %d, want the same", m.VideoSizes["best"], top, m.VideoSizes[top])
	}
}

// A video sent with exact picks stages rows that download exactly those, as
// the host serves them.
func TestAVideoSentWithExactPicksDownloadsExactlyThose(t *testing.T) {
	a := offersApp(t)
	const url = "https://youtube.com/watch?v=offers00002"
	if _, err := a.AddLinksWithOptions([]string{url}, "", OriginCnL, LinkBatchOptions{Choice: &ytdlp.LinkChoice{
		Variants:   []ytdlp.Variant{ytdlp.VariantVideo, ytdlp.VariantAudio, ytdlp.VariantSubtitle, ytdlp.VariantThumbnail},
		VideoTrack: "1080p60 webm vp9", AudioTrack: "opus 130k", AudioLang: "de",
		SubtitleLangs: []string{"de", "en-orig"}, SubtitleAuto: true, SubtitleFormat: "vtt",
	}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the probed rows", func() bool {
		for _, x := range tasksSharingURL(a, url) {
			if len(x.AvailableVideoTracks) > 0 {
				return true
			}
		}
		return false
	})
	for _, x := range fiveRows(t, a, url) {
		kind, _ := variantDecode(x.Variant)
		o := a.ytdlpOptionsForTask(x.ID)
		switch kind {
		case ytdlp.VariantVideo:
			if o.VideoPick != "1080p60 webm vp9" {
				t.Errorf("the video row picks %q", o.VideoPick)
			}
		case ytdlp.VariantAudio:
			if o.AudioTrack != "opus 130k" || o.AudioLang != "de" {
				t.Errorf("the audio row picks %q in %q, want opus 130k in de", o.AudioTrack, o.AudioLang)
			}
		case ytdlp.VariantSubtitle:
			if o.SubtitleLangs != "de,en-orig" || !o.SubtitleAuto || o.SubtitleFormat != "vtt" {
				t.Errorf("the subtitle row asks for %q (generated %v) as %q", o.SubtitleLangs, o.SubtitleAuto, o.SubtitleFormat)
			}
			if x.Ext != "vtt" {
				t.Errorf("the subtitle row shows %q, want vtt", x.Ext)
			}
		case ytdlp.VariantThumbnail:
			if !o.ThumbnailAsIs {
				t.Error("the thumbnail is converted, want it as the host serves it")
			}
			if x.Ext == "jpg" {
				t.Error("the thumbnail row promises a jpg it is not converted to")
			}
		}
	}
}

// A thumbnail sent with a format of its own is converted to it, and one sent
// with a format yt-dlp cannot write is kept as the host serves it.
func TestAThumbnailSentWithAFormatComesInThatFormat(t *testing.T) {
	a := offersApp(t)
	for _, c := range []struct {
		url, format string
		asIs        bool
	}{
		{"https://youtube.com/watch?v=offers00005", "png", false},
		{"https://youtube.com/watch?v=offers00006", "jpg", false},
		{"https://youtube.com/watch?v=offers00007", "avif", true},
	} {
		if _, err := a.AddLinksWithOptions([]string{c.url}, "", OriginCnL, LinkBatchOptions{Choice: &ytdlp.LinkChoice{
			Variants: []ytdlp.Variant{ytdlp.VariantThumbnail}, ThumbnailFormat: c.format,
		}}); err != nil {
			t.Fatal(err)
		}
		for _, x := range fiveRows(t, a, c.url) {
			if kind, _ := variantDecode(x.Variant); kind != ytdlp.VariantThumbnail {
				continue
			}
			o := a.ytdlpOptionsForTask(x.ID)
			if x.Ext != c.format || o.ThumbnailAsIs != c.asIs || (!c.asIs && o.ThumbnailFormat != c.format) {
				t.Errorf("%s: the thumbnail row shows %q and asks for %q (as served %v)", c.format, x.Ext, o.ThumbnailFormat, o.ThumbnailAsIs)
			}
		}
	}
}

// A choice of subtitles that names no format takes the host's own, and a
// paste keeps converting to srt.
func TestSubtitlesChosenWithoutAFormatKeepTheHostsOwn(t *testing.T) {
	a := offersApp(t)
	const sent, pasted = "https://youtube.com/watch?v=offers00003", "https://youtube.com/watch?v=offers00004"
	if _, err := a.AddLinksWithOptions([]string{sent}, "", OriginCnL, LinkBatchOptions{Choice: &ytdlp.LinkChoice{
		Variants: []ytdlp.Variant{ytdlp.VariantSubtitle}, SubtitleLangs: []string{"de"},
	}}); err != nil {
		t.Fatal(err)
	}
	a.AddLinks([]string{pasted}, "")
	for url, want := range map[string]string{sent: "best", pasted: ""} {
		for _, x := range fiveRows(t, a, url) {
			if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantSubtitle {
				if got := a.ytdlpOptionsForTask(x.ID).SubtitleFormat; got != want {
					t.Errorf("%s: the subtitle row asks for format %q, want %q", url, got, want)
				}
			}
		}
	}
}
