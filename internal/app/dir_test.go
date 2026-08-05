package app

import (
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestDirFor pins where a file lands: the task's own folder wins, then the
// configured folder, optionally with a per-package subfolder — and the two
// never combine into a nested duplicate.
func TestDirFor(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base,
	}); err != nil {
		t.Fatal(err)
	}

	plain := &core.Task{Package: "Season 1"}
	if got := a.dirFor(plain); got != base {
		t.Errorf("without subfolders: %q, want %q", got, base)
	}

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base, SubfolderByPackage: true,
	}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "Season 1")
	if got := a.dirFor(plain); got != want {
		t.Errorf("with subfolders: %q, want %q", got, want)
	}

	// A task with its own folder is taken at its word, even with subfolders on.
	own := &core.Task{Package: "Season 1", Dir: filepath.Join(base, "elsewhere")}
	if got := a.dirFor(own); got != own.Dir {
		t.Errorf("task folder: %q, want %q", got, own.Dir)
	}

	// An unnamed package must not create a stray folder.
	if got := a.dirFor(&core.Task{}); got != base {
		t.Errorf("no package: %q, want %q", got, base)
	}
}

func TestSanitizeSegment(t *testing.T) {
	cases := map[string]string{
		"Season 1":       "Season 1",
		"a/b":            "a-b",
		"why?":           "why-",
		"  spaced  ":     "spaced",
		"...":            "package",
		"":               "package",
		"line\nbreak":    "line break",
		`quote"and<tag>`: "quote-and-tag-",
	}
	for in, want := range cases {
		if got := sanitizeSegment(in); got != want {
			t.Errorf("sanitizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}
