package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func backupServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerBackup(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// TestDownloadBackupIsARestorableArchive is the route end to end: what a
// GET produces has to be exactly what internal/backup.Stage will accept,
// carrying the three entries a restore needs and naming a real task that
// was in the store at the time.
func TestDownloadBackupIsARestorableArchive(t *testing.T) {
	a, srv := backupServer(t)
	if err := a.Store.Save(&core.Task{ID: "t1", Name: "file.bin", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET backup = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" || !bytes.Contains([]byte(cd), []byte("attachment")) {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("the response body is not a valid zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"manifest.json", "settings.json", "knightloader.db"} {
		if !names[want] {
			t.Errorf("archive is missing %s", want)
		}
	}

	// The whole point of an unredacted backup: a restored settings.json has
	// to carry the real secret, not the placeholder GET /api/settings would
	// have shown a browser.
	s := a.Settings.Get()
	s.Reconnect.Password = "correct-router-password"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	resp2, err := http.Get(srv.URL + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	raw2, _ := io.ReadAll(resp2.Body)
	zr2, err := zip.NewReader(bytes.NewReader(raw2), int64(len(raw2)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := zr2.Open("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	settingsBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(settingsBytes, []byte("correct-router-password")) {
		t.Error("the backup does not carry the real router password; a restore from it could not put it back")
	}
	if bytes.Contains(settingsBytes, []byte("********")) {
		t.Error("the backup carries a redacted placeholder instead of the real secret")
	}
}

// TestUploadRestoreStagesAValidBackup drives the route the way a settings
// page would: download a backup, then immediately upload it back. It has
// to validate and stage without touching the live store or settings, and
// with RequestExit unset (the default on testApp) it must say a manual
// restart is needed rather than claim one is already under way.
func TestUploadRestoreStagesAValidBackup(t *testing.T) {
	a, srv := backupServer(t)
	if err := a.Store.Save(&core.Task{ID: "t1", Name: "file.bin", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	code, body := postMultipartFile(t, srv.URL+"/api/system/restore", "file", "backup.zip", archive)
	if code != http.StatusAccepted {
		t.Fatalf("POST restore = %d: %s", code, body)
	}
	var result struct {
		Manifest   backup.Manifest `json:"manifest"`
		Restarting bool            `json:"restarting"`
		Status     string          `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decoding the restore response: %v (%s)", err, body)
	}
	if result.Restarting {
		t.Error("restarting=true with RequestExit unset")
	}
	if result.Status == "" {
		t.Error("no status sentence in the restore response")
	}

	// Staged, not applied: the live store this same App still has open must
	// be completely unaffected by an upload that only stages a restore for
	// next boot.
	if _, err := os.Stat(filepath.Join(a.DataDir, "restore-pending")); err != nil {
		t.Errorf("nothing was staged under DataDir: %v", err)
	}
	live, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Errorf("the live store changed shape after a restore upload: %d tasks, want 1", len(live))
	}
}

// TestUploadRestoreTriggersRequestExit is restore composing with the same
// mechanism quit and restart use, once RequestExit is wired: a successful
// upload does not just sit there waiting for somebody to separately press
// restart.
func TestUploadRestoreTriggersRequestExit(t *testing.T) {
	a, srv := backupServer(t)
	resp, err := http.Get(srv.URL + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	var restartCalled bool
	a.RequestExit = func(restart bool) bool { restartCalled = restart; return true }

	code, body := postMultipartFile(t, srv.URL+"/api/system/restore", "file", "backup.zip", archive)
	if code != http.StatusAccepted {
		t.Fatalf("POST restore = %d: %s", code, body)
	}
	if !restartCalled {
		t.Error("RequestExit was not called with restart=true after a validated upload")
	}
	var result struct {
		Restarting bool `json:"restarting"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Restarting {
		t.Error(`response reports restarting=false even though RequestExit accepted the request`)
	}
}

// TestUploadRestoreRejectsGarbage is the route-level half of
// internal/backup's own validation tests: the HTTP layer has to surface the
// specific reason, as plain text, rather than a generic 400.
func TestUploadRestoreRejectsGarbage(t *testing.T) {
	_, srv := backupServer(t)
	code, body := postMultipartFile(t, srv.URL+"/api/system/restore", "file", "backup.zip", []byte("not a zip"))
	if code != http.StatusBadRequest {
		t.Fatalf("POST restore with garbage = %d, want 400", code)
	}
	if len(body) == 0 {
		t.Error("no reason given for the rejected upload")
	}
}

// TestUploadRestoreRequiresTheFileField pins the multipart contract the
// error message promises: a request with no "file" field is a client bug,
// not a 500.
func TestUploadRestoreRequiresTheFileField(t *testing.T) {
	_, srv := backupServer(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("notfile", "irrelevant")
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/system/restore", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST restore with no file field = %d, want 400", resp.StatusCode)
	}
}

// postMultipartFile uploads data as a single-file multipart form, the shape
// routes_containers.go's own upload route already expects and this one
// mirrors.
func postMultipartFile(t *testing.T, url, field, filename string, data []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}
