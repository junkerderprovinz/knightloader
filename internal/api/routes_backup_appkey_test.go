package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

func newAppKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func downloadArchive(t *testing.T, base string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(base + "/api/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET backup = %d: %s", resp.StatusCode, raw)
	}
	return resp, raw
}

func TestWithAnAppKeyTheBackupIsSealedAndRestoresWithIt(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	a.AppKey = newAppKey(t)
	s := a.Settings.Get()
	s.Reconnect.Password = "correct-router-password"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	resp, archive := downloadArchive(t, srv.URL)
	if bytes.HasPrefix(archive, []byte("PK")) || bytes.Contains(archive, []byte("correct-router-password")) {
		t.Fatal("with an app key the backup is still a readable zip")
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, ".zip.sealed") {
		t.Errorf("Content-Disposition = %q, want a .zip.sealed name", cd)
	}

	code, body := postMultipartFile(t, srv.URL+"/api/system/restore", "file", "backup.zip.sealed", archive)
	if code != http.StatusAccepted {
		t.Fatalf("restoring the sealed backup with its own key = %d: %s", code, body)
	}
}

func TestASealedBackupIsRefusedWithoutItsKey(t *testing.T) {
	t.Parallel()
	a, srv := backupServer(t)
	a.AppKey = newAppKey(t)
	_, archive := downloadArchive(t, srv.URL)

	for _, tc := range []struct {
		name string
		key  *appkey.Key
		code string
	}{
		{"no key", nil, "restore.appKeyMissing"},
		{"another key", newAppKey(t), "restore.appKeyWrong"},
	} {
		b, other := backupServer(t)
		b.AppKey = tc.key
		status, body := postMultipartFile(t, other.URL+"/api/system/restore", "file", "backup.zip.sealed", archive)
		var refusal struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &refusal)
		if status != http.StatusBadRequest || refusal.Code != tc.code {
			t.Errorf("%s: restore = %d %s, want 400 with code %s", tc.name, status, body, tc.code)
		}
		if !strings.Contains(refusal.Error, appkey.EnvVar) {
			t.Errorf("%s: the refusal %q does not say which key is needed", tc.name, refusal.Error)
		}
	}
}

func TestWithoutAnAppKeyTheBackupStaysAZip(t *testing.T) {
	t.Parallel()
	_, srv := backupServer(t)
	resp, archive := downloadArchive(t, srv.URL)
	if !bytes.HasPrefix(archive, []byte("PK")) || resp.Header.Get("Content-Type") != "application/zip" {
		t.Fatal("without an app key the backup is no longer the plain zip it was")
	}
}

func TestAuthStateSaysWhetherAnAppKeyIsSet(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	read := func() map[string]any {
		resp, err := http.Get(srv.URL + "/api/auth")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := read()["appKey"]; got != false {
		t.Fatalf("appKey = %v without a key, want false", got)
	}
	a.AppKey = newAppKey(t)
	if got := read()["appKey"]; got != true {
		t.Fatalf("appKey = %v with a key, want true", got)
	}
}
