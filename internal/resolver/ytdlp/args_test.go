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

// TestBuildArgsQualityAudioOnlyFoldsToNoOpinionUnderVariantVideo pins
// QualityAudioOnly's retirement (options.go's own doc comment on the
// constant): Qualities() no longer offers it, so Sanitize folds an old
// saved value onto QualityBest the same as any other value it no longer
// recognises - a video-variant row with it set passes no -f at all rather
// than the audio-extraction behaviour the preset used to trigger. Getting
// audio-only now means enabling the Audio row (VariantAudio, tested below)
// and leaving Video off, not a video-quality preset in disguise.
func TestBuildArgsQualityAudioOnlyFoldsToNoOpinionUnderVariantVideo(t *testing.T) {
	args := buildArgs("d", Options{Quality: QualityAudioOnly})
	if hasArg(args, "-f") {
		t.Errorf("buildArgs(Quality: QualityAudioOnly) passed -f, want none (folded to QualityBest): %v", args)
	}
	if hasArg(args, "-x") {
		t.Errorf("buildArgs(Quality: QualityAudioOnly) passed -x under VariantVideo, want none: %v", args)
	}
}

func TestBuildArgsVariantAudioPairsDashXWithBestaudio(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio})
	got, ok := valueAfter(args, "-f")
	if !ok || got != "bestaudio/best" {
		t.Errorf("-f = %q (found=%v), want %q", got, ok, "bestaudio/best")
	}
	if !hasArg(args, "-x") {
		t.Errorf("VariantAudio did not pass -x: %v", args)
	}
	if hasArg(args, "--audio-format") {
		t.Errorf("VariantAudio with no AudioFormat passed --audio-format, want none: %v", args)
	}
}

func TestBuildArgsVariantAudioHonoursAudioFormat(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioFormat: "mp3"})
	got, ok := valueAfter(args, "--audio-format")
	if !ok || got != "mp3" {
		t.Errorf("--audio-format = %q (found=%v), want %q", got, ok, "mp3")
	}
}

// TestBuildArgsVariantAudioFormatBestOmitsDashAudioFormat is the same "no
// opinion" shape formatSelector's own QualityBest branch uses: "best" is
// AudioFormats()'s own default entry, standing for "whatever yt-dlp already
// picks", not a real -x --audio-format target worth naming on the command
// line.
func TestBuildArgsVariantAudioFormatBestOmitsDashAudioFormat(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioFormat: "best"})
	if hasArg(args, "--audio-format") {
		t.Errorf("VariantAudio with AudioFormat=best passed --audio-format, want none: %v", args)
	}
}

func TestBuildArgsVariantThumbnailAddsSkipDownloadAndWriteThumbnail(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantThumbnail})
	if !hasArg(args, "--skip-download") {
		t.Errorf("VariantThumbnail did not pass --skip-download: %v", args)
	}
	if !hasArg(args, "--write-thumbnail") {
		t.Errorf("VariantThumbnail did not pass --write-thumbnail: %v", args)
	}
}

func TestBuildArgsVariantSubtitleAddsSkipDownloadWriteSubsAndDefaultLangs(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantSubtitle})
	if !hasArg(args, "--skip-download") {
		t.Errorf("VariantSubtitle did not pass --skip-download: %v", args)
	}
	if !hasArg(args, "--write-subs") {
		t.Errorf("VariantSubtitle did not pass --write-subs: %v", args)
	}
	got, ok := valueAfter(args, "--sub-langs")
	if !ok || got != DefaultSubtitleLangs {
		t.Errorf("--sub-langs = %q (found=%v), want default %q", got, ok, DefaultSubtitleLangs)
	}
	if hasArg(args, "--write-auto-subs") {
		t.Errorf("VariantSubtitle with SubtitleAuto=false passed --write-auto-subs, want none: %v", args)
	}
}

func TestBuildArgsVariantSubtitleAutoAddsWriteAutoSubs(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantSubtitle, SubtitleAuto: true})
	if !hasArg(args, "--write-auto-subs") {
		t.Errorf("VariantSubtitle with SubtitleAuto=true did not pass --write-auto-subs: %v", args)
	}
}

func TestBuildArgsVariantDescriptionAddsSkipDownloadAndWriteDescription(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantDescription})
	if !hasArg(args, "--skip-download") {
		t.Errorf("VariantDescription did not pass --skip-download: %v", args)
	}
	if !hasArg(args, "--write-description") {
		t.Errorf("VariantDescription did not pass --write-description: %v", args)
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

// TestBuildArgsVariantVideoAddsNoSubtitleFlags is TestBuildArgsSubtitlesOff's
// replacement under the row model: a video row has no subtitle fields to
// read at all any more (SubtitleLangs/SubtitleAuto are read only under
// VariantSubtitle, see Options.SubtitleLangs's own doc comment), so passing
// them alongside VariantVideo must have no effect on this row's own args.
func TestBuildArgsVariantVideoAddsNoSubtitleFlags(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantVideo, SubtitleAuto: true, SubtitleLangs: "de"})
	for _, flag := range []string{"--write-subs", "--write-auto-subs", "--embed-subs", "--sub-langs"} {
		if hasArg(args, flag) {
			t.Errorf("VariantVideo still passed %s: %v", flag, args)
		}
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
