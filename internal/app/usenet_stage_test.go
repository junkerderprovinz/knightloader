package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/usenet"
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
	a := newCrawlApp(t, false)
	j := usenet.Job{Name: "Season", Package: "Season", Service: "torbox", Label: "TorBox", Remote: "9"}
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
	byID := map[string]string{}
	for _, task := range a.Tasks() {
		byID[task.ID] = task.Name
	}
	if byID[ids[0]] != "Show.S01E01.mkv" {
		t.Errorf("the first task is %q, want the file at the top", byID[ids[0]])
	}
	top := a.TaskFolder(ids[0])
	for i, sub := range []string{"Show.S01E01", "Show.S01E02"} {
		if got, want := a.TaskFolder(ids[i+1]), filepath.Join(top, "Subs", sub); got != want {
			t.Errorf("subtitle %d lands in %s, want %s", i+1, got, want)
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
