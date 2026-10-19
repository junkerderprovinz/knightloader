package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
)

// backupApp returns an app on dir with automatic backups every intervalDays,
// keeping keep of them.
func backupApp(t *testing.T, dir string, intervalDays, keep int) *App {
	t.Helper()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	cfg := a.Settings.Get()
	cfg.BackupIntervalDays, cfg.BackupKeep = intervalDays, keep
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

// sweepBackup runs the scheduled check once and waits for what it started.
func sweepBackup(t *testing.T, a *App) AutoBackupState {
	t.Helper()
	a.runAutoBackupIfDue()
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := a.AutoBackupState()
		if err != nil {
			t.Fatal(err)
		}
		if !st.Running {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("the automatic backup was still running after 30 seconds")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fileNames(files []backup.AutoFile) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
}

func TestADueBackupWritesTheArchiveARestoreAccepts(t *testing.T) {
	t.Parallel()
	a := backupApp(t, t.TempDir(), 1, 7)

	st := sweepBackup(t, a)
	if len(st.Files) != 1 || st.Last == nil || st.Last.Error != "" {
		t.Fatalf("after one due sweep the state is %+v", st)
	}
	if st.Dir != filepath.Join(a.DataDir, backup.AutoDirName) {
		t.Errorf("dir = %s", st.Dir)
	}
	raw, err := os.ReadFile(filepath.Join(st.Dir, st.Files[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(t.TempDir(), raw, buildinfo.Version); err != nil {
		t.Errorf("a restore refuses the automatic backup: %v", err)
	}
	if st.NextRunAt == nil || st.NextRunAt.Before(time.Now().Add(23*time.Hour)) {
		t.Errorf("nextRunAt = %v, want a day after the backup", st.NextRunAt)
	}

	if again := sweepBackup(t, a); len(again.Files) != 1 {
		t.Errorf("a second sweep the same day wrote another backup: %v", fileNames(again.Files))
	}
}

func TestARestartDoesNotBackUpAgainBeforeTheInterval(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := backupApp(t, dir, 7, 7)
	if st := sweepBackup(t, first); len(st.Files) != 1 {
		t.Fatalf("the first start wrote %v", fileNames(st.Files))
	}
	first.Close()

	second := backupApp(t, dir, 7, 7)
	if st := sweepBackup(t, second); len(st.Files) != 1 {
		t.Errorf("a restart wrote another backup within the interval: %v", fileNames(st.Files))
	}
}

func TestAnOldBackupOnDiskMakesTheNextOneDue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	folder := filepath.Join(dir, backup.AutoDirName)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	old := backup.AutoName(time.Now().AddDate(0, 0, -8), false)
	if err := os.WriteFile(filepath.Join(folder, old), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := backupApp(t, dir, 7, 7)
	if st := sweepBackup(t, a); len(st.Files) != 2 {
		t.Errorf("a week and a day after the last backup the sweep left %v", fileNames(st.Files))
	}
}

func TestRetentionDeletesOnlyTheOldestAutomaticBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	folder := filepath.Join(dir, backup.AutoDirName)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	var old []string
	for days := 10; days >= 8; days-- {
		old = append(old, backup.AutoName(time.Now().AddDate(0, 0, -days), false))
	}
	others := []string{"knightloader-backup-20260101-000000.zip", "mine.zip"}
	for _, name := range append(slices.Clone(old), others...) {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a := backupApp(t, dir, 7, 2)
	st := sweepBackup(t, a)
	if got, want := fileNames(st.Files), []string{st.Last.File, old[2]}; !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
	for _, name := range others {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("%s, which KnightLoader did not write as an automatic backup, is gone: %v", name, err)
		}
	}
}

func TestAutomaticBackupsAreOffByDefault(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	st := sweepBackup(t, a)
	if len(st.Files) != 0 || st.Last != nil || st.NextRunAt != nil {
		t.Errorf("with the shipped settings the sweep did something: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, backup.AutoDirName)); !os.IsNotExist(err) {
		t.Errorf("the backups folder exists on an install that never asked for one: %v", err)
	}
	if row := a.backupSubsystem(a.Settings.Get()); row.State != StateUnused {
		t.Errorf("the health row reads %s", row.State)
	}
}

func TestABackupBelowTheLowSpaceFloorIsSkippedAndSaysWhy(t *testing.T) {
	t.Parallel()
	a := backupApp(t, t.TempDir(), 1, 7)
	if _, ok := diskspace.Free(a.DataDir); !ok {
		t.Skip("this platform cannot measure free space")
	}
	cfg := a.Settings.Get()
	cfg.DiskLowSpace = 1_000_000_000_000_000
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	st := sweepBackup(t, a)
	if len(st.Files) != 0 {
		t.Errorf("a backup was written below the floor: %v", fileNames(st.Files))
	}
	if st.Last == nil || st.Last.Skipped != AutoBackupSkippedLowSpace || st.Last.FloorBytes != cfg.DiskLowSpace {
		t.Errorf("last = %+v, want a low-space skip naming the floor", st.Last)
	}
	if row := a.backupSubsystem(cfg); row.State != StateDegraded || row.Remedy != remedyBackupLowSpace {
		t.Errorf("the health row reads %+v", row)
	}

	cfg.DiskLowSpace = 0
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if st := sweepBackup(t, a); len(st.Files) != 1 || st.Last.Skipped != "" {
		t.Errorf("with room again the next sweep left %+v", st)
	}
}

func TestAFailedBackupIsReportedAndWaitsBeforeTheNextTry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A file where the folder should be, so the folder cannot be made.
	if err := os.WriteFile(filepath.Join(dir, backup.AutoDirName), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := backupApp(t, dir, 1, 7)

	st := sweepBackup(t, a)
	if st.Last == nil || st.Last.Error == "" {
		t.Fatalf("last = %+v, want the failure", st.Last)
	}
	if st.NextRunAt == nil || st.NextRunAt.Before(time.Now().Add(autoBackupRetry-time.Minute)) {
		t.Errorf("nextRunAt = %v, want about %s from now", st.NextRunAt, autoBackupRetry)
	}
	if row := a.backupSubsystem(a.Settings.Get()); row.State != StateFailed || row.Detail != st.Last.Error {
		t.Errorf("the health row reads %+v", row)
	}
	failedAt := st.Last.At
	if again := sweepBackup(t, a); !again.Last.At.Equal(failedAt) {
		t.Errorf("the next sweep tried again at once, at %v", again.Last.At)
	}
}

func TestWithAnAppKeyTheAutomaticBackupIsSealed(t *testing.T) {
	t.Parallel()
	a := backupApp(t, t.TempDir(), 1, 7)
	key, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	a.AppKey = key

	st := sweepBackup(t, a)
	if len(st.Files) != 1 || filepath.Ext(st.Files[0].Name) != backup.SealedExt {
		t.Fatalf("with an app key the backup is %v", fileNames(st.Files))
	}
	raw, err := os.ReadFile(filepath.Join(st.Dir, st.Files[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Unseal(nil, raw); err == nil {
		t.Error("the sealed backup opens without the key")
	}
	archive, err := backup.Unseal(key, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(t.TempDir(), archive, buildinfo.Version); err != nil {
		t.Errorf("a restore with the key refuses the backup: %v", err)
	}
}
