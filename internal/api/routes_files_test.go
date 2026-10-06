package api

// The file route as seen from outside: what headers come back for an
// allowlisted type and for everything else, that a locked instance locks this
// route too, and that SafeTaskFile's refusals map to the right status. The
// path containment itself is app_files_test.go's, including the one escape
// shape that cannot be reached from here (see
// TestServeTaskFileSymlinkEscapeIs403).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unknown task", app.ErrTaskFileNotFound, http.StatusNotFound},
		{"nothing on disk yet", app.ErrTaskFileNoBytes, http.StatusNotFound},
		{"a torrent without media", app.ErrTaskFileNoMedia, http.StatusNotFound},
		{"a stopped download", app.ErrTaskFileIncomplete, http.StatusConflict},
		{"a download being mended", app.ErrTaskFileMending, http.StatusServiceUnavailable},
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

// playLink asks the play route for a link to task id's file with the API
// token secret.
func playLink(t *testing.T, srvURL, id, secret string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/api/tasks/"+id+"/play", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Path string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("play route answered %d: %v", resp.StatusCode, err)
	}
	return body.Path
}

func getStatus(t *testing.T, u string) int {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// A media player the phone app opens sends no cookie and no token. The link
// from the play route has to be enough for that one file, and for nothing
// else.
func TestAPlayLinkOpensItsFileOnALockedInstance(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, "movie.mkv", []byte("frames"))
	other := putFile(t, a, base, "other.mkv", []byte("other frames"))
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, phone, err := a.APITokens.CreateScoped("phone", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	link := playLink(t, srv.URL, id, phone)

	resp, err := http.Get(srv.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "frames" {
		t.Fatalf("play link answered %d with %q, want 200 with the file", resp.StatusCode, body)
	}

	_, ticket, _ := strings.Cut(link, "?")
	changed := link[:len(link)-1] + "A"
	if strings.HasSuffix(link, "A") {
		changed = link[:len(link)-1] + "B"
	}
	cases := map[string]string{
		"another task's file":  "/api/tasks/" + other + "/file?" + ticket,
		"another route":        "/api/tasks/" + id + "/torrent-files?" + ticket,
		"a changed ticket":     changed,
		"the file without one": "/api/tasks/" + id + "/file",
	}
	for name, path := range cases {
		if got := getStatus(t, srv.URL+path); got != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401", name, got)
		}
	}
}

func TestAnExpiredPlayLinkIsRefused(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, "movie.mkv", []byte("frames"))
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	ticket := playTicket(id, "", "", a.Auth.Epoch(), time.Now().Add(-time.Minute))
	if got := getStatus(t, srv.URL+"/api/tasks/"+id+"/file?ticket="+ticket); got != http.StatusUnauthorized {
		t.Fatalf("an expired play link answered %d, want 401", got)
	}
}

// Revoking a lost phone's token, signing out everywhere or changing the
// password has to end the play links handed out before, not leave them open
// for the rest of their twelve hours.
func TestAPlayLinkEndsWithTheCredentialsThatMadeIt(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, "movie.mkv", []byte("frames"))
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	token := func(name string) (string, string) {
		tok, secret, err := a.APITokens.CreateScoped(name, []apitoken.Scope{apitoken.ScopeRead})
		if err != nil {
			t.Fatal(err)
		}
		return tok.ID, secret
	}
	opens := func(link string) bool { return getStatus(t, srv.URL+link) == http.StatusOK }

	lostID, lost := token("lost phone")
	_, kept := token("tablet")
	lostLink, keptLink := playLink(t, srv.URL, id, lost), playLink(t, srv.URL, id, kept)
	if err := a.APITokens.Revoke(lostID); err != nil {
		t.Fatal(err)
	}
	if opens(lostLink) {
		t.Error("a link from a revoked token still opens the file")
	}
	if !opens(keptLink) {
		t.Fatal("revoking one token ended the link of another")
	}

	if err := a.Auth.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	if opens(keptLink) {
		t.Error("a link from before signing out everywhere still opens the file")
	}

	_, again := token("tablet again")
	link := playLink(t, srv.URL, id, again)
	if err := a.Auth.SetPassword("a-good-password", "another-good-one"); err != nil {
		t.Fatal(err)
	}
	if opens(link) {
		t.Error("a link from before the password change still opens the file")
	}
}

// stalledFile is a download whose bytes never arrive.
type stalledFile struct{}

func (stalledFile) Read([]byte) (int, error)       { select {} }
func (stalledFile) Seek(int64, int) (int64, error) { return 0, nil }
func (stalledFile) Close() error                   { return nil }
func (stalledFile) ReadContext(ctx context.Context, _ []byte) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

// lateFile is a download whose bytes arrive after a while.
type lateFile struct {
	*bytes.Reader
	after time.Duration
}

func (f lateFile) Close() error { return nil }
func (f lateFile) ReadContext(ctx context.Context, p []byte) (int, error) {
	select {
	case <-time.After(f.after):
		return f.Read(p)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestAReadOfAStalledDownloadGivesUpAfterItsWait(t *testing.T) {
	t.Parallel()
	r := waitingReader{ctx: context.Background(), f: stalledFile{}, wait: 50 * time.Millisecond}
	start := time.Now()
	n, err := r.Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read = %d, %v; want nothing and a deadline", n, err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("the read gave up before its wait was over")
	}
}

// A range of a file still downloading is answered with its bytes once they
// arrive, rather than with whatever the disk holds there at the time.
func TestARangeOfALateFileWaitsForItsBytes(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdefghij")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := lateFile{Reader: bytes.NewReader(data), after: 30 * time.Millisecond}
		http.ServeContent(w, r, "", time.Time{}, waitingReader{ctx: r.Context(), f: f, wait: time.Second})
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=10-14")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(body) != "abcde" {
		t.Fatalf("range answered %d with %q, want 206 with abcde", resp.StatusCode, body)
	}
}

// A player paused with the stream open stops reading, and the route's write
// blocks. When the request ends, as every request does at shutdown, the write
// gives up.
func TestAWriteToAPausedPlayerGivesUpWhenTheRequestEnds(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, "film.mp4", make([]byte, 64<<20))

	requests, endRequests := context.WithCancel(context.Background())
	defer endRequests()
	returned := make(chan struct{})
	h := Handler(a)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		h.ServeHTTP(w, r)
	}))
	srv.Config.BaseContext = func(net.Listener) context.Context { return requests }
	srv.Start()
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET /api/tasks/%s/file HTTP/1.1\r\nHost: kl\r\n\r\n", id); err != nil {
		t.Fatal(err)
	}
	// Nothing is read, so the socket buffers fill and the write blocks.
	select {
	case <-returned:
		t.Fatal("the route returned before the player read anything")
	case <-time.After(300 * time.Millisecond):
	}
	endRequests()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("the route still writes to a player that stopped reading after its request ended")
	}
}

// core.MediaKind decides which file of a torrent plays and whether the
// interface offers Play, so it has to name exactly what this route serves as
// audio or video.
func TestMediaKindsAreWhatTheRouteServesAsAudioOrVideo(t *testing.T) {
	t.Parallel()
	for ext, ct := range inlineTypes {
		want := ""
		if kind, _, _ := strings.Cut(ct, "/"); kind == "audio" || kind == "video" {
			want = kind
		}
		if got := core.MediaKind("x" + ext); got != want {
			t.Errorf("core.MediaKind(%q) = %q, want %q for %s", ext, got, want, ct)
		}
	}
	for _, name := range []string{"song.FLAC", "film.MKV"} {
		if core.MediaKind(name) == "" {
			t.Errorf("%s is not taken for media", name)
		}
	}
	if got := core.MediaKind("setup.exe"); got != "" {
		t.Errorf("setup.exe is taken for %s", got)
	}
}

// A link for one file of a torrent opens that file and no other, so sharing
// the link to one episode does not open the whole season.
func TestAPlayLinkOpensOnlyTheFileItWasMadeFor(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	ticket := playTicket("t1", "1", "", a.Auth.Epoch(), time.Now().Add(time.Hour))
	opens := func(query string) bool {
		r := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/file?ticket="+ticket+query, nil)
		return playTicketOpens(a, r)
	}
	if !opens("&file=1") {
		t.Fatal("the link does not open its own file")
	}
	for _, query := range []string{"", "&file=0", "&file=2", "&file=01"} {
		if opens(query) {
			t.Errorf("the link for file 1 opens %q", query)
		}
	}
}
