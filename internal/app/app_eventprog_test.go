package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// startedRun is what a fake runner saw of one run.
type startedRun struct {
	args []string
	env  map[string]string
}

// fakePrograms swaps the app's dispatcher for one whose runner records instead
// of starting anything, and returns what it records.
func fakePrograms(t *testing.T, a *App) <-chan startedRun {
	t.Helper()
	runs := make(chan startedRun, 16)
	d := a.newEventPrograms(func(_ context.Context, _ string, args, env []string) (string, error) {
		r := startedRun{args: args, env: map[string]string{}}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			r.env[k] = v
		}
		runs <- r
		return "", nil
	})
	t.Cleanup(func() { _ = d.Close() })
	a.EventPrograms = d
	a.Events.Subscribe("test", d.On)
	return runs
}

func withProgram(t *testing.T, a *App, edit func(*settings.Settings)) {
	t.Helper()
	s := a.Settings.Get()
	s.Categories = []settings.Category{{ID: "films", Name: "Films"}}
	s.EventPrograms = []eventprog.Program{{
		ID: "1", Name: "after", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "after", Args: []string{"%%file%%"}},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}}
	if edit != nil {
		edit(&s)
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
}

func TestAProgramIsHandedTheFinishedFileItsFolderAndCategory(t *testing.T) {
	a := newQueueApp(t)
	runs := fakePrograms(t, a)
	withProgram(t, a, nil)

	dir := t.TempDir()
	file := filepath.Join(dir, "film.mkv")
	task := putTask(t, a, core.Task{
		URL: "https://host.example/film.mkv", Name: "film.mkv", Package: "Pack",
		Status: core.StatusDone, Enabled: true, File: file, Category: "films",
	})
	tv := scriptTaskView(*task)
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskDone, Task: &tv})

	select {
	case r := <-runs:
		if len(r.args) != 1 || r.args[0] != file {
			t.Errorf("args = %q, want the file", r.args)
		}
		for name, want := range map[string]string{
			eventprog.EnvFile: file, eventprog.EnvFolder: dir, eventprog.EnvCategory: "Films",
			eventprog.EnvPackage: "Pack", eventprog.EnvTaskID: task.ID, eventprog.EnvEvent: "task.done",
		} {
			if r.env[name] != want {
				t.Errorf("%s = %q, want %q", name, r.env[name], want)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the saved program never ran")
	}
}

func TestTheModulesSwitchKeepsEveryProgramFromStarting(t *testing.T) {
	a := newQueueApp(t)
	runs := fakePrograms(t, a)
	withProgram(t, a, func(s *settings.Settings) { s.ModulesOff = []string{"eventprograms"} })

	tv := script.TaskView{ID: "x", Name: "a.mkv"}
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskDone, Task: &tv})
	select {
	case r := <-runs:
		t.Fatalf("a program ran while the module was off: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}
}

func nextRun(t *testing.T, runs <-chan startedRun) startedRun {
	t.Helper()
	select {
	case r := <-runs:
		return r
	case <-time.After(30 * time.Second):
		t.Fatal("the saved program never ran")
	}
	return startedRun{}
}

func TestAPackageIsGivenTheFolderMostOfItsFinishedFilesWentTo(t *testing.T) {
	a := newQueueApp(t)
	withProgram(t, a, nil)
	most, other, busy := t.TempDir(), t.TempDir(), t.TempDir()
	for _, task := range []core.Task{
		{Name: "e01.mkv", Dir: most, Category: "films"},
		{Name: "e02.mkv", Dir: most, Category: "films"},
		{Name: "sample.mkv", Dir: other},
		// Still arriving, so not where the package went, however many.
		{Name: "e03.mkv", Dir: busy, Status: core.StatusRunning},
		{Name: "e04.mkv", Dir: busy, Status: core.StatusRunning},
		{Name: "e05.mkv", Dir: busy, Status: core.StatusRunning},
	} {
		task.URL, task.Package, task.Enabled = "https://host.example/"+task.Name, "Pack", true
		if task.Status == "" {
			task.Status = core.StatusDone
		}
		putTask(t, a, task)
	}

	w := a.locateEvent(script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Pack"}})
	if w.Folder != most || w.Category != "Films" {
		t.Errorf("package.done was located at %q in %q, want %q in Films", w.Folder, w.Category, most)
	}
}

func TestAnUnpackingIsGivenTheFolderItsContentEndedUpIn(t *testing.T) {
	a := newQueueApp(t)
	a.mu.Lock()
	jobs := a.unpackLocked().jobs
	jobs["moved"] = &extractJob{ExtractJob: ExtractJob{ID: "moved", Dir: "/work/Set", MovedTo: "/media/Set"}}
	jobs["stayed"] = &extractJob{ExtractJob: ExtractJob{ID: "stayed", Dir: "/downloads/Set"}}
	a.mu.Unlock()

	for job, want := range map[string]string{"moved": "/media/Set", "stayed": "/downloads/Set"} {
		dir := jobs[job].Dir
		w := a.locateEvent(script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{JobID: job, Dir: dir}})
		if w.Folder != want {
			t.Errorf("the %s unpacking was located at %q, want %q", job, w.Folder, want)
		}
	}
}

func TestATieBetweenFoldersGoesToTheOneThatSortsFirst(t *testing.T) {
	if got := mostCommon([]string{"/b", "/a", "/b", "", ""}); got != "/b" {
		t.Errorf("mostCommon = %q, want the folder two files share", got)
	}
	if got := mostCommon([]string{"/b", "/a"}); got != "/a" {
		t.Errorf("mostCommon = %q on a tie, want the one that sorts first", got)
	}
	if got := mostCommon(nil); got != "" {
		t.Errorf("mostCommon(nil) = %q", got)
	}
}

func TestAProgramFindsAFinishedDownloadWhereItWasMovedTo(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	runs := fakePrograms(t, a)
	withProgram(t, a, nil)
	staged := stagedIn(t, workdir.For(work, base), "film.mkv", "the whole film")
	task := stageFiled(t, a, "1", "film.mkv", "Der.Film", "")
	editTask(a, "1", func(task *core.Task) { task.File = staged })
	tv := scriptTaskView(*task)
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskDone, Task: &tv})

	select {
	case r := <-runs:
		t.Fatalf("the program ran while its file was still in the working folder: %v", r.env[eventprog.EnvFile])
	case <-time.After(300 * time.Millisecond):
	}
	a.deliverDownload("1")
	r := nextRun(t, runs)
	if want := filepath.Join(base, "film.mkv"); r.env[eventprog.EnvFile] != want {
		t.Errorf("%s = %q, want %q, where the file was moved to", eventprog.EnvFile, r.env[eventprog.EnvFile], want)
	}
}

func TestAPackageProgramWaitsForEveryFileToLeaveTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.WorkDir = work
	})
	stagedIn(t, workdir.For(work, base), "film.mkv", "the whole film")
	stageFiled(t, a, "1", "film.mkv", "Der.Film", "")
	f := script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Der.Film"}}
	if a.eventReady(f) {
		t.Fatal("package.done reads as ready while its file is still in the working folder")
	}
	a.deliverDownload("1")
	if !a.eventReady(f) {
		t.Error("package.done still waits after its file was moved")
	}
}

func TestALinkThatNeverGotANameIsHandedNoFile(t *testing.T) {
	a := newQueueApp(t)
	runs := fakePrograms(t, a)
	withProgram(t, a, func(s *settings.Settings) {
		s.EventPrograms[0].Triggers = []script.Trigger{script.TriggerTaskFailed}
	})
	link := "https://host.example/file/abc"
	task := putTask(t, a, core.Task{URL: link, Name: link, Status: core.StatusError, Enabled: true})
	tv := scriptTaskView(*task)
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskFailed, Task: &tv})

	r := nextRun(t, runs)
	if r.env[eventprog.EnvFile] != "" || r.env[eventprog.EnvFolder] != "" {
		t.Errorf("%s, %s = %q, %q for a link that never had a file", eventprog.EnvFile, eventprog.EnvFolder,
			r.env[eventprog.EnvFile], r.env[eventprog.EnvFolder])
	}
}
