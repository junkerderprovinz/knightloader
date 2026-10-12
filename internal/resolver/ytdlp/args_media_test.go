package ytdlp

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// argPairs returns every value that followed flag, in order, for flags passed
// more than once such as --parse-metadata.
func argPairs(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// outputTemplates splits the -o values into the plain one and the
// type-prefixed ones ("thumbnail:..."), which music mode adds.
func outputTemplates(args []string) (plain string, typed []string) {
	for _, v := range argPairs(args, "-o") {
		if strings.Contains(v, ":") && strings.HasPrefix(v, "thumbnail:") {
			typed = append(typed, v)
			continue
		}
		plain = v
	}
	return plain, typed
}

func TestBuildArgsEmbedIsOffAtTheZeroValue(t *testing.T) {
	args := buildArgs("d", Options{})
	for _, flag := range []string{
		"--embed-metadata", "--embed-thumbnail", "--embed-chapters", "--embed-subs",
		"--split-chapters", "--write-info-json", "--live-from-start", "--parse-metadata",
		"--cookies",
	} {
		if hasArg(args, flag) {
			t.Errorf("buildArgs(Options{}) passed %s, want none: %v", flag, args)
		}
	}
	// Only the live guard may change the progress template.
	if got, ok := valueAfter(args, "--progress-template"); !ok || got != "KLP:%(progress)j" {
		t.Errorf("--progress-template = %q (found=%v), want the unchanged plain template", got, ok)
	}
}

func TestBuildArgsEmbedFlagsOnAVideoRow(t *testing.T) {
	args := buildArgs("d", Options{Embed: Embed{Metadata: true, Thumbnail: true, Chapters: true, SplitChapters: true}})
	for _, flag := range []string{"--embed-metadata", "--embed-thumbnail", "--embed-chapters", "--split-chapters"} {
		if !hasArg(args, flag) {
			t.Errorf("buildArgs did not pass %s: %v", flag, args)
		}
	}
}

// --embed-subs alone embeds nothing, since yt-dlp only fetches subtitles with
// --write-subs.
func TestBuildArgsEmbedSubsAlsoFetchesThem(t *testing.T) {
	args := buildArgs("d", Options{Embed: Embed{Subs: true}, SubtitleLangs: "de,en"})
	for _, flag := range []string{"--write-subs", "--embed-subs"} {
		if !hasArg(args, flag) {
			t.Errorf("Embed.Subs did not pass %s: %v", flag, args)
		}
	}
	if got, ok := valueAfter(args, "--sub-langs"); !ok || got != "de,en" {
		t.Errorf("--sub-langs = %q (found=%v), want the configured list", got, ok)
	}
	if hasArg(args, "--write-auto-subs") {
		t.Errorf("Embed.Subs with SubtitleAuto=false passed --write-auto-subs: %v", args)
	}
}

// yt-dlp refuses an empty --sub-langs.
func TestBuildArgsEmbedSubsFallsBackToTheDefaultLanguage(t *testing.T) {
	args := buildArgs("d", Options{Embed: Embed{Subs: true}})
	if got, ok := valueAfter(args, "--sub-langs"); !ok || got != DefaultSubtitleLangs {
		t.Errorf("--sub-langs = %q (found=%v), want the default %q", got, ok, DefaultSubtitleLangs)
	}
}

// An extracted audio file has no subtitle stream.
func TestBuildArgsEmbedSubsIsSkippedOnAnAudioRow(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, Embed: Embed{Subs: true}})
	for _, flag := range []string{"--write-subs", "--embed-subs", "--sub-langs"} {
		if hasArg(args, flag) {
			t.Errorf("VariantAudio passed %s for Embed.Subs: %v", flag, args)
		}
	}
}

// yt-dlp has no --write-nfo; the NFO is built from the info json.
func TestBuildArgsNFOAsksForTheInfoJSON(t *testing.T) {
	if args := buildArgs("d", Options{Embed: Embed{NFO: true}}); !hasArg(args, "--write-info-json") {
		t.Errorf("Embed.NFO did not ask for --write-info-json: %v", args)
	}
	// The measurement needs the announced duration from it.
	if args := buildArgs("d", Options{Measure: Measure{Enabled: true}}); !hasArg(args, "--write-info-json") {
		t.Errorf("Measure.Enabled did not ask for --write-info-json: %v", args)
	}
}

// The sidecar rows download no media file for either feature to act on.
func TestBuildArgsNoInfoJSONOnASidecarRow(t *testing.T) {
	for _, v := range []Variant{VariantThumbnail, VariantSubtitle, VariantDescription} {
		args := buildArgs("d", Options{Variant: v, Embed: Embed{NFO: true}, Measure: Measure{Enabled: true}})
		if hasArg(args, "--write-info-json") {
			t.Errorf("%s passed --write-info-json: %v", v, args)
		}
	}
}

func TestBuildArgsMusicModeTagsAndNames(t *testing.T) {
	dir := filepath.Join("d", "l")
	args := buildArgs(dir, Options{Variant: VariantAudio, Music: true})

	// One mapping per field a music library indexes by.
	maps := argPairs(args, "--parse-metadata")
	if len(maps) != 4 {
		t.Fatalf("--parse-metadata passed %d times, want 4: %v", len(maps), maps)
	}
	for _, want := range []string{"meta_artist", "meta_album", "meta_title", "meta_track"} {
		found := false
		for _, m := range maps {
			if strings.Contains(m, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no --parse-metadata mapping fills %s: %v", want, maps)
		}
	}
	// Only --embed-metadata writes the mapped fields into the file.
	if !hasArg(args, "--embed-metadata") {
		t.Errorf("music mode did not pass --embed-metadata: %v", args)
	}

	plain, typed := outputTemplates(args)
	want := filepath.Join(dir, musicOutputTemplate)
	if plain != want {
		t.Errorf("-o = %q, want the music naming scheme %q", plain, want)
	}

	// One cover per album folder.
	if len(typed) != 1 {
		t.Fatalf("music mode set %d thumbnail output templates, want 1: %v", len(typed), typed)
	}
	if !strings.HasSuffix(typed[0], filepath.Join("%(album,playlist_title,title)s", "cover.%(ext)s")) {
		t.Errorf("cover template = %q, want it inside the album folder", typed[0])
	}
	if got, ok := valueAfter(args, "--convert-thumbnails"); !ok || got != "jpg" {
		t.Errorf("--convert-thumbnails = %q (found=%v), want jpg so cover.jpg is a fact", got, ok)
	}
}

func TestBuildArgsMusicModeDoesNotOverruleATypedTemplate(t *testing.T) {
	dir := "d"
	const typed = "%(uploader)s/%(title)s.%(ext)s"
	args := buildArgs(dir, Options{Variant: VariantAudio, Music: true, OutputTemplate: typed})
	plain, covers := outputTemplates(args)
	if plain != filepath.Join(dir, typed) {
		t.Errorf("-o = %q, want the typed template %q", plain, filepath.Join(dir, typed))
	}
	// The cover follows the typed template.
	for _, c := range covers {
		if !strings.Contains(c, "%(uploader)s") {
			t.Errorf("cover template = %q, want it beside the typed template's own files", c)
		}
	}
}

func TestBuildArgsMusicModeIsAudioOnly(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantVideo, Music: true})
	if hasArg(args, "--parse-metadata") {
		t.Errorf("music mode leaked onto a video row: %v", args)
	}
	plain, _ := outputTemplates(args)
	if strings.Contains(plain, "%(artist") {
		t.Errorf("-o = %q, want the ordinary template on a video row", plain)
	}
}

// Most sites report no per-track language, and the filter alone would match
// nothing there.
func TestAudioSelectorFallsBackWhenTheSourceNamesNoLanguage(t *testing.T) {
	if got := audioSelector(""); got != "bestaudio/best" {
		t.Errorf("audioSelector(\"\") = %q, want the untouched selector", got)
	}
	got := audioSelector("de")
	if !strings.HasPrefix(got, "bestaudio[language^=de]") {
		t.Errorf("audioSelector(\"de\") = %q, want the language filter first", got)
	}
	if !strings.HasSuffix(got, "/bestaudio/best") {
		t.Errorf("audioSelector(\"de\") = %q, want a fallback for a source with no language field", got)
	}
	// A prefix match also takes "de-DE".
	if strings.Contains(got, "language=de") {
		t.Errorf("audioSelector(\"de\") = %q, want a prefix match so de-DE is included", got)
	}
}

func TestBuildArgsAudioLangUsesTheSelector(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, AudioLang: "de"})
	got, ok := valueAfter(args, "-f")
	if !ok || got != audioSelector("de") {
		t.Errorf("-f = %q (found=%v), want %q", got, ok, audioSelector("de"))
	}
}

// A bracket or comma in the value would break the whole format selector.
func TestSanitizeKeepsAudioLangInsideTheFilterGrammar(t *testing.T) {
	cases := map[string]string{
		"de":            "de",
		"pt-BR":         "pt-BR",
		" de ":          "de",
		"de]+bestvideo": "debestvideo",
		"de,en":         "deen",
		`de"`:           "de",
	}
	for in, want := range cases {
		o := Options{AudioLang: in}
		if got := o.Sanitize().AudioLang; got != want {
			t.Errorf("Sanitize(AudioLang %q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildArgsLiveFromStart(t *testing.T) {
	if args := buildArgs("d", Options{Live: Live{Enabled: true, FromStart: true}}); !hasArg(args, "--live-from-start") {
		t.Errorf("Live.FromStart did not pass --live-from-start: %v", args)
	}
	// The caps alone must not pull in a stream's whole backlog.
	if args := buildArgs("d", Options{Live: Live{Enabled: true}}); hasArg(args, "--live-from-start") {
		t.Errorf("Live.Enabled alone passed --live-from-start: %v", args)
	}
}

func TestBuildArgsLiveSwitchesTheProgressTemplate(t *testing.T) {
	args := buildArgs("d", Options{Live: Live{Enabled: true}})
	got, ok := valueAfter(args, "--progress-template")
	if !ok || got != liveProgressTemplate {
		t.Errorf("--progress-template = %q (found=%v), want the live template", got, ok)
	}
	if !strings.Contains(got, "%(info.is_live)s") {
		t.Errorf("the live progress template does not ask for is_live: %q", got)
	}
}

// Merged, the default: one -f with + between video and audio, one container,
// and the subtitles and cover inside it.
func TestBuildArgsMergesAVideoRowIntoOneFile(t *testing.T) {
	args := buildArgs("d", Options{Quality: Quality1080p, Embed: Embed{Subs: true, Thumbnail: true}})
	f, _ := valueAfter(args, "-f")
	if !strings.Contains(f, "+ba") || strings.Contains(f, ",") {
		t.Errorf("-f = %q, want a merge of video and audio", f)
	}
	for _, want := range []string{"--merge-output-format", "--embed-subs", "--embed-thumbnail"} {
		if !hasArg(args, want) {
			t.Errorf("merged video is missing %s: %v", want, args)
		}
	}
	if got, _ := valueAfter(args, "-o"); got != filepath.Join("d", "%(title)s.%(ext)s") {
		t.Errorf("-o = %q, want the plain template", got)
	}
}

// Kept apart: the same choice with a comma where the + was, nothing to merge
// into, the subtitles and cover written beside the streams, and each stream
// named by its format so a webm video and webm audio do not collide.
func TestBuildArgsKeepsAVideoRowApart(t *testing.T) {
	o := Options{Quality: Quality1080p, Embed: Embed{Separate: true, Subs: true, Thumbnail: true, Metadata: true, Chapters: true}}
	args := buildArgs("d", o)

	f, _ := valueAfter(args, "-f")
	tall, short := "[width<=1080][height>1080]", "[height<=?1080]"
	want := "(bv" + tall + "/bv" + short + "/b" + tall + "/b" + short + "),(ba/b" + tall + "/b" + short + ")"
	if f != want {
		t.Errorf("-f = %q, want %q", f, want)
	}
	for _, gone := range []string{"--merge-output-format", "--embed-subs", "--embed-thumbnail"} {
		if hasArg(args, gone) {
			t.Errorf("a video kept apart still passes %s: %v", gone, args)
		}
	}
	for _, want := range []string{"--write-subs", "--convert-subs", "--write-thumbnail", "--embed-metadata", "--embed-chapters"} {
		if !hasArg(args, want) {
			t.Errorf("a video kept apart is missing %s: %v", want, args)
		}
	}
	var outs []string
	for i, a := range args {
		if a == "-o" {
			outs = append(outs, args[i+1])
		}
	}
	plain := filepath.Join("d", "%(title)s.%(ext)s")
	wantOuts := []string{"subtitle:" + plain, "thumbnail:" + plain, filepath.Join("d", "%(title)s.f%(format_id)s.%(ext)s")}
	if !slices.Equal(outs, wantOuts) {
		t.Errorf("-o values = %q, want %q", outs, wantOuts)
	}
}

// With no choice of its own a video kept apart asks for what yt-dlp would
// pick, and a preset's container wish keeps its order of preference.
func TestBuildArgsKeepsTheDefaultAndAWishApart(t *testing.T) {
	f, _ := valueAfter(buildArgs("d", Options{Embed: Embed{Separate: true}}), "-f")
	if f != "(bv*/b),(ba/b)" {
		t.Errorf("-f = %q, want (bv*/b),(ba/b)", f)
	}
	wish := Options{VideoPick: "mp4 h264", Embed: Embed{Separate: true}}
	f, _ = valueAfter(buildArgs("d", wish), "-f")
	merged, _ := valueAfter(buildArgs("d", Options{VideoPick: "mp4 h264"}), "-f")
	if !strings.HasPrefix(f, "(bv[ext=mp4]") || !strings.Contains(f, "),(ba[ext=m4a]/ba/") {
		t.Errorf("-f = %q for a wish kept apart (merged it is %q)", f, merged)
	}
}

// A custom selector is the user's own and goes to yt-dlp unchanged.
func TestBuildArgsLeavesACustomSelectorAloneWhenKeptApart(t *testing.T) {
	o := Options{Quality: QualityCustom, CustomFormat: "bv*+ba", Embed: Embed{Separate: true}}
	if f, _ := valueAfter(buildArgs("d", o), "-f"); f != "bv*+ba" {
		t.Errorf("-f = %q, want the custom selector as typed", f)
	}
}

// Only the video row merges, so the switch leaves the audio row as it was.
func TestBuildArgsKeptApartDoesNotTouchTheAudioRow(t *testing.T) {
	merged := buildArgs("d", Options{Variant: VariantAudio})
	apart := buildArgs("d", Options{Variant: VariantAudio, Embed: Embed{Separate: true}})
	if !slices.Equal(merged, apart) {
		t.Errorf("the audio row changed with the switch:\n%v\n%v", merged, apart)
	}
}
