package ytdlp

// args_media_test.go: buildArgs for the library-facing wave - embedding, the
// music tagger, the audio-language filter and the livestream flags. The
// original args_test.go covers the format/quality/variant half; this file is
// beside it rather than inside it so the two waves' regressions stay legible
// as two lists.

import (
	"path/filepath"
	"strings"
	"testing"
)

// argPairs returns every value that followed flag, in order - the shape
// --parse-metadata needs, which valueAfter (args_test.go) cannot answer
// because it is passed four times in one invocation.
func argPairs(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// outputTemplates splits the -o values into the plain one (the file being
// downloaded) and the type-prefixed ones ("thumbnail:…"). Music mode passes
// both, and valueAfter would answer with whichever comes first, which is not
// the same question.
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

// TestBuildArgsEmbedIsOffAtTheZeroValue is the promise the whole wave rests
// on (jdp: "bestehende Defaults NICHT drehen, jeder neue Schalter kommt
// aus"): an install that never opens the new settings must spawn yt-dlp with
// exactly the flags it always did.
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
	// The progress template is the one line every download's display is
	// parsed out of, and the live guard is the only thing allowed to change
	// its shape - see live.go.
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

// TestBuildArgsEmbedSubsAlsoFetchesThem is the trap --embed-subs sets on its
// own: yt-dlp embeds subtitles it has, and without --write-subs it has none,
// so the flag alone produces a video with no subtitle track and no complaint.
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

// TestBuildArgsEmbedSubsFallsBackToTheDefaultLanguage covers the same field
// left blank: --sub-langs with no value is an invocation yt-dlp refuses
// outright, so a video row asking to embed subtitles must name a language
// even when nobody typed one.
func TestBuildArgsEmbedSubsFallsBackToTheDefaultLanguage(t *testing.T) {
	args := buildArgs("d", Options{Embed: Embed{Subs: true}})
	if got, ok := valueAfter(args, "--sub-langs"); !ok || got != DefaultSubtitleLangs {
		t.Errorf("--sub-langs = %q (found=%v), want the default %q", got, ok, DefaultSubtitleLangs)
	}
}

// TestBuildArgsEmbedSubsIsSkippedOnAnAudioRow: an extracted mp3 has no
// subtitle stream, and yt-dlp answers the attempt with a warning that
// --no-warnings then swallows - so the flag would do nothing at all except
// make an audio download fetch a .vtt it then cannot use.
func TestBuildArgsEmbedSubsIsSkippedOnAnAudioRow(t *testing.T) {
	args := buildArgs("d", Options{Variant: VariantAudio, Embed: Embed{Subs: true}})
	for _, flag := range []string{"--write-subs", "--embed-subs", "--sub-langs"} {
		if hasArg(args, flag) {
			t.Errorf("VariantAudio passed %s for Embed.Subs: %v", flag, args)
		}
	}
}

// TestBuildArgsNFOAsksForTheInfoJSON pins the dependency the NFO has on a
// yt-dlp flag rather than on a second network call: there is no --write-nfo,
// so the info dict has to be asked for and converted afterwards (nfo.go).
func TestBuildArgsNFOAsksForTheInfoJSON(t *testing.T) {
	if args := buildArgs("d", Options{Embed: Embed{NFO: true}}); !hasArg(args, "--write-info-json") {
		t.Errorf("Embed.NFO did not ask for --write-info-json: %v", args)
	}
	// The measurement needs it too, for the duration the SOURCE announced -
	// the only place a finished download can still learn what it was
	// promised.
	if args := buildArgs("d", Options{Measure: Measure{Enabled: true}}); !hasArg(args, "--write-info-json") {
		t.Errorf("Measure.Enabled did not ask for --write-info-json: %v", args)
	}
}

// TestBuildArgsNoInfoJSONOnASidecarRow: an NFO beside a .srt describes
// nothing, and the thumbnail/subtitle/description rows are --skip-download
// jobs with no media file for either feature to act on.
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

	// Four mappings, one per field a music library indexes by. Without them
	// an --embed-metadata mp3 carries the VIDEO's title and uploader, which
	// is the twelve-singles-by-a-channel-name failure this switch exists for.
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
	// The mappings fill fields; only --embed-metadata writes them into the
	// file. Music mode without it renames files and tags nothing.
	if !hasArg(args, "--embed-metadata") {
		t.Errorf("music mode did not pass --embed-metadata: %v", args)
	}

	plain, typed := outputTemplates(args)
	want := filepath.Join(dir, musicOutputTemplate)
	if plain != want {
		t.Errorf("-o = %q, want the music naming scheme %q", plain, want)
	}

	// One cover per album folder, which is what a library reads - as opposed
	// to one embedded copy per track, which is what a player reads.
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

// TestBuildArgsMusicModeDoesNotOverruleATypedTemplate: a person who filled in
// the output-template field can see that field, and a switch elsewhere on the
// page silently overriding it gets reported as "the template setting does
// nothing".
func TestBuildArgsMusicModeDoesNotOverruleATypedTemplate(t *testing.T) {
	dir := "d"
	const typed = "%(uploader)s/%(title)s.%(ext)s"
	args := buildArgs(dir, Options{Variant: VariantAudio, Music: true, OutputTemplate: typed})
	plain, covers := outputTemplates(args)
	if plain != filepath.Join(dir, typed) {
		t.Errorf("-o = %q, want the typed template %q", plain, filepath.Join(dir, typed))
	}
	// And the cover follows it rather than staying in the music folders that
	// are no longer being used.
	for _, c := range covers {
		if !strings.Contains(c, "%(uploader)s") {
			t.Errorf("cover template = %q, want it beside the typed template's own files", c)
		}
	}
}

// TestBuildArgsMusicModeIsAudioOnly: the naming scheme is Artist/Album/NN, and
// applying it to a video row would file films under an artist folder.
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

// TestAudioSelectorFallsBackWhenTheSourceNamesNoLanguage is the trap the
// language filter sets: most sites report no per-track language at all, and
// "bestaudio[language^=de]" alone matches nothing on those, which yt-dlp
// answers with a failed task rather than an untagged download.
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
	// ^= and not =: the German track on a YouTube video is "de-DE" as often
	// as "de", and an exact match misses half of them.
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

// TestSanitizeKeepsAudioLangInsideTheFilterGrammar: `[language^=de]` is a
// bracket expression, and a value carrying `]` does not select a different
// language, it makes a malformed selector yt-dlp refuses the whole invocation
// over.
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
	// The switch itself is not the flag: somebody who wants the caps but
	// wants to join at the live edge must not silently get the whole backlog
	// of a stream that has been running since Friday.
	if args := buildArgs("d", Options{Live: Live{Enabled: true}}); hasArg(args, "--live-from-start") {
		t.Errorf("Live.Enabled alone passed --live-from-start: %v", args)
	}
}

// TestBuildArgsLiveSwitchesTheProgressTemplate is the one place the live
// guard is allowed to change a shared code path, and the reason parseProgress
// has to read two shapes.
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
