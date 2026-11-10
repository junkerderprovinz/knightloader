package ytdlp

import (
	"path/filepath"
	"slices"
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

// Only the ".." segments are dropped, which keeps the rest of the template;
// the result stays contained once joined (see the next test).
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
	if got := Defaults(); got != (Options{}) {
		t.Errorf("Defaults() = %+v, want the zero value", got)
	}
}

func TestSanitizeFoldsAnUnknownVideoFormatOntoBest(t *testing.T) {
	for _, in := range []string{"", "avi", "mp4 avc1"} {
		if got := (Options{VideoFormat: in}).Sanitize().VideoFormat; got != "best" {
			t.Errorf("VideoFormat %q = %q, want best", in, got)
		}
	}
	if got := (Options{VideoFormat: "webm"}).Sanitize().VideoFormat; got != "webm" {
		t.Errorf("VideoFormat webm = %q, want it kept", got)
	}
}

func TestALinksOwnFormatKeepsOneRowAndTheHostsContainer(t *testing.T) {
	host := HosterPreset{Variants: Variants(), VideoFormat: "mp4 avc1", Quality: Quality720p, AudioFormat: "opus"}.Sanitize()
	cases := []struct {
		format  string
		variant Variant
		pick    string
	}{
		{LinkFormatVideo, VariantVideo, "mp4 avc1"},
		{"1080p", VariantVideo, "mp4 avc1 1080p"},
		{LinkFormatAudio, VariantAudio, ""},
	}
	for _, c := range cases {
		choice, ok := FormatChoice(c.format)
		if !ok {
			t.Errorf("FormatChoice(%q) refused a format the button offers", c.format)
			continue
		}
		got := host.ForChoice(choice)
		if len(got.Variants) != 1 || got.Variants[0] != c.variant {
			t.Errorf("format %q stages %v, want the %s row alone", c.format, got.Variants, c.variant)
		}
		if c.variant == VariantVideo && got.VideoPick() != c.pick {
			t.Errorf("format %q picks %q, want %q", c.format, got.VideoPick(), c.pick)
		}
		if got.AudioFormat != "opus" {
			t.Errorf("format %q changed the audio format to %q", c.format, got.AudioFormat)
		}
	}
}

func TestALinksOwnFormatRefusesWhatNoRowCanMean(t *testing.T) {
	for _, format := range []string{"", "best", "custom", "audioOnly", "999p", "thumbnail"} {
		if _, ok := FormatChoice(format); ok {
			t.Errorf("FormatChoice(%q) was taken", format)
		}
	}
}

func TestALinksOwnChoiceStagesItsRowsAtItsQualities(t *testing.T) {
	host := HosterPreset{Variants: Variants(), VideoFormat: "mp4 avc1", Quality: Quality720p, AudioFormat: "opus", AudioBitrate: "160"}.Sanitize()
	got := host.ForChoice(LinkChoice{Variants: []Variant{VariantVideo, VariantAudio, VariantSubtitle}, Quality: Quality1080p, AudioFormat: "mp3"})
	if !slices.Equal(got.Variants, []Variant{VariantVideo, VariantAudio, VariantSubtitle}) {
		t.Errorf("the choice stages %v", got.Variants)
	}
	if got.VideoPick() != "mp4 avc1 1080p" {
		t.Errorf("the video row picks %q, want the host's container at the chosen 1080p", got.VideoPick())
	}
	if got.AudioFormat != "mp3" || got.AudioBitrate != "160" {
		t.Errorf("the audio row is %s at %q kbit/s, want mp3 at the host's 160", got.AudioFormat, got.AudioBitrate)
	}
	kept := host.ForChoice(LinkChoice{Variants: []Variant{VariantAudio}})
	if kept.Quality != Quality720p || kept.AudioFormat != "opus" {
		t.Errorf("a choice naming no quality changed the host's to %s and %s", kept.Quality, kept.AudioFormat)
	}
}

func TestALinksOwnChoiceRefusesWhatNoRowCanMean(t *testing.T) {
	for _, c := range []LinkChoice{
		{},
		{Variants: []Variant{"chapters"}},
		{Variants: []Variant{VariantVideo}, Quality: QualityCustom},
		{Variants: []Variant{VariantVideo}, Quality: "999p"},
		{Variants: []Variant{VariantAudio}, AudioFormat: "m4b"},
	} {
		if c.Valid() {
			t.Errorf("%+v was taken", c)
		}
	}
	for _, c := range []LinkChoice{
		{Variants: []Variant{VariantDescription}},
		{Variants: []Variant{VariantVideo}, Quality: QualityBest},
		{Variants: []Variant{VariantVideo, VariantAudio}, Quality: Quality480p, AudioFormat: "flac"},
	} {
		if !c.Valid() {
			t.Errorf("%+v was refused", c)
		}
	}
}
