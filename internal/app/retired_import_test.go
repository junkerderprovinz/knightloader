package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestADownloadImportedFromADebridAccountFailsWithItsReasonAfterTheUpgrade(t *testing.T) {
	dir := t.TempDir()
	storeBefore(t, dir,
		core.Task{ID: "waiting", URL: "debrid://realdebrid/ABC123", Name: "Film", Status: core.StatusQueued, Enabled: true, CreatedAt: time.Now()},
		core.Task{ID: "paused", URL: "debrid://torbox/web/42?account=work", Name: "Show", Status: core.StatusPaused, Enabled: true, CreatedAt: time.Now()},
		core.Task{ID: "finished", URL: "debrid://realdebrid/DEF456", Name: "Done", Status: core.StatusDone, Enabled: true, CreatedAt: time.Now()},
	)
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	for _, id := range []string{"waiting", "paused"} {
		got := liveTask(a, id)
		if got.Status != core.StatusError || got.ErrorCode != core.CodeDebridImport {
			t.Errorf("%s comes back as %s with code %q, want failed with %q", id, got.Status, got.ErrorCode, core.CodeDebridImport)
		}
	}
	a.mu.Lock()
	queued := slices.Contains(a.queue, "waiting")
	a.mu.Unlock()
	if queued {
		t.Error("an imported download went back into the queue")
	}
	if got := liveTask(a, "finished"); got.Status != core.StatusDone || got.ErrorCode != "" {
		t.Errorf("a finished import comes back as %s with code %q, want it left done", got.Status, got.ErrorCode)
	}
}

func TestWhatTheImportMeantToDeleteIsDeletedOnceAndItsFilesGo(t *testing.T) {
	dir := t.TempDir()
	site := &fakeAccount{}
	a := openAccountApp(t, dir, site, false)
	t.Cleanup(func() { _ = a.Close() })
	putTask(t, a, core.Task{ID: "undone", URL: "https://host.example/back.bin", Status: core.StatusQueued})
	records := filepath.Join(dir, importRecordsFile)
	drops := filepath.Join(dir, importDropsFile)
	if err := os.WriteFile(records, []byte(`{"fakedebrid":{"since":"2026-09-01T00:00:00Z","seen":["J1","J2"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drops, []byte(`{
		"gone": {"task": "gone", "slot": "fakedebrid", "job": "J1", "due": "2026-09-01T00:00:00Z"},
		"undone": {"task": "undone", "slot": "fakedebrid", "job": "J2", "due": "2026-09-01T00:00:00Z"},
		"elsewhere": {"task": "elsewhere", "slot": "alldebrid", "job": "J3", "due": "2026-09-01T00:00:00Z"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a.settleRetiredImport()
	waitFor(t, "the download of the removed task being deleted", func() bool { return len(site.deletedJobs()) > 0 })
	for _, p := range []string{records, drops} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there (stat err = %v)", filepath.Base(p), err)
		}
	}
	a.settleRetiredImport()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := site.deletedJobs(); !slices.Equal(got, []string{"J1"}) {
		t.Errorf("deleted %q on the account, want only J1, the job of the task that is gone", got)
	}
}

func TestAnUnreadableListOfImportedDownloadsIsRemovedWithoutADelete(t *testing.T) {
	dir := t.TempDir()
	site := &fakeAccount{}
	a := openAccountApp(t, dir, site, false)
	drops := filepath.Join(dir, importDropsFile)
	if err := os.WriteFile(drops, []byte(`{"gone": {"task": "gone", "slot": "fakedebrid", "jo`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.settleRetiredImport()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(drops); !os.IsNotExist(err) {
		t.Errorf("the unreadable list is still there (stat err = %v)", err)
	}
	if got := site.deletedJobs(); len(got) != 0 {
		t.Errorf("deleted %q on the account from a list that could not be read", got)
	}
}
