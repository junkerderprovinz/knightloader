package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/backup"
)

func TestTheAutomaticBackupsAreListedAndDownloadable(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	dir := a.AutoBackupDir()
	made := time.Now().Add(-time.Hour)
	file, err := backup.WriteAuto(dir, made, false, func(w io.Writer, _ string) error {
		_, err := w.Write([]byte("the archive"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.zip"), []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/system/backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st app.AutoBackupState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Dir != dir || len(st.Files) != 1 || st.Files[0].Name != file.Name {
		t.Errorf("GET backups = %+v, want only %s in %s", st, file.Name, dir)
	}
	if st.NextRunAt != nil {
		t.Errorf("nextRunAt = %v while the schedule is off", st.NextRunAt)
	}

	got, err := http.Get(srv.URL + "/api/system/backups/" + file.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if got.StatusCode != http.StatusOK || string(body) != "the archive" {
		t.Errorf("downloading %s = %d %q", file.Name, got.StatusCode, body)
	}
	if cd := got.Header.Get("Content-Disposition"); cd != `attachment; filename="`+file.Name+`"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestOnlyAnAutomaticBackupCanBeDownloadedByName(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	if err := os.MkdirAll(a.AutoBackupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.AutoBackupDir(), "mine.zip"), []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"mine.zip",
		"..%2Fsettings.json",
		"..%2Fknightloader.db",
		backup.AutoName(time.Now(), false),
	} {
		resp, err := http.Get(srv.URL + "/api/system/backups/" + name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET backups/%s = %d, want 404", name, resp.StatusCode)
		}
	}
}

func TestTheDownloadServesTheChosenFolder(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	chosen := filepath.Join(t.TempDir(), "chosen")
	cfg := a.Settings.Get()
	cfg.BackupDir = chosen
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	file, err := backup.WriteAuto(chosen, time.Now(), false, func(w io.Writer, _ string) error {
		_, err := w.Write([]byte("from the chosen folder"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/api/system/backups/" + file.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "from the chosen folder" {
		t.Errorf("downloading from the chosen folder = %d %q", resp.StatusCode, body)
	}
}

func TestARestoreCheckSaysWhetherThePairingComesAlong(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	_, archive := downloadArchive(t, srv.URL)
	check := func(archive []byte) (int, bool) {
		t.Helper()
		status, body := postMultipartFile(t, srv.URL+"/api/system/restore/check", "file", "backup.zip", archive)
		var answer struct {
			Paired bool `json:"paired"`
		}
		_ = json.Unmarshal(body, &answer)
		return status, answer.Paired
	}
	if status, paired := check(archive); status != http.StatusOK || paired {
		t.Errorf("an unpaired instance's backup checks as %d, paired %v", status, paired)
	}

	if err := os.WriteFile(filepath.Join(a.DataDir, "group.json"), []byte(`{"joinedAt":"2026-10-01T03:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, archive = downloadArchive(t, srv.URL)
	if status, paired := check(archive); status != http.StatusOK || !paired {
		t.Errorf("a paired instance's backup checks as %d, paired %v", status, paired)
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, "restore-pending")); !os.IsNotExist(err) {
		t.Errorf("the check staged a restore: %v", err)
	}
}
