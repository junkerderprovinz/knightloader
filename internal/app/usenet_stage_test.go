package app

import (
	"context"
	"errors"
	"os"
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
	if a.jobFinished(usenet.Job{Service: local.ResolverID, TaskIDs: []string{"ep", "vol"}}) {
		t.Fatal("the job counts as finished, which deletes the .nzb the held volume is fetched from")
	}
}

func TestAFinishedFileOfTheOwnServersKeepsItsNZBForARestart(t *testing.T) {
	a := newCrawlApp(t, false)
	own := a.usenetStateFor().own
	job, err := own.Submit(context.Background(), "Show", []byte(droppedNZB))
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks["ep"] = &core.Task{ID: "ep", URL: local.FileLink(job, 0, "show.mkv"), Name: "show.mkv", Status: core.StatusDone, Enabled: true}
	a.tasks["tb"] = &core.Task{ID: "tb", URL: "https://store.example/show.mkv", Name: "show.mkv", Status: core.StatusDone, Enabled: true}
	a.mu.Unlock()

	if a.jobFinished(usenet.Job{Service: local.ResolverID, Remote: job, TaskIDs: []string{"ep"}}) {
		t.Error("the own servers' job counts as finished while its file is listed, so a restart finds its .nzb deleted")
	}
	if !a.jobFinished(usenet.Job{Service: "torbox", Remote: "9", TaskIDs: []string{"tb"}}) {
		t.Error("a TorBox job whose file is done keeps its copy on the account")
	}
	a.RestartTasks([]string{"ep"})
	if got := statusOf(a, "ep"); got == core.StatusDone {
		t.Error("a finished file whose .nzb is kept is not restarted")
	}
}

func TestAFinishedFileWhoseNZBIsGoneIsNotRestarted(t *testing.T) {
	a := newCrawlApp(t, false)
	a.mu.Lock()
	a.tasks["ep"] = &core.Task{ID: "ep", URL: local.FileLink("0123456789abcdef", 0, "show.mkv"), Name: "show.mkv", Status: core.StatusDone, Enabled: true}
	a.mu.Unlock()
	a.RestartTasks([]string{"ep"})
	if got := statusOf(a, "ep"); got != core.StatusDone {
		t.Errorf("the file is %s after a restart, want it left done: it cannot be fetched again", got)
	}
}

func TestARemovalThatCanStillBeUndoneKeepsTheJob(t *testing.T) {
	a := newCrawlApp(t, false)
	a.mu.Lock()
	a.tasks["tb"] = &core.Task{ID: "tb", URL: "https://store.example/show.mkv", Name: "show.mkv", Status: core.StatusQueued, Enabled: true}
	a.mu.Unlock()
	job := usenet.Job{Service: "torbox", Remote: "9", TaskIDs: []string{"tb"}}

	if _, token := a.RemoveTasksUndoable([]string{"tb"}, false); token == "" {
		t.Fatal("the removal cannot be undone, so this test proves nothing")
	}
	if a.jobFinished(job) {
		t.Fatal("the job counts as finished during the undo window, so an undo brings back a task whose copy is deleted")
	}
}

func statusOf(a *App, id string) core.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.tasks[id]; t != nil {
		return t.Status
	}
	return ""
}

func TestARestartNamesTheFinishedFilesItCannotFetchAgain(t *testing.T) {
	a := newCrawlApp(t, false)
	a.mu.Lock()
	a.tasks["ep"] = &core.Task{ID: "ep", URL: local.FileLink("0123456789abcdef", 0, "show.mkv"), Name: "show.mkv", Status: core.StatusDone, Enabled: true}
	a.tasks["other"] = &core.Task{ID: "other", URL: local.FileLink("0123456789abcdef", 1, "show.nfo"), Name: "show.nfo", Status: core.StatusDone, Enabled: true}
	a.mu.Unlock()
	if left := a.RestartTasksIn([]string{"ep"}, nil); !slices.Equal(left, []string{"show.mkv"}) {
		t.Errorf("the restart reports %q as left alone, want the one file asked for", left)
	}
}

func TestAnUndoneRemoveOfAUsenetFileFindsTheBytesItShows(t *testing.T) {
	a := newCrawlApp(t, false)
	a.mu.Lock()
	row := &core.Task{
		ID: "ep", URL: local.FileLink("0123456789abcdef", 0, "show.mkv"), Name: "show.mkv",
		Resolver: local.ResolverID, Status: core.StatusPaused, Size: 9000, Loaded: 4000, Enabled: true,
	}
	a.tasks[row.ID] = row
	part := a.usenetPartLocked(row)
	a.mu.Unlock()
	if err := os.WriteFile(part, make([]byte, 9000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part+".segments", []byte("klsegments 1 9000 9\n111100000"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, token := a.RemoveTasksUndoable([]string{row.ID}, false)
	if back := a.UndoRemove(token); !slices.Equal(back, []string{row.ID}) {
		t.Fatalf("the undo brought back %v", back)
	}
	for _, p := range []string{part, part + ".segments"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the row is back at %d bytes, but %s is gone", row.Loaded, filepath.Base(p))
		}
	}

	a.RemoveTasks([]string{row.ID}, true)
	for _, p := range []string{part, part + ".segments"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s is left after a removal with files", filepath.Base(p))
		}
	}
}
