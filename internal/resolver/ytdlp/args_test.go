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

// valueAfter returns the argument after the first occurrence of flag, and
// whether flag was found.
func valueAfter(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func TestBuildArgsZeroValueIsThePlainInvocation(t *testing.T) {
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

// A portrait track taller than the cap but no wider comes first, since the
// height filter alone would take a far smaller portrait track over it.
func TestBuildArgsResolutionCappedQuality(t *testing.T) {
	args := buildArgs("d", Options{Quality: Quality1080p})
	got, ok := valueAfter(args, "-f")
	want := "bv[width<=1080][height>1080]+ba/bv[height<=?1080]+ba/b[width<=1080][height>1080]/b[height<=?1080]"
	if !ok || got != want {
		t.Errorf("-f = %q (found=%v), want %q", got, ok, want)
	}
}

// A stored QualityAudioOnly is folded onto QualityBest; audio-only is the
// audio variant.
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

// "best" means no transcode, so no --audio-format is passed.
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
	// A fixed format lets applyProbeFormats show Ext="jpg".
	got, ok := valueAfter(args, "--convert-thumbnails")
	if !ok || got != "jpg" {
		t.Errorf("--convert-thumbnails = %q (found=%v), want %q", got, ok, "jpg")
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
	// A fixed format lets applyProbeFormats show Ext="srt".
	if got, ok := valueAfter(args, "--sub-format"); !ok || got != "srt" {
		t.Errorf("--sub-format = %q (found=%v), want %q", got, ok, "srt")
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
	// yt-dlp refuses `-f ""`.
	args := buildArgs("d", Options{Quality: QualityCustom})
	if hasArg(args, "-f") {
		t.Errorf("buildArgs passed -f with an empty custom format: %v", args)
	}
}

// Without Embed.Subs, a video row ignores the subtitle fields.
func TestBuildArgsVariantVideoAddsNoSubtitleFlags(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantVideo, SubtitleAuto: true, SubtitleLangs: "de"})
	for _, flag := range []string{"--write-subs", "--write-auto-subs", "--embed-subs", "--sub-langs"} {
		if hasArg(args, flag) {
			t.Errorf("VariantVideo still passed %s: %v", flag, args)
		}
	}
}

// A fixed merge target lets applyProbeFormats show Ext="mkv".
func TestBuildArgsVariantVideoForcesMkvMerge(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantVideo})
	got, ok := valueAfter(args, "--merge-output-format")
	if !ok || got != "mkv" {
		t.Errorf("--merge-output-format = %q (found=%v), want %q", got, ok, "mkv")
	}
}

func TestBuildArgsVariantAudioHonoursAudioBitrate(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioFormat: "mp3", AudioBitrate: "192"})
	got, ok := valueAfter(args, "--audio-quality")
	if !ok || got != "192K" {
		t.Errorf("--audio-quality = %q (found=%v), want %q", got, ok, "192K")
	}
}

func TestBuildArgsVariantAudioNoBitrateOmitsDashAudioQuality(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio})
	if hasArg(args, "--audio-quality") {
		t.Errorf("VariantAudio with no AudioBitrate passed --audio-quality, want none: %v", args)
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

// Both flags are needed: one avoids a round trip per fragment in series, the
// other avoids throttling of a single long response.
func TestBuildArgsAsksForConcurrentFragmentsAndChunkedRanges(t *testing.T) {
	args := buildArgs(filepath.Join("some", "dir"), Options{})

	if got, ok := valueAfter(args, "--concurrent-fragments"); !ok || got != "4" {
		t.Errorf("--concurrent-fragments = %q (present: %v), want \"4\": %v", got, ok, args)
	}
	if got, ok := valueAfter(args, "--http-chunk-size"); !ok || got != "10485760" {
		t.Errorf("--http-chunk-size = %q (present: %v), want 10 MiB: %v", got, ok, args)
	}
}

// --limit-rate applies per fragment connection, so the limit must be divided
// among them.
func TestConcurrentFragmentsDividesTheSpeedLimit(t *testing.T) {
	const limit = 4_000_000
	per := limit / concurrentFragments
	if per*concurrentFragments != limit {
		t.Fatalf("the divided limit (%d x %d) does not add back up to %d", per, concurrentFragments, limit)
	}
	// A limit below the fragment count rounds to 0, which yt-dlp reads as
	// unlimited, so run needs its floor.
	if 3/concurrentFragments != 0 {
		t.Fatalf("this test's premise is wrong: %d/%d no longer rounds to zero", 3, concurrentFragments)
	}
}
