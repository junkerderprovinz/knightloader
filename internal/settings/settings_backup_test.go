package settings

import (
	"errors"
	"path/filepath"
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
