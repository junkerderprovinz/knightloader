package ytdlp

import (
	"context"
	"reflect"
	"testing"
)

// TestProbeReadsTheLanguagesTheSourceActuallyOffers is the answer to the
// silent empty subtitle task: the free-text field defaults to "en", the
// source here has no hand-written English track at all, and until this probe
// carried the lists nothing anywhere could say so.
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
	// The two kinds stay apart: a hand-written translation and a
	// speech-to-text pass are not the same product, which is why yt-dlp has
	// two flags for them.
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

// TestAvailableAudioLangsPutsTheOriginalFirst: a menu that buries the
// original track under six machine dubs makes the right choice the hardest
// one to find.
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

// TestAvailableAudioLangsListsALanguageOnce, not once per bitrate: a language
// shows up on every format it is offered in, and a menu repeating "en" five
// times is not a menu.
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

// TestAvailableAudioLangsIgnoresVideoOnlyAndUnnamedTracks: a video-only
// format carries no audio to have a language, and a track the source did not
// name cannot be asked for by name either.
func TestAvailableAudioLangsIgnoresVideoOnlyAndUnnamedTracks(t *testing.T) {
	formats := []FormatEntry{
		{Acodec: "none", Vcodec: "avc1", Language: "en"},
		{Acodec: "mp4a.40.2", Language: ""},
	}
	if got := AvailableAudioLangs(formats); got != nil {
		t.Errorf("AvailableAudioLangs = %+v, want nothing", got)
	}
}

// TestAvailableAudioLangsCountsAProgressiveTrack: a source whose only German
// audio sits inside a combined 360p stream still genuinely offers German, and
// leaving it out of the menu says otherwise.
func TestAvailableAudioLangsCountsAProgressiveTrack(t *testing.T) {
	formats := []FormatEntry{{Vcodec: "avc1.42001E", Acodec: "mp4a.40.2", Language: "de", Height: 360}}
	got := AvailableAudioLangs(formats)
	if len(got) != 1 || got[0].Code != "de" || got[0].Dubbed {
		t.Errorf("AvailableAudioLangs = %+v, want the progressive track's own language", got)
	}
}

// TestAudioLangSaysNothingWhenTheSourceHasNoOpinion: every site that is not
// YouTube ships one audio track and never had a second one to rank it
// against, so calling those "possibly a dub" would put a warning on every
// ordinary video in order to describe one site's feature.
func TestAudioLangSaysNothingWhenTheSourceHasNoOpinion(t *testing.T) {
	got := AvailableAudioLangs([]FormatEntry{{Acodec: "mp4a.40.2", Language: "en"}})
	if len(got) != 1 || got[0].Dubbed {
		t.Errorf("AvailableAudioLangs = %+v, want en reported as an original", got)
	}
}

// TestProbeReadsIsLiveFromEitherField: newer extractors set live_status and
// older ones only the boolean, and reading one of the two would mean the
// guard never arms on half of them.
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

// TestProbeDoesNotCallAFinishedStreamLive: a stream that has ended is an
// ordinary recording with an ordinary length, and putting the recording caps
// on it would stop a normal download at an arbitrary size.
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

// TestProbeOfASourceWithNoLanguageDataStaysEmpty guards the existing helper
// fixtures: a probe result carrying nothing must answer with nothing rather
// than with an empty-string entry that would render as a blank menu row.
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
