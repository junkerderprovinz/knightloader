package settings

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAutomaticBackupsShipOffWithADayAndAWeekKept(t *testing.T) {
	d := Defaults()
	if d.BackupAuto {
		t.Error("backupAuto ships on: an update must not start writing archives by itself")
	}
	if d.BackupIntervalDays != 1 || d.BackupKeep != 7 || d.BackupDir != "" {
		t.Errorf("the schedule ships as every %d days, keeping %d, in %q; want 1, 7 and the data folder",
			d.BackupIntervalDays, d.BackupKeep, d.BackupDir)
	}
}

func TestSanitizeBackupClampsBothNumbers(t *testing.T) {
	for _, c := range []struct {
		interval, keep         int
		wantInterval, wantKeep int
	}{
		{interval: 0, keep: 7, wantInterval: 1, wantKeep: 7},
		{interval: -3, keep: 0, wantInterval: 1, wantKeep: 1},
		{interval: 7, keep: -3, wantInterval: 7, wantKeep: 1},
		{interval: 100000, keep: 100000, wantInterval: maxBackupIntervalDays, wantKeep: maxBackupKeep},
	} {
		got := sanitize(Settings{BackupIntervalDays: c.interval, BackupKeep: c.keep})
		if got.BackupIntervalDays != c.wantInterval || got.BackupKeep != c.wantKeep {
			t.Errorf("interval %d, keep %d sanitised to %d, %d; want %d, %d",
				c.interval, c.keep, got.BackupIntervalDays, got.BackupKeep, c.wantInterval, c.wantKeep)
		}
	}
}

func TestSanitizeBackupDropsARelativeFolder(t *testing.T) {
	if got := sanitize(Settings{BackupDir: "  backups  "}).BackupDir; got != "" {
		t.Errorf("a relative backup folder sanitised to %q, want the default", got)
	}
	abs := filepath.Join(t.TempDir(), "kl-backups")
	if got := sanitize(Settings{BackupDir: " " + abs + " "}).BackupDir; got != abs {
		t.Errorf("an absolute backup folder sanitised to %q, want %q", got, abs)
	}
}

func TestASaveRefusesARelativeBackupFolder(t *testing.T) {
	err := CheckFolders(t.TempDir(), Settings{BackupDir: "backups"}, nil)
	var p *PathProblem
	if !errors.As(err, &p) || p.Field != "backupDir" || p.Code != "notAbsolute" {
		t.Errorf("CheckFolders = %v, want backupDir refused as not absolute", err)
	}
}

// The default folder is inside the data folder, so a chosen one may be too.
func TestABackupFolderInsideTheDataFolderIsAllowed(t *testing.T) {
	data := t.TempDir()
	s := Settings{BackupDir: filepath.Join(data, "elsewhere")}
	if err := CheckFolders(data, s, nil); err != nil {
		t.Errorf("CheckFolders refused a backup folder inside the data folder: %v", err)
	}
}

func TestAMissingBackupFolderPassesAndIsNotLeftBehind(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "kl", "backups")
	if err := CheckFolders(t.TempDir(), Settings{BackupDir: dir}, nil); err != nil {
		t.Fatalf("CheckFolders refused a folder that can be made: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("the check left %v behind; a path half typed into the box would stay on disk", entries)
	}
}

func TestTheBackupFolderProbeLeavesItsContentsAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckFolders(t.TempDir(), Settings{BackupDir: dir}, nil); err != nil {
		t.Fatalf("CheckFolders refused a writable folder: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "mine.txt" {
		t.Errorf("after the check the folder holds %v", entries)
	}
}

func TestABackupFolderThatCannotBeMadeIsRefusedAtItsField(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocker, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckFolders(t.TempDir(), Settings{BackupDir: filepath.Join(blocker, "backups")}, nil)
	var p *PathProblem
	if !errors.As(err, &p) || p.Field != "backupDir" || p.Code != "cannotCreate" {
		t.Errorf("CheckFolders = %v, want backupDir refused as impossible to create", err)
	}
}

func TestAReadOnlyBackupFolderIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix modes that bind this process")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	err := CheckFolders(t.TempDir(), Settings{BackupDir: dir}, nil)
	var p *PathProblem
	if !errors.As(err, &p) || p.Field != "backupDir" || p.Code != "cannotWrite" {
		t.Errorf("CheckFolders = %v, want backupDir refused as not writable", err)
	}
}
