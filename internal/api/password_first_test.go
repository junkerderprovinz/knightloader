package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// sendJSON sends method path with body as JSON through c and returns the status,
// the refusal code if there is one, and the body.
func sendJSON(t *testing.T, c *http.Client, method, url string, body any) (int, string, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	var refusal struct{ Code string }
	_ = json.Unmarshal(out, &refusal)
	return resp.StatusCode, refusal.Code, out
}

// TestWithoutAPasswordNoStoredPasswordsGo: an instance without a password
// answers whoever reaches its port. The task list, the twelve words and the
// downloaded files are open to them, but the passwords in a backup wait until
// a password is set.
func TestWithoutAPasswordNoStoredPasswordsGo(t *testing.T) {
	requireContainerDeployment(t)
	srv, a := testServer(t)
	defer srv.Close()
	relayOff(t, a)
	c := http.DefaultClient

	if code, _, body := sendJSON(t, c, http.MethodPost, srv.URL+"/api/connect/activate", nil); code != http.StatusOK {
		t.Fatalf("activate answered %d: %s", code, body)
	}
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/system/backup", nil},
		{http.MethodGet, "/api/system/backups/knightloader-auto-20261001-030000.zip", nil},
	} {
		code, refusal, body := sendJSON(t, c, r.method, srv.URL+r.path, r.body)
		if code != http.StatusForbidden || refusal != "passwordNotSet" {
			t.Errorf("%s %s answered %d %.80s, want 403 passwordNotSet", r.method, r.path, code, body)
		}
	}
	// What gives no stored password away stays open, erasing files included.
	var shown struct{ Phrase string }
	code, _, body := sendJSON(t, c, http.MethodPost, srv.URL+"/api/connect/reveal", nil)
	if err := json.Unmarshal(body, &shown); code != http.StatusOK || err != nil || shown.Phrase == "" {
		t.Errorf("showing the twelve words again answered %d %.80s, want 200 with the phrase", code, body)
	}
	// A cleanup names the preview it confirms.
	_, _, body = sendJSON(t, c, http.MethodGet, srv.URL+"/api/cleanup/offline", nil)
	var preview struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/tasks/delete", map[string]any{"ids": []string{stage(t, a, "https://host.example/one.bin")[0].ID}, "files": true}},
		{http.MethodDelete, "/api/tasks/" + stage(t, a, "https://host.example/two.bin")[0].ID + "?files=1", nil},
		{http.MethodPost, "/api/cleanup/offline", map[string]any{"digest": preview.Digest, "files": true}},
	} {
		if code, _, body := sendJSON(t, c, r.method, srv.URL+r.path, r.body); code >= 300 {
			t.Errorf("without a password, %s %s answered %d %.80s", r.method, r.path, code, body)
		}
	}

	// With a password and a sign-in each of them works.
	const password = "a-good-password"
	if err := a.Auth.SetPassword("", password); err != nil {
		t.Fatal(err)
	}
	signed, _ := signedIn(t, srv.URL, password)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/system/backup", nil},
		{http.MethodPost, "/api/connect/reveal", map[string]string{"password": password}},
	} {
		if code, _, body := sendJSON(t, signed, r.method, srv.URL+r.path, r.body); code >= 300 {
			t.Errorf("signed in, %s %s answered %d %.80s", r.method, r.path, code, body)
		}
	}
}

// TestTheDesktopNeedsNoPasswordForItsOwnWindow: the desktop build opens no
// port, so what reaches its handler is its own window.
func TestTheDesktopNeedsNoPasswordForItsOwnWindow(t *testing.T) {
	prev := buildinfo.Deployment
	buildinfo.Deployment = "desktop"
	t.Cleanup(func() { buildinfo.Deployment = prev })
	srv, a := testServer(t)
	defer srv.Close()

	task := stage(t, a, "https://host.example/one.bin")[0]
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/system/backup", nil},
		{http.MethodPost, "/api/tasks/delete", map[string]any{"ids": []string{task.ID}, "files": true}},
	} {
		if code, _, body := sendJSON(t, http.DefaultClient, r.method, srv.URL+r.path, r.body); code >= 300 {
			t.Errorf("%s %s answered %d %.80s on the desktop", r.method, r.path, code, body)
		}
	}
}
