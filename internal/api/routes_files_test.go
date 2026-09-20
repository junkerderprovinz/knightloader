package api

// The file route as seen from outside: what headers come back for an
// allowlisted type and for everything else, that a locked instance locks this
// route too, and that SafeTaskFile's refusals map to the right status. The
// path containment itself is app_files_test.go's, including the one escape
// shape that cannot be reached from here (see
// TestServeTaskFileSymlinkEscapeIs403).

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// filesServer is this one route on a throwaway app with a real, writable
// download folder, the same shape as foldersServer.
func filesServer(t *testing.T) (*app.App, string, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerFiles(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, base, srv
}

func strp(s string) *string { return &s }

// putFile writes data under name inside base, then stages a task and points it
// there through the same options route the properties panel uses, so these
// tests only ask what an ordinary caller can make the route do.
func putFile(t *testing.T, a *app.App, base, name string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	created := stage(t, a, "https://host.example/"+name)
	id := created[0].ID
	if err := a.SetTaskOptions([]string{id}, app.TaskOptions{Dir: strp(base), Name: strp(name)}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestServeTaskFileInlineAllowlistedType(t *testing.T) {
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "notes.txt", []byte("hello from an nfo-like file"))

	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %q", resp.StatusCode, body)
	}
	if string(body) != "hello from an nfo-like file" {
		t.Errorf("body = %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `inline; filename="notes.txt"` {
		t.Errorf("Content-Disposition = %q, want inline for an allowlisted type", cd)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestServeTaskFileAttachmentForUnlistedType(t *testing.T) {
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "archive.bin", []byte{0x00, 0x01, 0x02})

	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want the fixed fallback rather than anything sniffed or claimed by the request", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="archive.bin"` {
		t.Errorf("Content-Disposition = %q, want attachment for a type off the allowlist", cd)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// TestServeTaskFileNeverSniffsAnHTMLPayload is the case the allowlist exists
// for: a hoster-served file with HTML bytes and an innocuous .txt name goes
// out as text/plain, or opening it inline runs the payload at this app's
// origin with this app's session live in the tab.
func TestServeTaskFileNeverSniffsAnHTMLPayload(t *testing.T) {
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "readme.txt", []byte("<script>document.title='pwned'</script>"))

	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain regardless of what the bytes look like", ct)
	}
}

// TestServeTaskFileContentLengthMatchesTheBytes checks the declared header as
// well as the bytes that arrive: the two can only disagree if something along
// the way guessed rather than measured.
func TestServeTaskFileContentLengthMatchesTheBytes(t *testing.T) {
	a, base, srv := filesServer(t)
	data := []byte("exactly this many bytes and no more")
	id := putFile(t, a, base, "movie.mkv", data)

	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.ContentLength != int64(len(data)) {
		t.Errorf("Content-Length = %d, want %d", resp.ContentLength, len(data))
	}
	if len(body) != len(data) {
		t.Errorf("body carried %d bytes, want %d", len(body), len(data))
	}
}

func TestServeTaskFileUnknownTaskIs404(t *testing.T) {
	_, _, srv := filesServer(t)
	resp, err := http.Get(srv.URL + "/api/tasks/does-not-exist/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServeTaskFileNotYetStartedIs404(t *testing.T) {
	a, _, srv := filesServer(t)
	created := stage(t, a, "https://host.example/still-collected.bin")

	resp, err := http.Get(srv.URL + "/api/tasks/" + created[0].ID + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a task with nothing downloaded yet answered %d, want 404", resp.StatusCode)
	}
}

// TestServeTaskFileSymlinkEscapeIs403 is the one shape of the escape check an
// ordinary HTTP caller can reach. SetTaskOptions cuts a rename to a single
// path segment (rules.FileSegment) before it reaches a task, so a name
// carrying "../" cannot be staged through this route at all; internal/app's
// TestSafeTaskFileNameWithSeparatorIsRefused covers that shape against
// SafeTaskFile directly. A symlink inside the task's own folder makes the same
// point and is something a download folder can end up holding.
func TestServeTaskFileSymlinkEscapeIs403(t *testing.T) {
	a, base, srv := filesServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.bin"), []byte("not for this task"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := stage(t, a, "https://host.example/movie.mkv")
	id := created[0].ID
	if err := a.SetTaskOptions([]string{id}, app.TaskOptions{Dir: strp(base), Name: strp("movie.mkv")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.bin"), filepath.Join(base, "movie.mkv")); err != nil {
		// Windows needs a privilege for this; the rule is the same either way
		// and the platform that ships is the one that can make the link.
		t.Skipf("symlinks are not available here: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %q", resp.StatusCode, body)
	}
}

// TestServeTaskFileRequiresASession: the route is registered with reg.Add, not
// reg.AddOpen, so once a password is set it is as locked as every other route
// under /api/. Verified end to end rather than read off registerFiles.
func TestServeTaskFileRequiresASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, "movie.mkv", []byte("x"))

	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/file")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("locked instance answered %d for the file route, want 401", resp.StatusCode)
	}
}

// TestTaskFileStatusMapping is the refusal-to-status table on its own,
// including the one refusal a stored name with a separator raises, which no
// HTTP caller can produce (see TestServeTaskFileSymlinkEscapeIs403).
func TestTaskFileStatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unknown task", app.ErrTaskFileNotFound, http.StatusNotFound},
		{"nothing on disk yet", app.ErrTaskFileNoBytes, http.StatusNotFound},
		{"not this app's file", app.ErrTaskFileNotLocal, http.StatusBadRequest},
		{"escape", app.ErrTaskFileEscape, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := taskFileStatus(c.err); got != c.want {
				t.Errorf("taskFileStatus(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
