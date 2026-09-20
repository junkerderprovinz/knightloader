package ytdlp

import (
	"path/filepath"
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
