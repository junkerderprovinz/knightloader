package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestBackupToIsARestorableSnapshot is the property the whole feature rests
// on: what VACUUM INTO writes has to be a database internal/backup and a
// restore can open on its own, not merely a file that exists.
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

	// The live store must be untouched by taking a backup of it — a
	// snapshot is a read, and the caller goes on saving to the original
	// path afterwards.
	live, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != len(want) {
		t.Fatalf("live store has %d tasks after BackupTo, want %d", len(live), len(want))
	}
}

// TestBackupToRefusesAnExistingPath pins VACUUM INTO's own behaviour, which
// callers rely on: BackupTo is always given a fresh temporary path, and a
// silent overwrite here would be a silent overwrite of whatever a previous,
// abandoned backup attempt left behind.
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

// TestBackupToConcurrentWithSaves is the actual point of using VACUUM INTO
// instead of a raw file copy: the single shared connection (SetMaxOpenConns
// in Open) serialises the snapshot against every writer, so it never
// observes a torn write. It cannot prove the absence of a race, only that
// the snapshot it took is always internally consistent — which a corrupt or
// unopenable result here would immediately disprove.
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
