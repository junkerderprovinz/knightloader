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

func TestSanitizeFoldsUnknownSubtitleModeOntoOff(t *testing.T) {
	got := Options{Subtitles: "burn-in"}.Sanitize()
	if got.Subtitles != SubtitlesOff {
		t.Errorf("Subtitles = %q, want %q for an unrecognised value", got.Subtitles, SubtitlesOff)
	}
}

func TestSanitizeKeepsEveryKnownQualityAndSubtitleMode(t *testing.T) {
	for _, q := range Qualities() {
		if got := (Options{Quality: q}).Sanitize().Quality; got != q {
			t.Errorf("Sanitize() folded known quality %q onto %q", q, got)
		}
	}
	for _, m := range SubtitleModes() {
		if got := (Options{Subtitles: m}).Sanitize().Subtitles; got != m {
			t.Errorf("Sanitize() folded known subtitle mode %q onto %q", m, got)
		}
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
