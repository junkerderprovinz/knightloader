package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// narrowAndAdmin are a token that may only read and one that may administer,
// on a server with a password.
func narrowAndAdmin(t *testing.T, a *app.App) (read, admin string) {
	t.Helper()
	_, read, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err = a.APITokens.CreateScoped("owner", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	return read, admin
}

func getWithToken(t *testing.T, base, path, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d (%s)", path, resp.StatusCode, b)
	}
	return string(b)
}

// TestANarrowTokenSeesNoCredentialsInLinksThatNameNoFile adds links whose
// address is the task's name, since they name no file, and one of them twice
// so it is folded, and reads them with a read token from the task list, the
// watch list, the folded links and the live stream.
func TestANarrowTokenSeesNoCredentialsInLinksThatNameNoFile(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	s := a.Settings.Get()
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)
	read, admin := narrowAndAdmin(t, a)
	links := []string{"https://host.example/?passkey=NOFILELEAK", "https://user:NOFILEPW@host.example/"}
	leaks := []string{"NOFILELEAK", "NOFILEPW"}
	if created, err := a.AddLinksWithOptions(links, "", app.OriginPaste, app.LinkBatchOptions{}); err != nil || len(created) != 2 {
		t.Fatalf("staging the links: %v, %d tasks", err, len(created))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.URL+"/api/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + read}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	snapshot, _ := json.Marshal(readOneOfType(t, c, "snapshot"))
	if _, err := a.AddLinksWithOptions(links[:1], "", app.OriginPaste, app.LinkBatchOptions{}); err != nil {
		t.Fatal(err)
	}
	skipped, _ := json.Marshal(readOneOfType(t, c, "skipped"))

	paths := []string{"/api/tasks", "/api/tasks/watch", "/api/collector/skipped"}
	for _, path := range paths {
		body := getWithToken(t, srv.URL, path, admin)
		if !strings.Contains(body, leaks[0]) {
			t.Fatalf("GET %s with an admin token = %s, want the links whole, or this test proves nothing", path, body)
		}
	}
	answers := map[string]string{"the stream's snapshot": string(snapshot), "the stream's folded link": string(skipped)}
	for _, path := range paths {
		answers["GET "+path] = getWithToken(t, srv.URL, path, read)
	}
	for what, body := range answers {
		for _, leak := range leaks {
			if strings.Contains(body, leak) {
				t.Errorf("%s gave a read token %q: %s", what, leak, body)
			}
		}
		if !strings.Contains(body, "host.example") {
			t.Errorf("%s lost the host: %s", what, body)
		}
	}
}

// TestANarrowTokenReadsTheHistoryWithoutCredentials files a finished download
// whose link carries a sign-in and a passkey and names no file, and reads the
// history with a read token and with an admin token.
func TestANarrowTokenReadsTheHistoryWithoutCredentials(t *testing.T) {
	t.Parallel()
	srv, a := lockedServer(t)
	read, admin := narrowAndAdmin(t, a)
	for _, task := range []*core.Task{
		{ID: "h1", URL: "https://user:HISTORYPW@host.example/?passkey=HISTORYLEAK", Name: "https://user:HISTORYPW@host.example/?passkey=HISTORYLEAK"},
		{ID: "h2", URL: "https://host.example/film.mkv?token=FILELEAK", Name: "film.mkv", Package: "https://u:PACKAGELEAK@host.example/"},
	} {
		task.Status, task.CreatedAt = core.StatusDone, time.Now()
		if err := a.Store.Save(task); err != nil {
			t.Fatal(err)
		}
	}
	leaks := []string{"HISTORYPW", "HISTORYLEAK", "FILELEAK", "PACKAGELEAK"}
	whole := getWithToken(t, srv.URL, "/api/history", admin)
	for _, leak := range leaks {
		if !strings.Contains(whole, leak) {
			t.Fatalf("the history for an admin token = %s, want %q in it, or this test proves nothing", whole, leak)
		}
	}
	body := getWithToken(t, srv.URL, "/api/history", read)
	for _, leak := range leaks {
		if strings.Contains(body, leak) {
			t.Errorf("the history gave a read token %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "https://host.example/film.mkv") {
		t.Errorf("the redacted history lost what names the file: %s", body)
	}
}
