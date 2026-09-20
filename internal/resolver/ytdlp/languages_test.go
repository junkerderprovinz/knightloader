package ytdlp

import (
	"context"
	"reflect"
	"testing"
)

// The source has English only as automatic captions, not as a manual track.
func TestProbeReadsTheLanguagesTheSourceActuallyOffers(t *testing.T) {
	b := fakeYtdlpBackend(t, "languages")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/v")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	tracks := AvailableSubtitleLangs(res)
	if !reflect.DeepEqual(tracks.Manual, []string{"de", "fr"}) {
		t.Errorf("manual = %v, want [de fr] sorted", tracks.Manual)
	}
	if !reflect.DeepEqual(tracks.Auto, []string{"de", "en", "es"}) {
		t.Errorf("auto = %v, want [de en es] sorted", tracks.Auto)
	}
	if tracks.Has("en") != true {
		t.Errorf("Has(en) = false - English is on offer, as an automatic caption")
	}
	if contains(tracks.Manual, "en") {
		t.Errorf("English leaked into the hand-written list: %v", tracks.Manual)
	}
	if tracks.Has("ja") {
		t.Errorf("Has(ja) = true for a language this source does not carry")
	}
}

func TestProbeReadsTheAnnouncedDuration(t *testing.T) {
	b := fakeYtdlpBackend(t, "languages")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/v")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if res.Duration != 2718.041 {
		t.Errorf("Duration = %v, want the announced 2718.041", res.Duration)
	}
}

func TestAvailableAudioLangsPutsTheOriginalFirst(t *testing.T) {
	b := fakeYtdlpBackend(t, "languages")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/v")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	got := AvailableAudioLangs(res.Formats)
	want := []AudioLang{{Code: "de"}, {Code: "en", Dubbed: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AvailableAudioLangs = %+v, want %+v", got, want)
	}
}

func TestAvailableAudioLangsListsALanguageOnce(t *testing.T) {
	formats := []FormatEntry{
		{Acodec: "mp4a.40.2", Language: "en", LanguagePreference: 10},
		{Acodec: "opus", Language: "en", LanguagePreference: 10},
		{Acodec: "mp4a.40.5", Language: "en", LanguagePreference: 10},
	}
	if got := AvailableAudioLangs(formats); len(got) != 1 || got[0].Code != "en" {
		t.Errorf("AvailableAudioLangs = %+v, want one entry for en", got)
	}
}

func TestAvailableAudioLangsIgnoresVideoOnlyAndUnnamedTracks(t *testing.T) {
	formats := []FormatEntry{
		{Acodec: "none", Vcodec: "avc1", Language: "en"},
		{Acodec: "mp4a.40.2", Language: ""},
	}
	if got := AvailableAudioLangs(formats); got != nil {
		t.Errorf("AvailableAudioLangs = %+v, want nothing", got)
	}
}

func TestAvailableAudioLangsCountsAProgressiveTrack(t *testing.T) {
	formats := []FormatEntry{{Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Language: "de", Height: 360}}
	got := AvailableAudioLangs(formats)
	if len(got) != 1 || got[0].Code != "de" || got[0].Dubbed {
		t.Errorf("AvailableAudioLangs = %+v, want the progressive track's own language", got)
	}
}

// Sites with a single audio track report no preference.
func TestAudioLangSaysNothingWhenTheSourceHasNoOpinion(t *testing.T) {
	got := AvailableAudioLangs([]FormatEntry{{Acodec: "mp4a.40.2", Language: "en"}})
	if len(got) != 1 || got[0].Dubbed {
		t.Errorf("AvailableAudioLangs = %+v, want en reported as an original", got)
	}
}

// Newer extractors set live_status, older ones only is_live.
func TestProbeReadsIsLiveFromEitherField(t *testing.T) {
	b := fakeYtdlpBackend(t, "live")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/live")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if !res.IsLive {
		t.Errorf("IsLive = false for a stream whose live_status says is_live")
	}
}

func TestProbeDoesNotCallAFinishedStreamLive(t *testing.T) {
	b := fakeYtdlpBackend(t, "waslive")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/was")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if res.IsLive {
		t.Errorf("IsLive = true for was_live - a finished stream is not a stream")
	}
	if res.Duration != 7200 {
		t.Errorf("Duration = %v, want the finished stream's real length", res.Duration)
	}
}

func TestProbeOfASourceWithNoLanguageDataStaysEmpty(t *testing.T) {
	b := fakeYtdlpBackend(t, "title")
	res, err := b.ProbeTitle(context.Background(), "https://example.invalid/v")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	tracks := AvailableSubtitleLangs(res)
	if tracks.Manual != nil || tracks.Auto != nil {
		t.Errorf("tracks = %+v, want both lists empty", tracks)
	}
	if res.IsLive || res.Duration != 0 {
		t.Errorf("res = live:%v duration:%v, want neither claimed", res.IsLive, res.Duration)
	}
}
