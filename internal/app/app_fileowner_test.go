package app

import (
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// ownerApp returns an app with the given folders written straight into the
// settings store. ApplySettings would start a real watch-folder poller.
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

// TestTheOwnershipListCoversEveryFolderTheDiskReportNamesPlusTheDropFolder keeps
// the two folder lists from drifting apart. Task rows are skipped because they
// come from the queue, not from the configuration.
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

// TestARelativeFolderIsLeftOutRatherThanResolved covers category folders, which
// sanitizePaths does not refuse when they are relative.
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
