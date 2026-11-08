package ytdlp

import (
	"slices"
	"testing"
)

// upTo720 is a source whose tallest track is 720p, with no 360p and no 144p.
var upTo720 = []FormatEntry{
	{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129, Filesize: 3000},
	{FormatID: "136", Ext: "mp4", Vcodec: "avc1.4d401f", Acodec: "none", Width: 1280, Height: 720, Filesize: 20000},
	{FormatID: "135", Ext: "mp4", Vcodec: "avc1.4d401e", Acodec: "none", Width: 854, Height: 480, Filesize: 9000},
	{FormatID: "133", Ext: "mp4", Vcodec: "avc1.4d4015", Acodec: "none", Width: 426, Height: 240, Filesize: 4000},
}

var audioOnly = []FormatEntry{
	{FormatID: "http_mp3_128", Ext: "mp3", Vcodec: "none", Acodec: "mp3", Abr: 128, Filesize: 2000},
	{FormatID: "hls_opus_64", Ext: "opus", Vcodec: "none", Acodec: "opus", Abr: 64, FilesizeApprox: 1000, Protocol: "m3u8_native"},
}

func TestASourceOffersTheResolutionsItHasAndNoOthers(t *testing.T) {
	if got, want := SourceQualities(upTo720), []Quality{Quality720p, Quality480p, Quality240p}; !slices.Equal(got, want) {
		t.Errorf("SourceQualities = %v, want %v", got, want)
	}
	if got, want := SourceQualities(youtubeLike), []Quality{Quality2160p, Quality1080p, Quality360p}; !slices.Equal(got, want) {
		t.Errorf("SourceQualities(youtubeLike) = %v, want %v", got, want)
	}
}

func TestAnAudioOnlySourceOffersNoResolution(t *testing.T) {
	if got := SourceQualities(audioOnly); len(got) != 0 {
		t.Errorf("SourceQualities = %v, want none", got)
	}
}

// YouTube calls a 1920x800 film 1080p, and the 1080p cap is the one that
// downloads it.
func TestATrackBetweenTwoStepsIsOfferedUnderTheStepAboveIt(t *testing.T) {
	wide := []FormatEntry{
		{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129},
		{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Width: 1920, Height: 800},
		{FormatID: "136", Ext: "mp4", Vcodec: "avc1.4d401f", Acodec: "none", Width: 1280, Height: 534},
	}
	if got, want := SourceQualities(wide), []Quality{Quality1080p, Quality720p}; !slices.Equal(got, want) {
		t.Errorf("SourceQualities = %v, want %v", got, want)
	}
}

// yt-dlp's bestvideo takes video-only tracks alone while there is audio to
// merge, so a 360p track that carries its own sound is never what a cap
// downloads, and 360p is not offered.
func TestAResolutionOnlyACombinedTrackHasIsNotOfferedBesideVideoOnlyTracks(t *testing.T) {
	formats := []FormatEntry{
		{FormatID: "160", Ext: "mp4", Vcodec: "avc1.4d400b", Acodec: "none", Height: 144},
		{FormatID: "137", Ext: "mp4", Vcodec: "avc1.640028", Acodec: "none", Height: 1080},
		{FormatID: "140", Ext: "m4a", Vcodec: "none", Acodec: "mp4a.40.2", Abr: 129},
		{FormatID: "18", Ext: "mp4", Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Height: 360},
	}
	if got, want := SourceQualities(formats), []Quality{Quality1080p, Quality144p}; !slices.Equal(got, want) {
		t.Errorf("SourceQualities = %v, want %v", got, want)
	}
}

func TestACapTheSourceLacksResolvesToTheOneThatDownloadsTheSameTrack(t *testing.T) {
	cases := []struct {
		pick    string
		formats []FormatEntry
		want    string
	}{
		{"1080p", upTo720, "720p"},
		{"2160p", upTo720, "720p"},
		{"360p", upTo720, "240p"},
		{"720p", upTo720, "720p"},
		// Below every track a cap finds nothing at all, so it takes the smallest.
		{"144p", upTo720, "240p"},
		{"1080p", audioOnly, "best"},
		{"best", upTo720, "best"},
		{"custom", upTo720, "custom"},
	}
	for _, c := range cases {
		if got := ResolveVideoPick(c.pick, c.formats); got != c.want {
			t.Errorf("ResolveVideoPick(%q) = %q, want %q", c.pick, got, c.want)
		}
	}
}

// A preset's wish whose format the source lacks falls back to its cap, and the
// cap to what the source has.
func TestAnUnmetWishFallsBackToACapTheSourceHas(t *testing.T) {
	if got := ResolveVideoPick("webm vp9 1080p", upTo720); got != "720p" {
		t.Errorf("ResolveVideoPick = %q, want 720p", got)
	}
}
