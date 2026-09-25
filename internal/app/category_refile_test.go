package app

// Filing links under another category after they were staged, which Sonarr
// does after every import when it has a category to move them to.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A finished download is on disk in the old category's folder, so its row has
// to keep pointing there, while a link nothing has been fetched for follows
// the category it was moved to.
func TestRefilingAFinishedDownloadKeepsItsFolder(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, base string) {
		s.Extract, s.VerifyChecksums = false, false
		s.SubfolderByPackage = true
		s.Categories = []settings.Category{
			{ID: "tv-sonarr", Name: "tv-sonarr", Dir: filepath.Join(base, "tv")},
			{ID: "tv-imported", Name: "tv-imported"},
		}
	})
	folder := filepath.Join(base, "tv", "Show.S01E01")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	finishedTask(t, a, folder, "done", "show.s01e01.mkv")
	editTask(a, "done", func(x *core.Task) { x.Package, x.Category = "Show.S01E01", "tv-sonarr" })
	putTask(t, a, core.Task{ID: "waiting", URL: "https://host.example/show.s01e02.mkv", Name: "show.s01e02.mkv",
		Package: "Show.S01E02", Category: "tv-sonarr", Status: core.StatusCollected, Enabled: true})
	if got := a.TaskFolder("done"); got != folder {
		t.Fatalf("test setup: the finished download is in %q, want %q", got, folder)
	}

	got := a.SetCategory([]string{"done", "waiting"}, "tv-imported")

	if len(got) != 2 {
		t.Errorf("refiled %v, want both links", got)
	}
	for _, id := range []string{"done", "waiting"} {
		if c := liveTask(a, id).Category; c != "tv-imported" {
			t.Errorf("%s is in category %q, want tv-imported", id, c)
		}
	}
	if got := a.TaskFolder("done"); got != folder {
		t.Errorf("the finished download is looked for in %q, want %q where its file is", got, folder)
	}
	if _, err := os.Stat(filepath.Join(a.TaskFolder("done"), "show.s01e01.mkv")); err != nil {
		t.Errorf("the finished file is not where its row says it is: %v", err)
	}
	if got, want := a.TaskFolder("waiting"), filepath.Join(base, "Show.S01E02"); got != want {
		t.Errorf("the waiting link downloads to %q, want %q under its new category", got, want)
	}
}
