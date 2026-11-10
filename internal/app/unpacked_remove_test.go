package app

// Removing a download with its files takes what unpacking it wrote as well,
// and nothing else that happens to be near it.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// unpackedApp starts an instance that unpacks only when asked, with mutate
// applied to its settings before they take effect.
func unpackedApp(t *testing.T, dataDir string, mutate func(s *settings.Settings, base string)) (*App, string) {
	t.Helper()
	return newRuleAppIn(t, dataDir, func(s *settings.Settings, base string) {
		s.Extract, s.VerifyChecksums = false, false
		if mutate != nil {
			mutate(s, base)
		}
	})
}

// downloadedZip writes a zip holding one file at path and stages the finished
// download that wrote it.
func downloadedZip(t *testing.T, a *App, id, path, entry string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeZip(t, path, entry, "unpacked "+id)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	task := stageDone(t, a, id, filepath.Base(path))
	a.mu.Lock()
	task.Dir, task.File, task.Size, task.Loaded = filepath.Dir(path), path, fi.Size(), fi.Size()
	a.mu.Unlock()
}

// unpack unpacks the archives of ids and waits for every job to finish.
func unpack(t *testing.T, a *App, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := a.StartExtraction([]string{id}); err != nil {
			t.Fatal(err)
		}
		waitForJob(t, a, id, ExtractDone)
	}
}

func TestRemovingWithFilesDeletesWhatUnpackingWrote(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	archive := filepath.Join(base, "release.zip")
	downloadedZip(t, a, "1", archive, "inside.txt")
	unpack(t, a, "1")
	out := filepath.Join(base, "release")
	if !fileExists(filepath.Join(out, "inside.txt")) {
		t.Fatal("the archive was never unpacked, so this proves nothing")
	}

	a.Remove("1", true)

	if fileExists(archive) {
		t.Error("the archive survived a removal with files")
	}
	if fileExists(out) {
		t.Error("the folder the archive unpacked into survived a removal with files")
	}
	if !fileExists(base) {
		t.Error("the download folder went with the unpacked files")
	}
}

func TestRemovingWithFilesKeepsTheUnpackFolderItself(t *testing.T) {
	collect := filepath.Join(t.TempDir(), "unpacked")
	if err := os.MkdirAll(collect, 0o755); err != nil {
		t.Fatal(err)
	}
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) { s.ExtractTo = collect })
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")

	a.Remove("1", true)

	if fileExists(filepath.Join(collect, "release")) {
		t.Error("the unpacked folder survived a removal with files")
	}
	if !fileExists(collect) {
		t.Error("the folder every archive unpacks into was deleted")
	}
}

func TestRemovingWithFilesSparesAFolderThatWasThereAndWhatElseIsInIt(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) { s.ExtractCollision = "overwrite" })
	out := filepath.Join(base, "release")
	notes := fileBytes(t, filepath.Join(out, "notes.txt"), 64)
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	if !fileExists(filepath.Join(out, "inside.txt")) {
		t.Fatal("the archive did not unpack into the folder that was there")
	}

	a.Remove("1", true)

	if fileExists(filepath.Join(out, "inside.txt")) {
		t.Error("the unpacked file survived a removal with files")
	}
	if !fileExists(notes) {
		t.Error("a file that was in the folder before the unpacking was deleted")
	}
}

// captureLog collects what the instance logs for the rest of the test.
func captureLog(t *testing.T) *lockedLog {
	t.Helper()
	logged := &lockedLog{}
	prev := log.Writer()
	log.SetOutput(logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	return logged
}

func TestRemovingWithFilesSparesAFileThatArrivedInTheUnpackedFolderLater(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	later := fileBytes(t, filepath.Join(base, "release", "subtitles.srt"), 64)
	logged := captureLog(t)

	a.Remove("1", true)

	if !fileExists(later) {
		t.Error("a file that arrived after the unpacking was deleted with it")
	}
	if want := "left " + filepath.Join(base, "release") + " where it is"; !strings.Contains(logged.String(), want) {
		t.Errorf("the log does not say what was left and why:\n%s", logged)
	}
}

// An archive unpacked with nothing recorded about it leaves its files, and the
// log says so.
func TestRemovingWithFilesSaysWhenNothingRecordsWhatWasUnpacked(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	out := fileBytes(t, filepath.Join(base, "release", "inside.txt"), 10)
	a.mu.Lock()
	a.tasks["1"].Unpack = core.UnpackDone
	a.mu.Unlock()
	logged := captureLog(t)

	a.Remove("1", true)

	if !fileExists(out) {
		t.Error("a file nothing records as unpacked was deleted")
	}
	if !strings.Contains(logged.String(), "left what unpacking release.zip wrote where it is") {
		t.Errorf("the log does not say the unpacked files were left:\n%s", logged)
	}
}

func TestRemovingWithFilesAfterARestartDeletesWhatUnpackingWrote(t *testing.T) {
	dataDir, base := t.TempDir(), t.TempDir()
	a, stop := bootUnpackApp(t, dataDir, base)
	archive := filepath.Join(base, "release.zip")
	downloadedZip(t, a, "1", archive, "inside.txt")
	unpack(t, a, "1")
	stop()

	again, _ := bootUnpackApp(t, dataDir, base)
	if jobs := again.ExtractJobs(); len(jobs) != 0 {
		t.Fatalf("the restarted instance knows %d jobs, so this proves nothing about the task", len(jobs))
	}
	again.Remove("1", true)

	if fileExists(archive) {
		t.Error("the archive survived a removal with files after a restart")
	}
	if fileExists(filepath.Join(base, "release")) {
		t.Error("the unpacked folder survived a removal with files after a restart")
	}
}

func TestRemovingWithoutFilesKeepsTheUnpackedFilesAndItsUndoBringsThemBack(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	archive := filepath.Join(base, "release.zip")
	downloadedZip(t, a, "1", archive, "inside.txt")
	unpack(t, a, "1")
	out := filepath.Join(base, "release")

	_, token := a.RemoveTasksUndoable([]string{"1"}, false)
	if !fileExists(archive) || !fileExists(filepath.Join(out, "inside.txt")) {
		t.Fatal("a removal from the list took files with it")
	}
	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("the undo brought back %d rows, want 1", len(back))
	}

	a.Remove("1", true)
	if fileExists(out) {
		t.Error("the row brought back by the undo no longer deletes what its unpacking wrote")
	}
}

func TestTheUnpackedFilesOfASetGoWithItsLastPart(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	parts := []string{"notes.txt.001", "notes.txt.002", "notes.txt.003"}
	for i, name := range parts {
		path := fileBytes(t, filepath.Join(base, name), 100+i)
		task := stageDone(t, a, fmt.Sprint(i+1), name)
		a.mu.Lock()
		task.File, task.Size = path, int64(100+i)
		a.mu.Unlock()
	}
	unpack(t, a, "1")
	joined := filepath.Join(base, "notes.txt")
	if !fileExists(joined) {
		t.Fatal("the set was never joined, so this proves nothing")
	}

	a.Remove("1", true)
	if !fileExists(joined) {
		t.Fatal("the joined file went while two parts of its set are still listed")
	}
	a.RemoveTasks([]string{"2", "3"}, true)
	if fileExists(joined) {
		t.Error("the joined file survived the removal of the whole set with its files")
	}
}

func TestTwoUnpackingsIntoOneFolderKeepItUntilBothAreRemoved(t *testing.T) {
	collect := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
		s.ExtractTo, s.ExtractCollision = collect, "overwrite"
	})
	downloadedZip(t, a, "1", filepath.Join(base, "first", "release.zip"), "one.txt")
	downloadedZip(t, a, "2", filepath.Join(base, "second", "release.zip"), "two.txt")
	unpack(t, a, "1", "2")
	out := filepath.Join(collect, "release")
	if !fileExists(filepath.Join(out, "one.txt")) || !fileExists(filepath.Join(out, "two.txt")) {
		t.Fatal("the two archives did not unpack into one folder")
	}

	a.Remove("1", true)
	if !fileExists(filepath.Join(out, "two.txt")) {
		t.Fatal("removing the first download deleted what the second one unpacked")
	}
	a.Remove("2", true)
	if fileExists(out) {
		t.Error("the shared folder survived the removal of both downloads with their files")
	}
}

func TestRemovingWithFilesFollowsTheUnpackedFilesWhereTheyWereMoved(t *testing.T) {
	library := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) { s.ExtractMoveTo = library })
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	moved := filepath.Join(library, "inside.txt")
	if !fileExists(moved) {
		t.Fatal("the unpacked file was never moved, so this proves nothing")
	}

	a.Remove("1", true)

	if fileExists(moved) {
		t.Error("the moved file survived a removal with files")
	}
	if !fileExists(library) {
		t.Error("the folder the files were moved into was deleted")
	}
}

func TestTheUnpackedFilesAreRecordedOnEveryPartOfTheSet(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	for i, name := range []string{"notes.txt.001", "notes.txt.002"} {
		path := fileBytes(t, filepath.Join(base, name), 10)
		task := stageDone(t, a, fmt.Sprint(i+1), name)
		a.mu.Lock()
		task.File, task.Size = path, 10
		a.mu.Unlock()
	}
	unpack(t, a, "1")
	for _, id := range []string{"1", "2"} {
		got := liveTask(a, id).Unpacked
		if len(got) != 1 || got[0].Path != filepath.Join(base, "notes.txt") || got[0].Files["."] != 20 || len(got[0].Files) != 1 {
			t.Errorf("part %s records %+v, want the joined file of 20 bytes", id, got)
		}
	}
}

// unpackAgain unpacks a task's archive once more and waits for the row to
// settle, which a finished job from the first time cannot tell.
func unpackAgain(t *testing.T, a *App, id string) {
	t.Helper()
	if err := a.StartExtraction([]string{id}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second unpacking to settle", func() bool { return liveTask(a, id).Status == core.StatusDone })
}

func TestUnpackingAgainLeavesAFileAddedSince(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) { s.ExtractCollision = "overwrite" })
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	mine := fileBytes(t, filepath.Join(base, "release", "subtitles.srt"), 64)
	unpackAgain(t, a, "1")

	a.Remove("1", true)

	if !fileExists(mine) {
		t.Error("a file added after the first unpacking was deleted after unpacking again")
	}
}

func TestAnotherUnpackingIntoTheFolderLeavesAFileAddedSince(t *testing.T) {
	collect := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
		s.ExtractTo, s.ExtractCollision = collect, "overwrite"
	})
	downloadedZip(t, a, "1", filepath.Join(base, "first", "release.zip"), "one.txt")
	unpack(t, a, "1")
	mine := fileBytes(t, filepath.Join(collect, "release", "subtitles.srt"), 64)
	downloadedZip(t, a, "2", filepath.Join(base, "second", "release.zip"), "two.txt")
	unpack(t, a, "2")

	a.Remove("2", true)
	a.Remove("1", true)

	if !fileExists(mine) {
		t.Error("a file added to the folder between two unpackings into it was deleted")
	}
}

// Two archives that unpack into one folder, the first removed from the list
// only and the second with its files: what the first one unpacked stays, in
// either order.
func TestRemovingOneOfTwoUnpackingsFromTheListKeepsTheFolderTheyShare(t *testing.T) {
	for _, order := range [][2]string{{"1", "2"}, {"2", "1"}} {
		t.Run("keep "+order[0], func(t *testing.T) {
			collect := t.TempDir()
			a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
				s.ExtractTo, s.ExtractCollision = collect, "overwrite"
			})
			downloadedZip(t, a, "1", filepath.Join(base, "first", "release.zip"), "1.txt")
			downloadedZip(t, a, "2", filepath.Join(base, "second", "release.zip"), "2.txt")
			unpack(t, a, "1", "2")
			kept := filepath.Join(collect, "release", order[0]+".txt")
			if !fileExists(kept) {
				t.Fatal("the archives did not unpack into one folder")
			}

			a.Remove(order[0], false)
			a.Remove(order[1], true)

			if !fileExists(kept) {
				t.Errorf("what task %s unpacked went with task %s, although %s was removed from the list only", order[0], order[1], order[0])
			}
		})
	}
}

// The user deleted the unpacked file and put a smaller one of their own in
// its folder, so the folder holds fewer bytes than the unpacking left.
func TestRemovingWithFilesSparesAFolderWhoseFilesWereSwapped(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), nil)
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	out := filepath.Join(base, "release")
	if err := os.Remove(filepath.Join(out, "inside.txt")); err != nil {
		t.Fatal(err)
	}
	mine := fileBytes(t, filepath.Join(out, "mine.txt"), 1)
	logged := captureLog(t)

	a.Remove("1", true)

	if !fileExists(mine) {
		t.Error("a file the unpacking did not write was deleted with its folder")
	}
	if want := "left " + out + " where it is"; !strings.Contains(logged.String(), want) {
		t.Errorf("the log does not say what was left and why:\n%s", logged)
	}
}

// renamedPackageApp unpacks one archive of package A into its own folder under
// collect.
func renamedPackageApp(t *testing.T) (*App, string) {
	t.Helper()
	collect := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
		s.ExtractTo, s.ExtractSubfolder = collect, true
	})
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	editTask(a, "1", func(x *core.Task) { x.Package = "A" })
	unpack(t, a, "1")
	if !fileExists(filepath.Join(collect, "A", "release", "inside.txt")) {
		t.Fatalf("the archive did not unpack into its package's folder: %+v", liveTask(a, "1").Unpacked)
	}
	return a, collect
}

func TestARenameOntoATakenFolderLeavesThatFolderToItsPackage(t *testing.T) {
	a, collect := renamedPackageApp(t)
	foreign := fileBytes(t, filepath.Join(collect, "B", "release", "foreign.txt"), 5)
	if _, err := a.RenamePackage([]string{"1"}, "B"); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(collect, "B", "release (2)", "inside.txt")
	if !fileExists(moved) {
		t.Fatal("the rename did not move the unpacked folder beside the one that was there")
	}

	a.Remove("1", true)

	if !fileExists(foreign) {
		t.Error("removing the renamed package with its files deleted the folder another package had unpacked")
	}
	if fileExists(moved) {
		t.Error("what the renamed package unpacked survived a removal with files")
	}
}

func TestARenameOntoAFreeNameStillDeletesWhatWasUnpacked(t *testing.T) {
	a, collect := renamedPackageApp(t)
	if _, err := a.RenamePackage([]string{"1"}, "B"); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(collect, "B", "release")
	if !fileExists(filepath.Join(moved, "inside.txt")) {
		t.Fatal("the rename did not move the unpacked folder")
	}

	a.Remove("1", true)

	if fileExists(moved) {
		t.Error("what the renamed package unpacked survived a removal with files")
	}
}

func TestUnpackingAgainStillDeletesWhatItWroteWithItsDownload(t *testing.T) {
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) { s.ExtractCollision = "overwrite" })
	downloadedZip(t, a, "1", filepath.Join(base, "release.zip"), "inside.txt")
	unpack(t, a, "1")
	unpackAgain(t, a, "1")

	a.Remove("1", true)

	if fileExists(filepath.Join(base, "release")) {
		t.Error("the folder unpacked twice survived a removal with files")
	}
}

// An undo of a removal from the list takes back what it kept, so the folder
// goes again with the last of the two downloads removed with files.
func TestUndoingARemovalFromTheListLetsTheSharedFolderGoAgain(t *testing.T) {
	collect := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
		s.ExtractTo, s.ExtractCollision = collect, "overwrite"
	})
	downloadedZip(t, a, "1", filepath.Join(base, "first", "release.zip"), "one.txt")
	downloadedZip(t, a, "2", filepath.Join(base, "second", "release.zip"), "two.txt")
	unpack(t, a, "1", "2")
	_, first := a.RemoveTasksUndoable([]string{"1"}, false)
	_, second := a.RemoveTasksUndoable([]string{"2"}, false)
	for _, token := range []string{first, second} {
		if back := a.UndoRemove(token); len(back) != 1 {
			t.Fatalf("the undo brought back %d rows, want 1", len(back))
		}
	}

	a.RemoveTasks([]string{"1", "2"}, true)

	if out := filepath.Join(collect, "release"); fileExists(out) {
		t.Errorf("the shared folder survived the removal of both downloads with their files: %+v", liveTask(a, "2").Unpacked)
	}
}

// The confirmation asks the server what a removal with files would take of
// what the selection unpacked, and that is what the removal deletes.
func TestTheUnpackedWeightIsWhatARemovalWithFilesDeletes(t *testing.T) {
	collect := t.TempDir()
	a, base := unpackedApp(t, t.TempDir(), func(s *settings.Settings, _ string) {
		s.ExtractTo, s.ExtractCollision = collect, "overwrite"
	})
	downloadedZip(t, a, "1", filepath.Join(base, "first", "release.zip"), "one.txt")
	downloadedZip(t, a, "2", filepath.Join(base, "second", "release.zip"), "two.txt")
	downloadedZip(t, a, "3", filepath.Join(base, "third", "other.zip"), "three.txt")
	unpack(t, a, "1", "2", "3")
	size := func(path string) int64 {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size()
	}
	one, two := size(filepath.Join(collect, "release", "one.txt")), size(filepath.Join(collect, "release", "two.txt"))
	three := size(filepath.Join(collect, "other", "three.txt"))

	for _, c := range []struct {
		name  string
		ids   []string
		files int
		bytes int64
	}{
		{"what another download unpacked into as well stays", []string{"1"}, 0, 0},
		{"its own file goes", []string{"2"}, 1, two},
		{"both take the folder", []string{"1", "2"}, 2, one + two},
		{"in either order", []string{"2", "1"}, 2, one + two},
		{"a folder of its own goes", []string{"3"}, 1, three},
	} {
		if files, bytes := a.UnpackedWeight(c.ids); files != c.files || bytes != c.bytes {
			t.Errorf("%s: %v weighs %d files, %d bytes, want %d, %d", c.name, c.ids, files, bytes, c.files, c.bytes)
		}
	}

	fileBytes(t, filepath.Join(collect, "other", "mine.txt"), 3)
	if files, _ := a.UnpackedWeight([]string{"3"}); files != 0 {
		t.Errorf("a folder holding a file of the user's weighs %d files, want none: it stays", files)
	}

	downloadedZip(t, a, "4", filepath.Join(collect, "release", "next.zip"), "four.txt")
	if files, bytes := a.UnpackedWeight([]string{"1", "2"}); files != 1 || bytes != two {
		t.Errorf("with another download's file in the folder, %d files, %d bytes go, want only two.txt: the folder stays", files, bytes)
	}
}
