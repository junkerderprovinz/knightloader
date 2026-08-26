package ytdlp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFoldsUnknownQualityOntoBest(t *testing.T) {
	got := Options{Quality: "8k-hdr-please"}.Sanitize()
	if got.Quality != QualityBest {
		t.Errorf("Quality = %q, want %q for an unrecognised value", got.Quality, QualityBest)
	}
}

func TestSanitizeKeepsEveryKnownQuality(t *testing.T) {
	for _, q := range Qualities() {
		if got := (Options{Quality: q}).Sanitize().Quality; got != q {
			t.Errorf("Sanitize() folded known quality %q onto %q", q, got)
		}
	}
}

func TestSanitizeFoldsUnknownAudioBitrateOntoEmpty(t *testing.T) {
	got := Options{AudioBitrate: "1337"}.Sanitize()
	if got.AudioBitrate != "" {
		t.Errorf("AudioBitrate = %q, want %q for an unrecognised value", got.AudioBitrate, "")
	}
}

func TestSanitizeKeepsEveryKnownAudioBitrate(t *testing.T) {
	for _, b := range AudioBitrates() {
		if got := (Options{AudioBitrate: b}).Sanitize().AudioBitrate; got != b {
			t.Errorf("Sanitize() folded known bitrate %q onto %q", b, got)
		}
	}
}

// TestAvailableAudioFormatsKeepsOnlyNativeCodecsPlusBest is [87]/[88]'s own
// audio case (jdp, 2026-08-26: "bei der audio spur sollen nur die formate
// angezeigt werden die wirklich von hoster angeboten werden. Youtube bietet
// zb keine flac audio"): a source offering opus and AAC audio-only tracks
// gets "opus"/"m4a" plus the always-kept "best" - mp3/wav/flac, none of
// which the source actually has, do not appear even though ffmpeg could
// technically transcode to any of them.
func TestAvailableAudioFormatsKeepsOnlyNativeCodecsPlusBest(t *testing.T) {
	got := AvailableAudioFormats([]string{"opus", "mp4a.40.2", "opus"})
	// AudioFormats()'s own menu order (best, mp3, m4a, opus, wav, flac), not
	// the order the codecs were passed in - the point of filtering against
	// that menu rather than building a fresh list is that the result reads
	// like the ordinary, unfiltered menu with entries missing, not a
	// differently-ordered one.
	want := []string{"best", "m4a", "opus"}
	if len(got) != len(want) {
		t.Fatalf("AvailableAudioFormats = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AvailableAudioFormats = %v, want %v", got, want)
			break
		}
	}
}

// TestAvailableAudioFormatsWithNoRecognisedCodecKeepsOnlyBest is the "no
// data" floor: a codec audioFormatForCodec does not recognise (vorbis,
// ac-3...) contributes nothing rather than a guess, so only "best" - which
// names no codec of its own - survives.
func TestAvailableAudioFormatsWithNoRecognisedCodecKeepsOnlyBest(t *testing.T) {
	got := AvailableAudioFormats([]string{"vorbis", "ac-3"})
	if len(got) != 1 || got[0] != "best" {
		t.Errorf("AvailableAudioFormats = %v, want [best]", got)
	}
}

// TestAvailableAudioBitratesCapsAtTheSourceOwnBestTrack is [87]/[88]'s own
// bitrate case (jdp, 2026-08-26: "alle formate immer auf hosterangebot
// begrenzen. auch die audioqualitäten!"): a source whose best audio track
// reports 130kbit/s keeps every menu entry at or under that (Auto/64/96/
// 128) and drops the higher presets (160/192/256/320), which would only
// ever promise more than the source has to give.
func TestAvailableAudioBitratesCapsAtTheSourceOwnBestTrack(t *testing.T) {
	got := AvailableAudioBitrates(130)
	want := []string{"", "64", "96", "128"}
	if len(got) != len(want) {
		t.Fatalf("AvailableAudioBitrates(130) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AvailableAudioBitrates(130) = %v, want %v", got, want)
			break
		}
	}
}

// TestAvailableAudioBitratesUnfilteredWithNoData is the "nothing probed
// yet" floor every AvailableX function in this package shares.
func TestAvailableAudioBitratesUnfilteredWithNoData(t *testing.T) {
	got := AvailableAudioBitrates(0)
	want := AudioBitrates()
	if len(got) != len(want) {
		t.Fatalf("AvailableAudioBitrates(0) = %v, want the full menu %v", got, want)
	}
}

func TestSanitizeTrimsFreeText(t *testing.T) {
	got := Options{
		CustomFormat:  "  bestvideo+bestaudio  ",
		SubtitleLangs: "  en,de  ",
	}.Sanitize()
	if got.CustomFormat != "bestvideo+bestaudio" {
		t.Errorf("CustomFormat = %q, want trimmed", got.CustomFormat)
	}
	if got.SubtitleLangs != "en,de" {
		t.Errorf("SubtitleLangs = %q, want trimmed", got.SubtitleLangs)
	}
}

func TestSanitizeClipsPathologicalFreeText(t *testing.T) {
	huge := strings.Repeat("a", maxFieldLen*4)
	got := Options{CustomFormat: huge}.Sanitize()
	if len(got.CustomFormat) != maxFieldLen {
		t.Errorf("CustomFormat length = %d, want %d", len(got.CustomFormat), maxFieldLen)
	}
}

// TestSanitizeTemplateStripsTraversal pins the exact rewriting: every ".."
// TOKEN is dropped, wherever it sits, and everything else survives
// untouched. Dropping only the token - not the rest of the template after
// it - is deliberate: "%(title)s/../../../etc/passwd" becoming
// "%(title)s/etc/passwd" is still fully contained once joined onto the task
// directory (see TestSanitizeTemplateNeverEscapesTheDirectoryItIsJoinedOnto
// below for the actual guarantee that matters), and it keeps as much of a
// person's real template as it safely can rather than discarding the whole
// tail over one bad segment.
func TestSanitizeTemplateStripsTraversal(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"%(title)s.%(ext)s":                  "%(title)s.%(ext)s",
		"%(uploader)s/%(title)s.%(ext)s":     "%(uploader)s/%(title)s.%(ext)s",
		"../../../etc/passwd":                "etc/passwd",
		"%(title)s/../../../etc/passwd":      "%(title)s/etc/passwd",
		"..":                                 "",
		"../..":                              "",
		"/etc/passwd":                        "etc/passwd",
		`..\..\windows\system32\%(title)s`:   "windows/system32/%(title)s",
		"  %(playlist)s/%(title)s.%(ext)s  ": "%(playlist)s/%(title)s.%(ext)s",
	}
	for in, want := range cases {
		if got := sanitizeTemplate(in); got != want {
			t.Errorf("sanitizeTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeTemplateNeverEscapesTheDirectoryItIsJoinedOnto is the
// guarantee buildArgs actually depends on, checked the way it actually
// matters: join the sanitized result onto an absolute directory and confirm
// the outcome is still inside it. This is what makes the "drop only the
// token" choice above safe - a leftover "etc/passwd" is fine precisely
// because filepath.Join can only ever relocate it under dir, never above
// it, once every ".." is gone.
func TestSanitizeTemplateNeverEscapesTheDirectoryItIsJoinedOnto(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator), "data", "downloads")
	attempts := []string{
		"../../../etc/passwd",
		"..",
		"../../../../../../../../../../etc/passwd",
		"%(title)s/../../../../../../root/.ssh/id_rsa",
		"....//....//etc/passwd", // not a real ".." token once split on / and \
		`C:\Windows\System32\config\SAM`,
	}
	for _, in := range attempts {
		got := filepath.Join(dir, sanitizeTemplate(in))
		prefix := dir + string(filepath.Separator)
		if got != dir && !strings.HasPrefix(got, prefix) {
			t.Errorf("sanitizeTemplate(%q) joined onto %q = %q, which escapes it", in, dir, got)
		}
	}
}

func TestDefaultsIsTheZeroValue(t *testing.T) {
	// Spelled out as its own test because it is a promise, not an accident:
	// see Options's own doc comment for why every field must default to
	// "change nothing this backend did before Options existed".
	if got := Defaults(); got != (Options{}) {
		t.Errorf("Defaults() = %+v, want the zero value", got)
	}
}
