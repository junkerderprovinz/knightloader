package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// If this fails, a crawljob's intent is parsed and then thrown away on the way
// to the task: the file said where to put it, how urgently and in how many
// pieces, and the list shows a plain download with none of it.
func TestADroppedJobsIntentReachesTheTasks(t *testing.T) {
	a := newCrawlApp(t, false)
	dest := t.TempDir()
	priority := 2
	extract := false

	a.stageWatchJob(watch.Job{
		URLs:      []string{"https://host.example/one.bin"},
		Package:   "From The Share",
		Dir:       dest,
		Passwords: []string{"first", "second"},
		Priority:  &priority,
		Chunks:    4,
		Comment:   "dropped in by the box",
		Extract:   &extract,
	})

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	got := tasks[0]
	if got.Package != "From The Share" {
		t.Errorf("Package = %q, want the name the file gave", got.Package)
	}
	if got.Dir != dest {
		t.Errorf("Dir = %q, want the destination the file gave", got.Dir)
	}
	if got.Password != "first" {
		t.Errorf("Password = %q, want the first the file listed", got.Password)
	}
	if got.Priority != priority {
		t.Errorf("Priority = %d, want %d", got.Priority, priority)
	}
	if got.Chunks != 4 {
		t.Errorf("Chunks = %d, want 4", got.Chunks)
	}
	if got.Comment != "dropped in by the box" {
		t.Errorf("Comment = %q, want the file's note", got.Comment)
	}
	if got.AutoExtract == nil || *got.AutoExtract {
		t.Errorf("AutoExtract = %v, want the file's explicit no", got.AutoExtract)
	}
	if !got.Enabled {
		t.Error("Enabled = false for a job that never asked for the links to be parked")
	}
	// The second password is no use to this task and every use to the next
	// archive from the same source.
	if pw := a.Settings.Get().ArchivePasswords; len(pw) == 0 {
		t.Error("the passwords the file carried were not kept for later archives")
	}
}

// If this fails, enabled=FALSE in a dropped file is ignored and links somebody
// staged deliberately parked start downloading unattended.
func TestADisabledDroppedJobIsParked(t *testing.T) {
	a := newCrawlApp(t, false)
	a.stageWatchJob(watch.Job{
		URLs:      []string{"https://host.example/one.bin"},
		Disabled:  true,
		AutoStart: true, // contradicted by the parked flag, which has to win
	})

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	if tasks[0].Enabled {
		t.Fatal("Enabled = true, want the links parked as the file asked")
	}
}

// If this fails, a dropped file naming one output file renames every task it
// created, and twenty downloads are pointed at one destination.
func TestAFileNameIsOnlyTakenFromASingleLinkJob(t *testing.T) {
	a := newCrawlApp(t, false)
	a.stageWatchJob(watch.Job{
		URLs:     []string{"https://host.example/one.bin", "https://host.example/two.bin"},
		Filename: "the-one-name.bin",
	})
	for _, task := range a.Tasks() {
		if task.Filename == "the-one-name.bin" {
			t.Fatalf("%s took the file name of a two-link job", task.URL)
		}
	}
}

// If this fails, only one drop folder is ever polled, which is the whole of what
// a second one was configured for.
func TestEveryConfiguredDropFolderIsWatched(t *testing.T) {
	a := newCrawlApp(t, false)
	one, two := t.TempDir(), t.TempDir()
	t.Setenv(envWatchDirs, two)

	a.applyWatchFolders(settings.Settings{WatchDir: one})
	if dirs := watchedDirs(a); len(dirs) != 2 {
		t.Fatalf("watching %v, want the settings folder and the one from the environment", dirs)
	}

	// Taking the environment away leaves the folder the settings still name,
	// rather than tearing the whole intake down.
	os.Unsetenv(envWatchDirs)
	a.applyWatchFolders(settings.Settings{WatchDir: one})
	if dirs := watchedDirs(a); len(dirs) != 1 {
		t.Fatalf("watching %v, want only the folder the settings name", dirs)
	}

	// And clearing the setting stops the watcher outright, which is what the
	// module switch on the features page turns into.
	a.applyWatchFolders(settings.Settings{})
	if dirs := watchedDirs(a); len(dirs) != 0 {
		t.Fatalf("watching %v after the folder was cleared, want nothing", dirs)
	}
}

// If this fails, the same directory reached the watcher twice and one dropped
// file would be staged twice.
func TestOneDirectoryNamedTwiceIsListedOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envWatchDirs, dir+string(filepath.Separator))

	folders := watchFolders(settings.Settings{WatchDir: dir})
	if len(folders) != 2 {
		// The reader keeps both spellings on purpose: telling them apart is the
		// watcher's job, and it does it on the resolved path.
		t.Fatalf("watchFolders = %v, want both spellings handed on", folders)
	}
	a := newCrawlApp(t, false)
	a.applyWatchFolders(settings.Settings{WatchDir: dir})
	if dirs := watchedDirs(a); len(dirs) != 1 {
		t.Fatalf("watching %v, want the two spellings of one directory collapsed", dirs)
	}
}

func watchedDirs(a *App) []string {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	if a.watcher == nil {
		return nil
	}
	return a.watcher.Dirs()
}
