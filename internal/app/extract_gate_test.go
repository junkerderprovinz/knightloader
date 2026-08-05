package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestSetKeyGroupsVolumes pins which files belong to one archive. Getting this
// wrong either extracts a half-downloaded set or never extracts at all.
func TestSetKeyGroupsVolumes(t *testing.T) {
	same := [][]string{
		{"film.part01.rar", "film.part02.rar", "film.part10.rar"},
		{"film.rar", "film.r00", "film.r01"},
		{"film.7z.001", "film.7z.002"},
		{"film.zip", "film.z01", "film.z02"},
	}
	for _, group := range same {
		want, ok := extract.SetKey(group[0])
		if !ok {
			t.Fatalf("SetKey(%q) reported no set", group[0])
		}
		for _, n := range group[1:] {
			got, ok := extract.SetKey(n)
			if !ok || got != want {
				t.Errorf("SetKey(%q) = %q/%v, want %q", n, got, ok, want)
			}
		}
	}

	// Different archive families with the same base name are not one set.
	rar, _ := extract.SetKey("film.rar")
	zip, _ := extract.SetKey("film.zip")
	if rar == zip {
		t.Error("a .rar and a .zip of the same name were treated as one set")
	}

	// Things that are not volumes at all.
	for _, n := range []string{"film.mkv", "notes.txt", "backup.tar.gz"} {
		if _, ok := extract.SetKey(n); ok {
			t.Errorf("SetKey(%q) claimed a volume set", n)
		}
	}
}

// TestExtractWaitsForAllVolumes is the behaviour that matters: unpacking a
// multi-part archive must not start while a part is still downloading.
func TestExtractWaitsForAllVolumes(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), Extract: true,
	}); err != nil {
		t.Fatal(err)
	}

	p1 := &core.Task{ID: "1", Name: "film.part01.rar", Status: core.StatusDone}
	p2 := &core.Task{ID: "2", Name: "film.part02.rar", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[p1.ID], a.tasks[p2.ID] = p1, p2
	target, _ := a.extractCandidateLocked(p1)
	a.mu.Unlock()
	if target != nil {
		t.Fatalf("started extraction while %q was still downloading", p2.Name)
	}

	// Once the last part lands, the FIRST volume is what gets opened.
	a.mu.Lock()
	p2.Status = core.StatusDone
	target, path := a.extractCandidateLocked(p2)
	a.mu.Unlock()
	if target == nil {
		t.Fatal("the complete set did not trigger an extraction")
	}
	if target.ID != p1.ID {
		t.Errorf("extracted %q, want the first volume %q", target.Name, p1.Name)
	}
	if path == "" {
		t.Error("no path handed to the extractor")
	}
}

// TestSingleArchiveExtractsImmediately keeps the common case unaffected by the
// volume gate.
func TestSingleArchiveExtractsImmediately(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	solo := &core.Task{ID: "1", Name: "release.zip", Status: core.StatusDone}
	a.mu.Lock()
	a.tasks[solo.ID] = solo
	target, _ := a.extractCandidateLocked(solo)
	a.mu.Unlock()
	if target == nil || target.ID != solo.ID {
		t.Fatal("a single archive was not extracted")
	}

	plain := &core.Task{ID: "2", Name: "film.mkv", Status: core.StatusDone}
	a.mu.Lock()
	a.tasks[plain.ID] = plain
	target, _ = a.extractCandidateLocked(plain)
	a.mu.Unlock()
	if target != nil {
		t.Error("a plain file was handed to the extractor")
	}
}
