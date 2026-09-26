package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestEveryGrabGetsAFolderNothingElseHas(t *testing.T) {
	t.Parallel()
	a := newQueueApp(t)
	shows := t.TempDir()
	s := a.Settings.Get()
	s.Categories = []settings.Category{{ID: "tv", Name: "tv", Dir: shows}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	// Somebody's own folder and file under the names the next grabs would get.
	if err := os.Mkdir(filepath.Join(shows, "Show.S01E01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shows, "Show.S01E01.1"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []string
	for range 2 {
		folder, err := a.GrabFolder("tv", "", "Show.S01E01")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, folder)
	}
	want := []string{filepath.Join(shows, "Show.S01E01.2"), filepath.Join(shows, "Show.S01E01.3")}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("two grabs of one release got %v, want %v", got, want)
	}
	for _, f := range got {
		if entries, err := os.ReadDir(f); err != nil || len(entries) != 0 {
			t.Errorf("%s is not an empty folder of its own: %v, %v", f, entries, err)
		}
	}

	// Without a category the download folder takes its place, and a save path
	// the caller names wins over both.
	if folder, err := a.GrabFolder("", "", "Film"); err != nil || filepath.Dir(folder) != a.Settings.Get().DownloadDir {
		t.Errorf("a grab without a category got %q (%v), want a folder in the download folder", folder, err)
	}
	elsewhere := t.TempDir()
	if folder, err := a.GrabFolder("tv", elsewhere, "Film"); err != nil || folder != filepath.Join(elsewhere, "Film") {
		t.Errorf("a grab with a save path got %q (%v), want %s", folder, err, filepath.Join(elsewhere, "Film"))
	}
	if _, err := a.GrabFolder("tv", "relative/path", "Film"); err == nil {
		t.Error("a relative save path was taken")
	}
}
