package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticBackupsShipOffWithAWeekKept(t *testing.T) {
	d := Defaults()
	if d.BackupIntervalDays != 0 {
		t.Errorf("backupIntervalDays ships at %d, want 0: an update must not start writing archives by itself", d.BackupIntervalDays)
	}
	if d.BackupKeep != DefaultBackupKeep || DefaultBackupKeep != 7 {
		t.Errorf("backupKeep ships at %d, want 7", d.BackupKeep)
	}
}

func TestSanitizeBackupClampsBothNumbers(t *testing.T) {
	for _, c := range []struct {
		interval, keep         int
		wantInterval, wantKeep int
	}{
		{interval: -1, keep: 7, wantInterval: 0, wantKeep: 7},
		{interval: 1, keep: 0, wantInterval: 1, wantKeep: 1},
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

func TestASettingsFileFromBeforeAutomaticBackupsKeepsAWeek(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"maxConcurrent":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.BackupIntervalDays != 0 || got.BackupKeep != DefaultBackupKeep {
		t.Errorf("an older settings.json loads with interval %d and keep %d", got.BackupIntervalDays, got.BackupKeep)
	}
}
