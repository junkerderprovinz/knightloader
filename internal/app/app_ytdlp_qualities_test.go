package app

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// probeUpTo720 is a video whose tallest track is 720p, with no 360p.
var probeUpTo720 = []ytdlp.FormatEntry{
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129, Filesize: 3000},
	{FormatID: "251", Ext: "webm", Vcodec: "none", Acodec: "opus", Abr: 141, Filesize: 3300},
	{FormatID: "136", Ext: "mp4", Vcodec: "avc1.4d401f", Acodec: "none", Width: 1280, Height: 720, Filesize: 20000},
	{FormatID: "135", Ext: "mp4", Vcodec: "avc1.4d401e", Acodec: "none", Width: 854, Height: 480, Filesize: 9000},
	{FormatID: "133", Ext: "mp4", Vcodec: "avc1.4d4015", Acodec: "none", Width: 426, Height: 240, Filesize: 4000},
}

var probeAudioOnly = []ytdlp.FormatEntry{
	{FormatID: "http_mp3_128", Ext: "mp3", Vcodec: "none", Acodec: "mp3", Abr: 128, Filesize: 2000},
	{FormatID: "hls_opus_64", Ext: "opus", Vcodec: "none", Acodec: "opus", Abr: 64, FilesizeApprox: 1000},
}

func TestTheQualityMenuListsTheResolutionsTheVideoHasAndNoOthers(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=upto720px01"
	family := putYtdlpFamily(t, a, url, nil)

	a.applyProbeFormats(url, probeUpTo720)

	video := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if want := []string{"best", "720p", "480p", "240p"}; !slices.Equal(video.AvailableQualities, want) {
		t.Errorf("AvailableQualities = %v, want %v", video.AvailableQualities, want)
	}
	audio := snapshot(t, a, family[ytdlp.VariantAudio].ID)
	if want := []string{"aac 129k", "opus 141k"}; !slices.Equal(sorted(audio.AvailableAudioTracks), want) {
		t.Errorf("AvailableAudioTracks = %v, want %v", audio.AvailableAudioTracks, want)
	}
}

func TestAnAudioOnlySourceOffersItsVideoRowBestAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://soundcloud.com/artist/song"
	family := putYtdlpFamily(t, a, url, map[ytdlp.Variant]string{ytdlp.VariantVideo: "1080p"})

	a.applyProbeFormats(url, probeAudioOnly)

	video := snapshot(t, a, family[ytdlp.VariantVideo].ID)
	if want := []string{"best"}; !slices.Equal(video.AvailableQualities, want) {
		t.Errorf("AvailableQualities = %v, want %v", video.AvailableQualities, want)
	}
	if video.Variant != "video:best" {
		t.Errorf("Variant = %q, want video:best; the source has no 1080p", video.Variant)
	}
}

// The extension's menu can be older than what the instance knows, and a cap
// the video lacks would read as a resolution it does not have.
func TestACapTheVideoLacksBecomesTheOneThatDownloadsTheSameTrack(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Up to 720p"
	fake.formats = probeUpTo720
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=capto720p01"
	if _, err := a.AddLinksWithOptions([]string{url}, "", OriginCnL, LinkBatchOptions{Format: "1080p"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the probe to resolve the cap", func() bool {
		for _, x := range tasksSharingURL(a, url) {
			if x.Variant == "video:720p" {
				return true
			}
		}
		return false
	})
}

func TestAFailedProbeLeavesItsReasonOnThePickerRows(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, done := newFakeYtdlp()
	fake.err = errors.New("ERROR: [youtube] gone0000001: Video unavailable")
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=gone0000001"
	a.AddLinks([]string{url}, "")
	<-done
	waitFor(t, "the reason on the video row", func() bool {
		for _, x := range tasksSharingURL(a, url) {
			if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantVideo {
				return x.ProbeError != ""
			}
		}
		return false
	})
	for _, x := range tasksSharingURL(a, url) {
		kind, _ := variantDecode(x.Variant)
		want := ""
		if kind == ytdlp.VariantVideo || kind == ytdlp.VariantAudio {
			want = fake.err.Error()
		}
		if x.ProbeError != want {
			t.Errorf("the %s row's ProbeError = %q, want %q", kind, x.ProbeError, want)
		}
		if len(x.AvailableQualities) != 0 || len(x.AvailableAudioFormats) != 0 {
			t.Errorf("the %s row has menus after a failed probe", kind)
		}
	}

	a.applyProbeFormats(url, probeUpTo720)
	for _, x := range tasksSharingURL(a, url) {
		if x.ProbeError != "" {
			t.Errorf("the %s row keeps %q after a probe answered", x.Variant, x.ProbeError)
		}
	}
}

func TestWithoutYtdlpTheRowsSayWhyTheyOfferBestAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://youtube.com/watch?v=noytdlp0001"
	family := putYtdlpFamily(t, a, url, nil)

	a.backfillYtdlpProbes()

	for _, v := range []ytdlp.Variant{ytdlp.VariantVideo, ytdlp.VariantAudio} {
		if got := snapshot(t, a, family[v].ID); got.ProbeError == "" {
			t.Errorf("the %s row has no reason", v)
		}
	}
}

// The extension's menu asks first and the link arrives a moment later; both
// read the one yt-dlp answer.
func TestTheExtensionsQuestionAndTheStagedLinkShareOneProbe(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	var asked []string
	var mu sync.Mutex
	wireYtdlp(a, countingYtdlpBackend{formats: probeUpTo720, mu: &mu, asked: &asked})

	const url = "https://youtube.com/watch?v=shared00001"
	for range 2 {
		menus, err := a.LinkQualities(t.Context(), url)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"720p", "480p", "240p"}; !slices.Equal(menus.Qualities, want) || !menus.Audio {
			t.Errorf("LinkQualities = %+v, want %v with audio", menus, want)
		}
	}
	a.AddLinks([]string{url}, "")
	waitFor(t, "the staged rows' menus", func() bool {
		for _, x := range tasksSharingURL(a, url) {
			if len(x.AvailableQualities) > 0 {
				return true
			}
		}
		return false
	})
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 {
		t.Errorf("yt-dlp was asked %d times, want once", len(asked))
	}
}

func TestAnAudioOnlySourceOffersTheExtensionNoResolution(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: probeAudioOnly, mu: &sync.Mutex{}, asked: &asked})

	menus, err := a.LinkQualities(t.Context(), "https://soundcloud.com/artist/other")
	if err != nil {
		t.Fatal(err)
	}
	if len(menus.Qualities) != 0 || !menus.Audio {
		t.Errorf("LinkQualities = %+v, want no resolution and audio", menus)
	}
}

func TestAFailedProbeGivesTheExtensionItsReason(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.err = errors.New("ERROR: [youtube] gone0000002: Private video")
	wireYtdlp(a, fake)

	if _, err := a.LinkQualities(t.Context(), "https://youtube.com/watch?v=gone0000002"); err == nil || err.Error() != fake.err.Error() {
		t.Errorf("LinkQualities error = %v, want %v", err, fake.err)
	}
}

func TestOnlyALinkYtdlpHandlesIsProbedForTheExtension(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	var asked []string
	wireYtdlp(a, countingYtdlpBackend{formats: probeUpTo720, mu: &sync.Mutex{}, asked: &asked})

	for _, u := range []string{"file:///etc/passwd", "https://example.org/some.zip", ""} {
		if _, err := a.LinkQualities(t.Context(), u); !errors.Is(err, ErrNotAMediaLink) {
			t.Errorf("LinkQualities(%q) error = %v, want ErrNotAMediaLink", u, err)
		}
	}
	if len(asked) != 0 {
		t.Errorf("yt-dlp was asked about %v", asked)
	}
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
