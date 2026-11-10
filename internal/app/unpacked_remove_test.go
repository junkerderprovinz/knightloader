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
		if len(got) != 1 || got[0].Path != filepath.Join(base, "notes.txt") || got[0].Files != 1 || got[0].Bytes != 20 {
			t.Errorf("part %s records %+v, want the joined file of 20 bytes", id, got)
		}
	}
}
