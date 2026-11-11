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

// The template can only write the frame yt-dlp picked, and the finished file
// takes the quality that frame is named by.
func TestAVideoIsNamedAfterTheQualityOfItsFrame(t *testing.T) {
	cases := []struct {
		name   string
		tag    string
		fields map[string]string
		want   string
	}{
		{"2:1", "", map[string]string{"width": "1920", "height": "960"}, "Clip (1080p).mkv"},
		{"portrait", "", map[string]string{"width": "1080", "height": "1920"}, "Clip (1080p).mkv"},
		{"16:9", "", map[string]string{"width": "1280", "height": "720"}, "Clip (720p).mkv"},
		{"21:9", "", map[string]string{"width": "2560", "height": "1080"}, "Clip (1440p).mkv"},
		{"beside another of its quality", "vp9", map[string]string{"width": "1920", "height": "960"}, "Clip (1080p vp9).mkv"},
		{"no width", "", map[string]string{"height": "300"}, "Clip (300p).mkv"},
		{"no size", "", map[string]string{}, "Clip (best).mkv"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fields := map[string]string{"title": "Clip", "ext": "mkv"}
			for k, v := range c.fields {
				fields[k] = v
			}
			written := fill(outputTemplate(Options{Variant: VariantVideo, NameTag: c.tag}), fields)
			if got := qualityName(written); got != c.want {
				t.Errorf("%q is named %q, want %q", written, got, c.want)
			}
		})
	}
	if got := qualityName("Song (640x480p) (1920x960p).f248.webm"); got != "Song (640x480p) (1080p).f248.webm" {
		t.Errorf("a frame in the title came out as %q, want only the last bracket named", got)
	}
}

func TestATrackBesideAnotherOfItsHeightNamesItsCodec(t *testing.T) {
	tmpl := outputTemplate(Options{Variant: VariantVideo, NameTag: "av1"})
	if got := fill(tmpl, map[string]string{"title": "Clip", "height": "720", "ext": "mp4"}); got != "Clip (720p av1).mp4" {
		t.Errorf("video name = %q, want the codec beside the height", got)
	}
	for _, c := range []struct {
		pick   string
		others []string
		want   string
	}{
		{"720p mp4 av1", []string{"720p mp4 avc1"}, "av1"},
		{"2160p60hdr webm vp9", []string{"2160p60 webm vp9"}, "60fps vp9 HDR"},
		{"2160p60 webm vp9", []string{"2160p60hdr webm vp9"}, "60fps vp9"},
		{"720p60 mp4 avc1", []string{"720p mp4 avc1"}, "60fps avc1"},
		{"720p mp4 av1", []string{"best"}, "av1"},
		{"720p mp4 av1", []string{"1080p"}, "av1"},
		{"720p mp4 av1", []string{"480p"}, ""},
		{"720p mp4 av1", []string{"1080p mp4 avc1"}, ""},
		{"720p mp4 av1", nil, ""},
		{"720p", []string{"720p mp4 avc1"}, ""},
		{"best", []string{"720p mp4 avc1"}, ""},
	} {
		if got := NameTag(c.pick, c.others); got != c.want {
			t.Errorf("NameTag(%q, %q) = %q, want %q", c.pick, c.others, got, c.want)
		}
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
