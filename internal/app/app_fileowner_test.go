package app

// The folder list, and the one thing that will actually go wrong with it: it
// drifting away from the disk report's list without anybody noticing.
//
// Both lists are built from the same configuration in the same order, and they
// are deliberately not the same function - the disk report measures bytes and
// stops at the folders downloads land in, this one also has to cover the drop
// folder because KL deletes the .crawljob it consumed there. So the two agree
// about their shared part BY CONVENTION, which is exactly the kind of agreement
// that quietly stops being true when somebody adds a fifth configurable folder
// to one of them.

import (
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// ownerApp is an app whose folders the test writes directly into the settings
// store.
//
// Settings.Set rather than ApplySettings, and that is not a shortcut:
// ApplySettings reconciles the live watcher, so setting WatchDir through it
// would start a real polling goroutine over a temporary directory for the rest
// of the test binary's life. Nothing in this file reads the watcher; both lists
// under test read a.Settings.Get() and nothing else.
func ownerApp(t *testing.T, mutate func(*settings.Settings)) *App {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	mutate(&s)
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestTheOwnershipListCoversEveryFolderTheDiskReportNamesPlusTheDropFolder is
// the drift guard. A fifth configured folder added to sampleDiskReport and not
// here would be a folder whose free space is watched and whose ownership is
// never checked - and the failure that produces is silent by construction,
// because the ownership page would simply not have a row for it.
//
// Task rows are skipped: those come out of the queue rather than out of the
// configuration (a per-task override, a rule pointing somewhere else), they
// change as downloads are added and removed, and a check that wrote a probe
// file into every destination the queue happens to mention would be writing
// into folders nobody configured.
func TestTheOwnershipListCoversEveryFolderTheDiskReportNamesPlusTheDropFolder(t *testing.T) {
	dl, work, cat, watch := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	a := ownerApp(t, func(s *settings.Settings) {
		s.DownloadDir = dl
		s.WorkDir = work
		s.Categories = []settings.Category{{ID: "filme", Name: "Filme", Dir: cat}}
		s.WatchDir = watch
	})

	targets := map[string]string{}
	for _, f := range a.TargetFolders() {
		targets[f.Dir] = f.Role
	}
	for _, v := range a.DiskReport().Volumes {
		if v.Role == roleTask {
			continue
		}
		role, ok := targets[v.Dir]
		if !ok {
			t.Errorf("the disk report measures %s as the %s folder and the ownership check never looks at it", v.Dir, v.Role)
			continue
		}
		if role != v.Role {
			t.Errorf("%s is the %s folder to the disk report and the %s folder to the ownership check", v.Dir, v.Role, role)
		}
	}
	if got := targets[filepath.Clean(watch)]; got != roleWatch {
		t.Errorf("the drop folder is in the ownership list as %q, want %q; KL deletes the job it consumed there, "+
			"and a folder it cannot delete from re-imports the same links on every poll", got, roleWatch)
	}
}

// TestAPlaceholderTemplateIsCutBackToTheFolderThatReallyExists. A download
// folder is very often "/downloads/<jd:date>/<jd:packagename>", and a directory
// literally called "<jd:date>" is never on disk. Probing the template as written
// would report a missing folder on a completely healthy install, and probing it
// after creating it would put that folder there.
func TestAPlaceholderTemplateIsCutBackToTheFolderThatReallyExists(t *testing.T) {
	base := t.TempDir()
	a := ownerApp(t, func(s *settings.Settings) {
		s.DownloadDir = filepath.Join(base, "media") + string(filepath.Separator) + "<jd:date>"
	})
	want := filepath.Join(base, "media")
	for _, f := range a.TargetFolders() {
		if f.Role != roleDownloads {
			continue
		}
		if f.Dir != want {
			t.Fatalf("the download folder template was reported as %q, want the fixed head %q", f.Dir, want)
		}
		return
	}
	t.Fatalf("no download folder at all in %v", a.TargetFolders())
}

// TestOneFolderGetsOneRowAndTheFirstRoleWinsIt. An install whose category
// folder is the download folder is ordinary, and two rows about one directory
// means two probe files written into it and two verdicts to reconcile on screen
// when they are about the same thing.
func TestOneFolderGetsOneRowAndTheFirstRoleWinsIt(t *testing.T) {
	dl := t.TempDir()
	a := ownerApp(t, func(s *settings.Settings) {
		s.DownloadDir = dl
		s.Categories = []settings.Category{{ID: "filme", Name: "Filme", Dir: dl}}
		s.WatchDir = dl
	})
	var rows int
	for _, f := range a.TargetFolders() {
		if f.Dir != filepath.Clean(dl) {
			continue
		}
		rows++
		if f.Role != roleDownloads {
			t.Errorf("the shared folder is listed as the %s folder; the first role to name it should have won", f.Role)
		}
	}
	if rows != 1 {
		t.Errorf("one directory produced %d rows", rows)
	}
}

// TestARelativeFolderIsLeftOutRatherThanResolved. It would resolve against
// whatever the process's working directory happens to be, which is why
// sanitizePaths refuses to store one - but a category folder is not covered by
// that refusal, and probing "downloads" from a service started in / would
// measure the wrong folder with total confidence.
func TestARelativeFolderIsLeftOutRatherThanResolved(t *testing.T) {
	a := ownerApp(t, func(s *settings.Settings) {
		s.Categories = []settings.Category{{ID: "filme", Name: "Filme", Dir: "relative/folder"}}
	})
	for _, f := range a.TargetFolders() {
		if !filepath.IsAbs(f.Dir) {
			t.Errorf("a relative folder reached the list: %+v", f)
		}
	}
}
