package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestBackupToIsARestorableSnapshot: what VACUUM INTO writes must open as a
// store on its own.
func TestBackupToIsARestorableSnapshot(t *testing.T) {
	s := open(t)
	want := []*core.Task{
		{ID: "a", Name: "one.bin", CreatedAt: time.Now()},
		{ID: "b", Name: "two.bin", Status: core.StatusDone, CreatedAt: time.Now()},
	}
	for _, task := range want {
		if err := s.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	dst := filepath.Join(t.TempDir(), "backup.db")
	if err := s.BackupTo(dst); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	backup, err := Open(dst)
	if err != nil {
		t.Fatalf("the snapshot could not be opened as a store: %v", err)
	}
	defer backup.Close()
	got, err := backup.All()
	if err != nil {
		t.Fatalf("All on the snapshot: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("snapshot has %d tasks, want %d", len(got), len(want))
	}

	// Taking a snapshot leaves the live store untouched.
	live, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != len(want) {
		t.Fatalf("live store has %d tasks after BackupTo, want %d", len(live), len(want))
	}
}

// TestBackupToRefusesAnExistingPath pins VACUUM INTO's refusal to overwrite,
// which callers rely on.
func TestBackupToRefusesAnExistingPath(t *testing.T) {
	s := open(t)
	dst := filepath.Join(t.TempDir(), "backup.db")
	if err := s.BackupTo(dst); err != nil {
		t.Fatalf("first BackupTo: %v", err)
	}
	if err := s.BackupTo(dst); err == nil {
		t.Fatal("a second BackupTo onto the same path should have failed, not overwritten it")
	}
}

// TestBackupToConcurrentWithSaves: the single connection serialises the
// snapshot against writers, so every snapshot taken during saves opens and
// reads cleanly.
func TestBackupToConcurrentWithSaves(t *testing.T) {
	s := open(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.Save(&core.Task{ID: "hot", Name: "file.bin", Loaded: int64(i), CreatedAt: time.Now()})
			i++
		}
	}()

	for i := 0; i < 10; i++ {
		dst := filepath.Join(t.TempDir(), "backup.db")
		if err := s.BackupTo(dst); err != nil {
			close(stop)
			<-done
			t.Fatalf("BackupTo while saves were in flight: %v", err)
		}
		snap, err := Open(dst)
		if err != nil {
			close(stop)
			<-done
			t.Fatalf("snapshot %d could not be opened: %v", i, err)
		}
		if _, err := snap.All(); err != nil {
			snap.Close()
			close(stop)
			<-done
			t.Fatalf("snapshot %d could not be read: %v", i, err)
		}
		snap.Close()
	}
	close(stop)
	<-done
}
