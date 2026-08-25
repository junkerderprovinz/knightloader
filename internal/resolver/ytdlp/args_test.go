package ytdlp

import (
	"path/filepath"
	"testing"
)

// hasArg reports whether flag appears verbatim in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// valueAfter returns the argument immediately following flag's last
// occurrence, and whether flag was found at all - good enough for the
// single-valued flags buildArgs ever emits.
func valueAfter(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// TestBuildArgsZeroValueMatchesTheOldHardcodedBehaviour is the regression
// this whole file exists to pin: before Options, run() always built exactly
// this slice (see options.go's own doc comment on Options).
func TestBuildArgsZeroValueMatchesTheOldHardcodedBehaviour(t *testing.T) {
	dir := filepath.Join("some", "dir")
	args := buildArgs(dir, Options{})

	for _, want := range []string{"--newline", "--no-warnings", "--no-color", "--no-playlist"} {
		if !hasArg(args, want) {
			t.Errorf("buildArgs(Options{}) is missing %q: %v", want, args)
		}
	}
	if hasArg(args, "-f") {
		t.Errorf("buildArgs(Options{}) passed -f, want none: %v", args)
	}
	if hasArg(args, "--write-subs") {
		t.Errorf("buildArgs(Options{}) passed --write-subs, want none: %v", args)
	}
	got, ok := valueAfter(args, "-o")
	want := filepath.Join(dir, "%(title)s.%(ext)s")
	if !ok || got != want {
		t.Errorf("-o = %q (found=%v), want %q", got, ok, want)
	}
}

func TestBuildArgsPlaylistTrueDropsNoPlaylist(t *testing.T) {
	args := buildArgs("d", Options{Playlist: true})
	if hasArg(args, "--no-playlist") {
		t.Errorf("--no-playlist present with Playlist=true: %v", args)
	}
}

func TestBuildArgsHeightCappedQuality(t *testing.T) {
	args := buildArgs("d", Options{Quality: Quality1080p})
	got, ok := valueAfter(args, "-f")
	want := "bestvideo[height<=?1080]+bestaudio/best[height<=?1080]"
	if !ok || got != want {
		t.Errorf("-f = %q (found=%v), want %q", got, ok, want)
	}
}

func TestBuildArgsAudioOnlyPairsDashXWithBestaudio(t *testing.T) {
	args := buildArgs("d", Options{Quality: QualityAudioOnly})
	got, ok := valueAfter(args, "-f")
	if !ok || got != "bestaudio/best" {
		t.Errorf("-f = %q (found=%v), want %q", got, ok, "bestaudio/best")
	}
	if !hasArg(args, "-x") {
		t.Errorf("audio-only did not pass -x: %v", args)
	}
}

func TestBuildArgsCustomQualityUsesCustomFormatVerbatim(t *testing.T) {
	args := buildArgs("d", Options{Quality: QualityCustom, CustomFormat: "worst[height>720]"})
	got, ok := valueAfter(args, "-f")
	if !ok || got != "worst[height>720]" {
		t.Errorf("-f = %q (found=%v), want the custom format verbatim", got, ok)
	}
}

func TestBuildArgsCustomQualityWithNoFormatOmitsDashF(t *testing.T) {
	// An empty CustomFormat must not become `-f ""`, which yt-dlp refuses -
	// falling through to "no opinion" is strictly safer than handing it a
	// blank selector.
	args := buildArgs("d", Options{Quality: QualityCustom})
	if hasArg(args, "-f") {
		t.Errorf("buildArgs passed -f with an empty custom format: %v", args)
	}
}

func TestBuildArgsSubtitlesOffAddsNoSubtitleFlags(t *testing.T) {
	args := buildArgs("d", Options{Subtitles: SubtitlesOff, SubtitleAuto: true})
	for _, flag := range []string{"--write-subs", "--write-auto-subs", "--embed-subs", "--sub-langs"} {
		if hasArg(args, flag) {
			t.Errorf("SubtitlesOff still passed %s: %v", flag, args)
		}
	}
}

func TestBuildArgsSubtitleFileDefaultsLangsToEnglish(t *testing.T) {
	args := buildArgs("d", Options{Subtitles: SubtitlesFile})
	if !hasArg(args, "--write-subs") {
		t.Errorf("SubtitlesFile did not pass --write-subs: %v", args)
	}
	if hasArg(args, "--embed-subs") {
		t.Errorf("SubtitlesFile passed --embed-subs, want file-only: %v", args)
	}
	got, ok := valueAfter(args, "--sub-langs")
	if !ok || got != DefaultSubtitleLangs {
		t.Errorf("--sub-langs = %q (found=%v), want default %q", got, ok, DefaultSubtitleLangs)
	}
}

func TestBuildArgsSubtitleLangsIsHonoured(t *testing.T) {
	args := buildArgs("d", Options{Subtitles: SubtitlesFile, SubtitleLangs: "de,fr"})
	got, ok := valueAfter(args, "--sub-langs")
	if !ok || got != "de,fr" {
		t.Errorf("--sub-langs = %q (found=%v), want %q", got, ok, "de,fr")
	}
}

func TestBuildArgsSubtitleEmbedAddsEmbedFlag(t *testing.T) {
	args := buildArgs("d", Options{Subtitles: SubtitlesEmbed})
	if !hasArg(args, "--write-subs") || !hasArg(args, "--embed-subs") {
		t.Errorf("SubtitlesEmbed missing --write-subs/--embed-subs: %v", args)
	}
}

func TestBuildArgsSubtitleAutoAddsWriteAutoSubs(t *testing.T) {
	args := buildArgs("d", Options{Subtitles: SubtitlesFile, SubtitleAuto: true})
	if !hasArg(args, "--write-auto-subs") {
		t.Errorf("SubtitleAuto=true did not pass --write-auto-subs: %v", args)
	}
}

func TestBuildArgsThumbnailAddsWriteThumbnail(t *testing.T) {
	args := buildArgs("d", Options{Thumbnail: true})
	if !hasArg(args, "--write-thumbnail") {
		t.Errorf("Thumbnail=true did not pass --write-thumbnail: %v", args)
	}
}

func TestBuildArgsThumbnailOffAddsNothing(t *testing.T) {
	args := buildArgs("d", Options{})
	if hasArg(args, "--write-thumbnail") {
		t.Errorf("buildArgs(Options{}) passed --write-thumbnail, want none: %v", args)
	}
}

func TestBuildArgsDescriptionAddsWriteDescription(t *testing.T) {
	args := buildArgs("d", Options{Description: true})
	if !hasArg(args, "--write-description") {
		t.Errorf("Description=true did not pass --write-description: %v", args)
	}
}

func TestBuildArgsKeepAudioPairsDashXWithKeepVideo(t *testing.T) {
	args := buildArgs("d", Options{KeepAudio: true})
	if !hasArg(args, "-x") || !hasArg(args, "--keep-video") {
		t.Errorf("KeepAudio=true missing -x/--keep-video: %v", args)
	}
}

// TestBuildArgsKeepAudioIsANoOpUnderAudioOnly is the guard on the flag
// above: AudioOnly already IS an audio extraction with no video format
// requested at all, so KeepAudio must not add a second, redundant -x or a
// --keep-video that has no video to keep.
func TestBuildArgsKeepAudioIsANoOpUnderAudioOnly(t *testing.T) {
	args := buildArgs("d", Options{Quality: QualityAudioOnly, KeepAudio: true})
	if hasArg(args, "--keep-video") {
		t.Errorf("KeepAudio under QualityAudioOnly passed --keep-video, want none: %v", args)
	}
	n := 0
	for _, a := range args {
		if a == "-x" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("-x appears %d times under QualityAudioOnly+KeepAudio, want exactly 1: %v", n, args)
	}
}

func TestBuildArgsCustomOutputTemplate(t *testing.T) {
	dir := filepath.Join("d", "l")
	args := buildArgs(dir, Options{OutputTemplate: "%(uploader)s/%(title)s.%(ext)s"})
	got, ok := valueAfter(args, "-o")
	want := filepath.Join(dir, "%(uploader)s/%(title)s.%(ext)s")
	if !ok || got != want {
		t.Errorf("-o = %q (found=%v), want %q", got, ok, want)
	}
}
