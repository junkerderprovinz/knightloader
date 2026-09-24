package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// postJSON sends a JSON body and returns the status and the raw response.
func postJSON(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// stage puts links in the collector through the app, so the test is about the
// routes and not about the paste box.
func stage(t *testing.T, a *app.App, urls ...string) []*core.Task {
	t.Helper()
	created := a.AddLinks(urls, "Batch")
	if len(created) != len(urls) {
		t.Fatalf("staged %d of %d links", len(created), len(urls))
	}
	return created
}

func ids(tasks []*core.Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

// TestStagedLinksAreEnabled guards the staging path: a Task built from a
// literal that forgets the field arrives switched off, and nothing in the
// interface would explain why it never starts.
func TestStagedLinksAreEnabled(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	for _, task := range stage(t, a, "https://host.example/one.bin") {
		if !task.Enabled {
			t.Fatal("a freshly staged link is disabled")
		}
	}
}

// TestBulkEnableAndHold: a selection acts as one. Both flags exist so a link
// can be parked without being confused with a paused download, and the route
// answers with what it touched so the interface need not re-fetch the list.
func TestBulkEnableAndHold(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a, "https://host.example/one.bin", "https://host.example/two.bin")
	sel := ids(created)

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/enabled",
		map[string]any{"ids": sel, "enabled": false})
	if code != http.StatusOK {
		t.Fatalf("disabling a selection = %d: %s", code, body)
	}
	var res struct {
		Ids   []string `json:"ids"`
		Count int      `json:"count"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatal(err)
	}
	if res.Count != len(sel) {
		t.Errorf("the route reports %d tasks touched, want %d", res.Count, len(sel))
	}
	for _, task := range a.Tasks() {
		if task.Enabled {
			t.Errorf("task %s is still enabled", task.ID)
		}
	}

	if code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/hold",
		map[string]any{"ids": sel[:1], "hold": true}); code != http.StatusOK {
		t.Fatalf("holding a selection = %d: %s", code, body)
	}
	held := 0
	for _, task := range a.Tasks() {
		if task.Hold {
			held++
		}
	}
	if held != 1 {
		t.Errorf("%d tasks are held, want exactly the one that was named", held)
	}
}

// TestBulkDeleteIsOneRequest: clearing a list through the per-task route is a
// request, a store write and a broadcast per row, which on a real list is slow
// enough to look like a hang.
func TestBulkDeleteIsOneRequest(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a,
		"https://host.example/one.bin",
		"https://host.example/two.bin",
		"https://host.example/three.bin")

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/delete",
		map[string]any{"ids": ids(created)})
	if code != http.StatusOK {
		t.Fatalf("bulk delete = %d: %s", code, body)
	}
	if left := len(a.Tasks()); left != 0 {
		t.Errorf("%d tasks left after deleting all of them", left)
	}
}

// TestCleanupPreviewDoesNotRemove: the classes select more than people picture,
// so the confirmation dialog has to be able to say what it is about to do, and
// a preview that removed anything would be a trap.
func TestCleanupPreviewDoesNotRemove(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a, "https://host.example/one.bin", "https://host.example/two.bin")
	a.SetEnabled(ids(created)[:1], false)

	code, body := postJSON(t, http.MethodGet, srv.URL+"/api/cleanup/disabled", nil)
	if code != http.StatusOK {
		t.Fatalf("cleanup preview = %d: %s", code, body)
	}
	var preview struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 {
		t.Errorf("the preview names %d tasks, want the one that is switched off", preview.Count)
	}
	if left := len(a.Tasks()); left != 2 {
		t.Fatalf("the preview removed something: %d tasks left of 2", left)
	}

	if code, body := postJSON(t, http.MethodPost, srv.URL+"/api/cleanup/disabled", nil); code != http.StatusOK {
		t.Fatalf("cleanup = %d: %s", code, body)
	}
	if left := len(a.Tasks()); left != 1 {
		t.Errorf("%d tasks left after cleaning up the disabled one, want 1", left)
	}
}

// TestUnknownCleanupClassSaysWhichExist: the menu is generated from the same
// list, so only an old build talking to a new one reaches this, which is when
// naming the classes that exist is worth the two lines.
func TestUnknownCleanupClassSaysWhichExist(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/cleanup/whatever", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown cleanup class = %d, want 400", code)
	}
	if !strings.Contains(string(body), string(app.CleanupFinished)) {
		t.Errorf("the refusal does not say which classes exist: %s", body)
	}
}

// TestUIStateSurvivesAReload: column widths and a collapse tree come back
// after a reload without a settings field per column.
func TestUIStateSurvivesAReload(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	// A fresh browser must get a usable empty document, not a 404 to handle.
	resp, err := http.Get(srv.URL + "/api/uistate")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(first)) != "{}" {
		t.Fatalf("first load = %d %q, want 200 and an empty document", resp.StatusCode, first)
	}

	const layout = `{"downloads":{"columns":{"name":320}},"collapsed":["pkg-1"]}`
	code, body := postJSON(t, http.MethodPut, srv.URL+"/api/uistate", json.RawMessage(layout))
	if code != http.StatusNoContent {
		t.Fatalf("storing interface state = %d: %s", code, body)
	}
	resp, err = http.Get(srv.URL + "/api/uistate")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(got)) != layout {
		t.Errorf("interface state came back as %s, want %s", got, layout)
	}

	// A second bucket is a second layout, or two clients that both want their own
	// column widths overwrite each other on every reload.
	if code, body := postJSON(t, http.MethodPut, srv.URL+"/api/uistate?key=phone",
		json.RawMessage(`{"downloads":{"columns":{"name":120}}}`)); code != http.StatusNoContent {
		t.Fatalf("storing a second bucket = %d: %s", code, body)
	}
	resp, err = http.Get(srv.URL + "/api/uistate")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(got)) != layout {
		t.Errorf("the default bucket was overwritten by another client: %s", got)
	}
}

// TestUIStateRefusesWhatItCannotHandBack keeps a client from storing something
// it will fail to parse on the next load, with nothing to say when it broke.
func TestUIStateRefusesWhatItCannotHandBack(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	for _, body := range []string{"not json at all", "[1,2,3]", "42"} {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/uistate", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("storing %q = %d, want 400", body, resp.StatusCode)
		}
	}
}

// uploadContainer posts a file the way the interface does.
func uploadContainer(t *testing.T, url, name string, data []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	resp, err := http.Post(url+"/api/containers", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// TestTextContainerIsStagedDirectly: a links.txt needs no key and is staged
// like a paste.
func TestTextContainerIsStagedDirectly(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	code, body := uploadContainer(t, srv.URL, "links.txt",
		[]byte("https://host.example/one.bin\nhttps://host.example/two.bin\n"))
	if code != http.StatusOK {
		t.Fatalf("uploading a link list = %d: %s", code, body)
	}
	if got := len(a.Tasks()); got != 2 {
		t.Errorf("%d tasks staged from the list, want 2", got)
	}
}

// TestEncryptedContainerRefusesWithTheReason: a .dlc needs a key issued to
// registered clients, so with no JD backend configured the answer has to name
// that. "Unsupported file" would send somebody looking for a corrupt download
// that is not corrupt.
func TestEncryptedContainerRefusesWithTheReason(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	// Structurally a DLC: base64 end to end, long enough that the last 88
	// characters decode as a key block. Nothing here could open a container,
	// but it has to pass the structural check, which is what tells a truncated
	// download from a file that needs a key.
	dlc := []byte(strings.Repeat("QUJD", 122))
	code, body := uploadContainer(t, srv.URL, "release.dlc", dlc)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("an encrypted container with no backend = %d, want 503: %s", code, body)
	}
	if !strings.Contains(string(body), "JDownloader") {
		t.Errorf("the refusal does not name what is missing: %s", body)
	}
}

// TestBrokenContainerSaysWhatIsWrongWithIt keeps the container package's own
// wording: a truncated download and an HTML error page saved under a .dlc name
// are both routine, and each has a different fix.
func TestBrokenContainerSaysWhatIsWrongWithIt(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, body := uploadContainer(t, srv.URL, "release.dlc", []byte("<html>404 not found</html>"))
	if code != http.StatusBadRequest {
		t.Fatalf("a damaged container = %d, want 400: %s", code, body)
	}
	if strings.TrimSpace(string(body)) == "" {
		t.Error("the refusal says nothing at all")
	}
}

// TestRemovingALinksShownRowsOneByOneLeavesNothingBehind: the row's own trash
// button removes one row at a time. Once a yt-dlp link's last shown row is
// gone, the rows its preset set aside must go too, or ticking their kind later
// would bring them back for a link that was removed.
func TestRemovingALinksShownRowsOneByOneLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range ytdlp.Variants() {
		if err := st.Save(&core.Task{
			ID: string(v), URL: "https://youtube.com/watch?v=trash000001", Name: "Some Video",
			Host: "youtube.com", Resolver: "ytdlp", Variant: string(v),
			Status: core.StatusCollected, Enabled: true, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(Handler(a))
	defer srv.Close()
	preset := ytdlp.DefaultHosterPreset()
	preset.Variants = []ytdlp.Variant{ytdlp.VariantVideo, ytdlp.VariantAudio}
	if err := a.SetHosterPreset("youtube.com", preset); err != nil {
		t.Fatal(err)
	}

	for _, v := range preset.Variants {
		if code, body := postJSON(t, http.MethodDelete, srv.URL+"/api/tasks/"+string(v), nil); code != http.StatusNoContent {
			t.Fatalf("DELETE the %s row = %d: %s", v, code, body)
		}
	}

	for _, left := range a.Tasks() {
		t.Errorf("the %s row is still listed for a link whose shown rows were both removed", left.Variant)
	}
}
