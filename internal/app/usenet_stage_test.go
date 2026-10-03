package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
)

func TestStagingAJobAgainTakesTheTasksItBecame(t *testing.T) {
	a := newCrawlApp(t, false)
	j := usenet.Job{Name: "Show", Package: "Show", Service: "torbox", Label: "TorBox", Remote: "9"}
	files := []usenet.File{{ID: "1", Name: "a.mkv", Size: 5}, {ID: "2", Name: "b.mkv", Size: 6}}

	first, err := a.stageUsenetFiles(j, files)
	if err != nil {
		t.Fatal(err)
	}
	// As after a restart that came before the job list was saved.
	second, err := a.stageUsenetFiles(j, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || !slices.Equal(first, second) {
		t.Errorf("staged %v, then %v; want the same two tasks both times", first, second)
	}
	if n := len(a.Tasks()); n != 2 {
		t.Errorf("the list holds %d tasks, want 2", n)
	}
}

func TestFilesInFoldersKeepThemUnderThePackage(t *testing.T) {
	own := filepath.Join(t.TempDir(), "Season")
	for _, dir := range []string{"", own} {
		a := newCrawlApp(t, false)
		j := usenet.Job{Name: "Season", Package: "Season", Dir: dir, Service: "torbox", Label: "TorBox", Remote: "9"}
		ids, err := a.stageUsenetFiles(j, []usenet.File{
			{ID: "2", Name: "2_English.srt", Dir: "Subs/Show.S01E01", Size: 10},
			{ID: "3", Name: "2_English.srt", Dir: "Subs/Show.S01E02", Size: 11},
			{ID: "1", Name: "Show.S01E01.mkv", Size: 100},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 3 {
			t.Fatalf("staged %v, want all three files", ids)
		}
		top := a.TaskFolder(ids[2])
		if dir != "" && top != dir {
			t.Errorf("the file at the top of a job with a folder of its own lands in %s, want %s", top, dir)
		}
		for i, sub := range []string{"Show.S01E01", "Show.S01E02"} {
			if got, want := a.TaskFolder(ids[i]), filepath.Join(top, "Subs", sub); got != want {
				t.Errorf("subtitle %d lands in %s, want %s", i+1, got, want)
			}
		}
	}
}

func TestAFailedNZBIsListedWithTheLinksThatDidNotMakeIt(t *testing.T) {
	a := newCrawlApp(t, false)
	a.SetUsenetServices(&readyService{refuse: errors.New("torbox createusenetdownload: BOZO_NZB this nzb is invalid")})
	if _, err := a.AddNZB(NZB{Name: "Broken", Data: []byte(droppedNZB), Origin: OriginContainer}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the refusal to be listed", func() bool {
		for _, s := range a.SkippedLinks() {
			if s.URL == "Broken.nzb" && s.Kind == "nzb" && strings.Contains(s.Reason, "invalid") {
				return true
			}
		}
		return false
	})
}

// The own servers fetch a held recovery volume from the .nzb they keep, so a
// job with one still held is not over once the rest is done.
func TestAHeldRecoveryVolumeKeepsItsJobOpen(t *testing.T) {
	a := newCrawlApp(t, false)
	const job = "0123456789abcdef"
	a.mu.Lock()
	a.tasks["ep"] = &core.Task{ID: "ep", URL: local.FileLink(job, 0, "show.mkv"), Name: "show.mkv", Status: core.StatusDone, Enabled: true}
	spare := &core.Task{ID: "vol", URL: local.FileLink(job, 1, "show.vol00+01.par2"), Name: "show.vol00+01.par2", Status: core.StatusQueued}
	a.tasks["vol"] = spare
	held := HeldSpare(spare)
	a.mu.Unlock()
	if !held {
		t.Fatal("the recovery volume is not held, so this test proves nothing")
	}
	if a.tasksFinished([]string{"ep", "vol"}) {
		t.Fatal("the job counts as finished, which deletes the .nzb the held volume is fetched from")
	}
}
