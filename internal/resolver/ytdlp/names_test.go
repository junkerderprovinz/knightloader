package ytdlp

import (
	"path/filepath"
	"strings"
	"testing"
)

// fill stands in for yt-dlp's own expansion of the fields these templates use:
// a plain field, and one with a replacement and a default after & and |.
func fill(tmpl string, fields map[string]string) string {
	out := tmpl
	for {
		start := strings.Index(out, "%(")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start:], ")s")
		spec := out[start+2 : start+end]
		name, rest, _ := strings.Cut(spec, "&")
		replace, fallback, _ := strings.Cut(rest, "|")
		value, ok := fields[name]
		got := value
		switch {
		case !ok:
			got = fallback
		case replace != "":
			got = strings.Replace(strings.Replace(replace, "{:.0f}", value, 1), "{}", value, 1)
		}
		out = out[:start] + got + out[start+end+2:]
	}
}

func TestTwoPicksOfOneVideoLandOnTwoNames(t *testing.T) {
	tmpl := outputTemplate(Options{Variant: VariantVideo})
	high := fill(tmpl, map[string]string{"title": "Clip", "height": "1080", "ext": "mkv"})
	low := fill(tmpl, map[string]string{"title": "Clip", "height": "720", "ext": "mkv"})
	if high == low {
		t.Fatalf("1080p and 720p of one video both named %q", high)
	}
	if high != "Clip (1080p).mkv" {
		t.Errorf("video name = %q, want the height in brackets before the extension", high)
	}
}

func TestTheAudioRowCarriesItsBitrate(t *testing.T) {
	tmpl := outputTemplate(Options{Variant: VariantAudio})
	if got := fill(tmpl, map[string]string{"title": "Clip", "abr": "128", "ext": "m4a"}); got != "Clip (128kbit).m4a" {
		t.Errorf("audio name = %q, want the bitrate in brackets", got)
	}
}

func TestANameSaysBestWhereTheSiteGivesNoQuality(t *testing.T) {
	for _, v := range []Variant{VariantVideo, VariantAudio} {
		if got := fill(outputTemplate(Options{Variant: v}), map[string]string{"title": "Clip", "ext": "mp4"}); got != "Clip (best).mp4" {
			t.Errorf("%s name without a quality = %q, want (best)", v, got)
		}
	}
}

func TestEachSideRowWritesUnderItsOwnMark(t *testing.T) {
	for _, v := range []Variant{VariantThumbnail, VariantSubtitle, VariantDescription} {
		o := Options{Variant: v, SideTag: "high"}
		got := fill(outputTemplate(o), map[string]string{"title": "Clip", "ext": "jpg"})
		if got != "Clip.kl-high.jpg" {
			t.Errorf("%s name = %q, want the row's mark after the title", v, got)
		}
		if other := fill(outputTemplate(Options{Variant: v, SideTag: "low"}), map[string]string{"title": "Clip", "ext": "jpg"}); other == got {
			t.Errorf("two %s rows of one video both named %q", v, got)
		}
		if shown := o.shown("Clip.kl-high.de"); shown != "Clip.de" {
			t.Errorf("%s row shows %q, want the name without its mark", v, shown)
		}
	}
	if got := outputTemplate(Options{Variant: VariantVideo, Embed: Embed{Separate: true}}); got != defaultOutputTemplate {
		t.Errorf("separate streams template = %q, want the plain title the format codes tell apart", got)
	}
}

func TestATypedTemplateStaysAsTyped(t *testing.T) {
	const typed = "%(uploader)s/%(title)s.%(ext)s"
	args := buildArgs("d", Options{Variant: VariantVideo, OutputTemplate: typed})
	if got, _ := valueAfter(args, "-o"); got != filepath.Join("d", typed) {
		t.Errorf("-o = %q, want the typed template", got)
	}
}
