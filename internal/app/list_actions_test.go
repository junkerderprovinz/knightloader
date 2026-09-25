package app

// The verbs a package header's menu offers over every link in the package:
// renaming it, pausing and resuming it.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newPackageApp names each package's folder after the package, the setting
// under which a package rename can move where files go.
func newPackageApp(t *testing.T) (*App, string) {
	t.Helper()
	return newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.SubfolderByPackage = true
	})
}

// Before anything has been downloaded there is no file to lose track of, so
// the folder takes the new name with the package. A row that shares a link
// with a named one comes along even though nobody named it, or one link's
// rows would end up in two folders.
func TestAPackageNothingHasStartedInTakesItsFolderAlong(t *testing.T) {
	a, base := newPackageApp(t)
	putTask(t, a, core.Task{ID: "a", URL: "https://host.example/a.bin", Name: "a.bin",
		Package: "Old", Status: core.StatusCollected, Enabled: true})
	putTask(t, a, core.Task{ID: "b", URL: "https://host.example/b.bin", Name: "b.bin",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})
	putTask(t, a, core.Task{ID: "b-audio", URL: "https://host.example/b.bin", Name: "b.bin",
		Package: "Old", Status: core.StatusCollected, Enabled: true})

	got, err := a.RenamePackage([]string{"a", "b"}, "  New  ")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "b", "b-audio"}) {
		t.Errorf("renamed %v, want both named rows and the one sharing a link", got)
	}
	for _, id := range []string{"a", "b", "b-audio"} {
		live := liveTask(a, id)
		if live.Package != "New" {
			t.Errorf("%s is in package %q, want %q", id, live.Package, "New")
		}
		if live.Dir != "" {
			t.Errorf("%s had its folder written down as %q; nothing had started, so it should follow the name", id, live.Dir)
		}
		if !live.ManualPackage {
			t.Errorf("%s does not record the package as chosen by hand, so a probe could put the old one back", id)
		}
		if got, want := a.TaskFolder(id), filepath.Join(base, "New"); got != want {
			t.Errorf("%s downloads to %q, want %q", id, got, want)
		}
	}
}

// Once one file of a package is on disk, the whole package keeps its folder. A
// finished file looked for under the new name is a file the list has lost, and
// the parts still waiting must land beside the ones already there, or an
// archive split across two folders is never unpacked.
func TestAPackageWithAFileOnDiskKeepsItsFolder(t *testing.T) {
	a, base := newPackageApp(t)
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	finishedTask(t, a, old, "part1", "film.part1.rar")
	editTask(a, "part1", func(x *core.Task) { x.Package = "Old" })
	putTask(t, a, core.Task{ID: "part2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})

	if _, err := a.RenamePackage([]string{"part1", "part2"}, "Film"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"part1", "part2"} {
		if live := liveTask(a, id); live.Package != "Film" {
			t.Errorf("%s is in package %q, want the new name", id, live.Package)
		}
		if got := a.TaskFolder(id); got != old {
			t.Errorf("%s downloads to %q, want the folder the package already had, %q", id, got, old)
		}
	}
	if _, err := os.Stat(filepath.Join(a.TaskFolder("part1"), "film.part1.rar")); err != nil {
		t.Errorf("the finished part is not where its row says it is: %v", err)
	}
}

// Moving files into another package is the same hazard as renaming theirs:
// once one is on disk, the set keeps the folder it has, or the list looks for
// the finished part in the new package's folder and the waiting one lands
// apart from it.
func TestMovingAFinishedFileIntoAPackageKeepsItsFolder(t *testing.T) {
	a, base := newPackageApp(t)
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	finishedTask(t, a, old, "part1", "film.part1.rar")
	editTask(a, "part1", func(x *core.Task) { x.Package = "Old" })
	putTask(t, a, core.Task{ID: "part2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})

	a.SetPackage([]string{"part1", "part2"}, "Film")

	for _, id := range []string{"part1", "part2"} {
		if live := liveTask(a, id); live.Package != "Film" {
			t.Errorf("%s is in package %q, want the one it was moved to", id, live.Package)
		}
		if got := a.TaskFolder(id); got != old {
			t.Errorf("%s downloads to %q, want the folder it already had, %q", id, got, old)
		}
	}
	if _, err := os.Stat(filepath.Join(a.TaskFolder("part1"), "film.part1.rar")); err != nil {
		t.Errorf("the finished part is not where its row says it is: %v", err)
	}
}

// Links nothing has been fetched for have no file to lose, so they follow the
// package they are moved into.
func TestMovingLinksNothingHasStartedInTakesThemToTheNewFolder(t *testing.T) {
	a, base := newPackageApp(t)
	putTask(t, a, core.Task{ID: "a", URL: "https://host.example/a.bin", Name: "a.bin",
		Package: "Old", Status: core.StatusCollected, Enabled: true})

	a.SetPackage([]string{"a"}, "New")

	if live := liveTask(a, "a"); live.Dir != "" {
		t.Errorf("Dir = %q, want it left to follow the package", live.Dir)
	}
	if got, want := a.TaskFolder("a"), filepath.Join(base, "New"); got != want {
		t.Errorf("a downloads to %q, want %q", got, want)
	}
}

// Links moved out of two packages at once keep their folder only where one of
// them has written to it. The package nothing has started in has no file to
// lose, so it follows the move like any other.
func TestMovingTwoPackagesKeepsOnlyTheFolderWithAFileInIt(t *testing.T) {
	a, base := newPackageApp(t)
	old := filepath.Join(base, "Old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	finishedTask(t, a, old, "part1", "film.part1.rar")
	editTask(a, "part1", func(x *core.Task) { x.Package = "Old" })
	putTask(t, a, core.Task{ID: "part2", URL: "https://host.example/film.part2.rar", Name: "film.part2.rar",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})
	putTask(t, a, core.Task{ID: "extra", URL: "https://host.example/extra.bin", Name: "extra.bin",
		Package: "Other", Status: core.StatusCollected, Enabled: true})

	a.SetPackage([]string{"part1", "part2", "extra"}, "Film")

	for _, id := range []string{"part1", "part2"} {
		if got := a.TaskFolder(id); got != old {
			t.Errorf("%s downloads to %q, want the folder its set already has, %q", id, got, old)
		}
	}
	if live := liveTask(a, "extra"); live.Dir != "" {
		t.Errorf("the untouched link was pinned to %q, want it left to follow the package", live.Dir)
	}
	if got, want := a.TaskFolder("extra"), filepath.Join(base, "Film"); got != want {
		t.Errorf("the untouched link downloads to %q, want %q", got, want)
	}
}

// A link already handed to a backend counts as started before it reports a
// byte: the backend was told where to write, and resumes there.
func TestAPackageWithALinkHandedToABackendKeepsItsFolder(t *testing.T) {
	a, base := newPackageApp(t)
	putTask(t, a, core.Task{ID: "handed", URL: "https://host.example/h.bin", Name: "h.bin",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})
	putTask(t, a, core.Task{ID: "waiting", URL: "https://host.example/w.bin", Name: "w.bin",
		Package: "Old", Status: core.StatusQueued, Enabled: true, Hold: true})
	a.mu.Lock()
	a.started["handed"] = true
	a.mu.Unlock()

	if _, err := a.RenamePackage([]string{"handed", "waiting"}, "New"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"handed", "waiting"} {
		if got, want := a.TaskFolder(id), filepath.Join(base, "Old"); got != want {
			t.Errorf("%s downloads to %q, want the folder the backend already writes to, %q", id, got, want)
		}
	}
}

// A folder that was never named after the package does not change with it, so
// nothing is written into Dir: a pinned folder would stop following the
// download folder setting for no reason.
func TestAFolderNotNamedAfterThePackageIsLeftToTheSettings(t *testing.T) {
	a, base := newQuietApp(t)
	finishedTask(t, a, base, "1", "one.bin")

	if _, err := a.RenamePackage([]string{"1"}, "Anything"); err != nil {
		t.Fatal(err)
	}
	if live := liveTask(a, "1"); live.Dir != "" {
		t.Errorf("Dir = %q, want it left empty since the folder does not depend on the package", live.Dir)
	}
}

// A package name becomes a folder name, so it is held to the rule a file name
// is: one segment, and something in it.
func TestAPackageNameHasToBeOneSegment(t *testing.T) {
	for _, in := range []string{"", "   ", "Season 1/Episode 2", `a\b`, ".."} {
		t.Run(in, func(t *testing.T) {
			a, _ := newPackageApp(t)
			putTask(t, a, core.Task{ID: "1", URL: "https://host.example/1.bin", Name: "1.bin",
				Package: "Old", Status: core.StatusCollected, Enabled: true})

			if _, err := a.RenamePackage([]string{"1"}, in); err == nil {
				t.Fatalf("%q was taken as a package name", in)
			}
			if live := liveTask(a, "1"); live.Package != "Old" {
				t.Errorf("the package became %q despite the refusal", live.Package)
			}
		})
	}
}

// Pausing a package names every link in it, and only the running and waiting
// ones have anything to pause. A finished link turned "paused" would be offered
// a resume that downloads it again, and a staged one would leave the collector.
func TestPausingASelectionLeavesTheOtherStatesAlone(t *testing.T) {
	a := newQueueApp(t)
	running := putTask(t, a, core.Task{ID: "running", URL: "https://host.example/r.bin",
		Status: core.StatusRunning, Enabled: true})
	waiting := putTask(t, a, core.Task{ID: "waiting", URL: "https://host.example/w.bin",
		Status: core.StatusQueued, Enabled: true, Hold: true})
	others := map[string]core.Status{
		"staged":    core.StatusCollected,
		"done":      core.StatusDone,
		"unpacking": core.StatusExtracting,
		"failed":    core.StatusError,
	}
	for id, status := range others {
		putTask(t, a, core.Task{ID: id, URL: "https://host.example/" + id + ".bin", Status: status, Enabled: true})
	}
	a.mu.Lock()
	a.active[running.ID] = true
	a.started[running.ID] = true
	a.queue = []string{waiting.ID}
	a.mu.Unlock()

	got := a.PauseTasks([]string{"running", "waiting", "staged", "done", "unpacking", "failed", "gone"})

	if !slices.Equal(got, []string{"running", "waiting"}) {
		t.Errorf("paused %v, want only the running and the waiting link", got)
	}
	for _, id := range []string{"running", "waiting"} {
		if s := liveTask(a, id).Status; s != core.StatusPaused {
			t.Errorf("%s is %q, want paused", id, s)
		}
	}
	for id, status := range others {
		if s := liveTask(a, id).Status; s != status {
			t.Errorf("%s went from %q to %q", id, status, s)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active[running.ID] {
		t.Error("the paused download still holds its slot")
	}
	if slices.Contains(a.queue, waiting.ID) {
		t.Error("the paused link is still in the wait queue, so the next free slot would start it")
	}
}

// One pass for the whole selection. Pausing link by link would hand the slot
// the first one frees to the next link of the same package, which is then
// paused again a moment after it started.
func TestPausingAPackageDoesNotStartItsOwnLinks(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 1, MaxPerHost: 1, DownloadDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	putTask(t, a, core.Task{ID: "first", URL: "https://host.example/1.bin", Status: core.StatusRunning, Enabled: true})
	putTask(t, a, core.Task{ID: "second", URL: "https://host.example/2.bin", Status: core.StatusQueued, Enabled: true})
	a.mu.Lock()
	a.active["first"] = true
	a.started["first"] = true
	a.queue = []string{"second"}
	a.mu.Unlock()

	a.PauseTasks([]string{"first", "second"})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active["second"] || a.started["second"] {
		t.Error("the second link was started in the slot the first one freed")
	}
	if s := a.tasks["second"].Status; s != core.StatusPaused {
		t.Errorf("the second link is %q, want paused", s)
	}
}

// Resuming a package puts back what was paused and nothing else: a finished
// link must not be queued to download again.
func TestResumingPutsOnlyPausedLinksBack(t *testing.T) {
	a := newQueueApp(t)
	// Held, so the dispatcher leaves them queued instead of reaching for a
	// network this test does not have.
	putTask(t, a, core.Task{ID: "paused", URL: "https://host.example/p.bin", Status: core.StatusPaused, Enabled: true, Hold: true})
	putTask(t, a, core.Task{ID: "done", URL: "https://host.example/d.bin", Status: core.StatusDone, Enabled: true, Hold: true})

	got := a.ResumeTasks([]string{"paused", "done"})

	if !slices.Equal(got, []string{"paused"}) {
		t.Errorf("resumed %v, want only the paused link", got)
	}
	if s := liveTask(a, "paused").Status; s != core.StatusQueued {
		t.Errorf("the paused link is %q, want queued", s)
	}
	if s := liveTask(a, "done").Status; s != core.StatusDone {
		t.Errorf("the finished link is %q, want it left done", s)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !slices.Contains(a.queue, "paused") {
		t.Error("the resumed link is not in the wait queue, so nothing would ever start it")
	}
	if slices.Contains(a.queue, "done") {
		t.Error("the finished link was put in the wait queue")
	}
}
