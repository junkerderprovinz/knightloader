package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// touchedCount reads the count a bulk route answers with.
func touchedCount(t *testing.T, body []byte) int {
	t.Helper()
	var res struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return res.Count
}

// TestAPackageIsPausedAndResumedInOneRequest: a package menu names every link
// in the package, and the answer counts only the links that changed, so the
// interface can say what happened without fetching the list again.
func TestAPackageIsPausedAndResumedInOneRequest(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	sel := ids(stage(t, a, "https://host.example/one.bin", "https://host.example/two.bin"))
	// Held on the way into the queue, so the dispatcher leaves them waiting
	// rather than reaching for a network the test does not have.
	a.SetHold(sel, true)
	a.StartTasks(sel)

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/pause", map[string]any{"ids": sel})
	if code != http.StatusOK {
		t.Fatalf("pausing a package = %d: %s", code, body)
	}
	if n := touchedCount(t, body); n != 2 {
		t.Errorf("the route reports %d links paused, want 2", n)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Status != core.StatusPaused {
			t.Errorf("%s is %q after the pause, want paused", task.ID, task.Status)
		}
	}

	// A second pause finds nothing left to pause and says so.
	if _, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/pause", map[string]any{"ids": sel}); touchedCount(t, body) != 0 {
		t.Errorf("pausing paused links reports %s, want a count of 0", body)
	}

	code, body = postJSON(t, http.MethodPost, srv.URL+"/api/tasks/resume", map[string]any{"ids": sel})
	if code != http.StatusOK {
		t.Fatalf("resuming a package = %d: %s", code, body)
	}
	if n := touchedCount(t, body); n != 2 {
		t.Errorf("the route reports %d links resumed, want 2", n)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Status != core.StatusQueued {
			t.Errorf("%s is %q after the resume, want queued", task.ID, task.Status)
		}
	}

	for _, route := range []string{"/api/tasks/pause", "/api/tasks/resume"} {
		if code, _ := postJSON(t, http.MethodPost, srv.URL+route, map[string]any{"ids": []string{}}); code != http.StatusBadRequest {
			t.Errorf("%s without ids = %d, want 400", route, code)
		}
	}
}

// TestThePackageRenameRouteRefusesWithTheReason: the rename window shows the
// server's sentence when it refuses, so the refusal has to carry one, and it
// must come before anything is changed.
func TestThePackageRenameRouteRefusesWithTheReason(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	sel := ids(stage(t, a, "https://host.example/one.bin", "https://host.example/two.bin"))

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/package/rename",
		map[string]any{"ids": sel, "name": "Season 1/Episode 2"})
	if code != http.StatusBadRequest {
		t.Fatalf("a name with a slash = %d, want 400", code)
	}
	if !strings.Contains(string(body), "/") {
		t.Errorf("the refusal does not say what was wrong with the name: %s", body)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Package != "Batch" {
			t.Errorf("%s moved to package %q despite the refusal", task.ID, task.Package)
		}
	}

	// The window words the refusal itself, from the code and the name.
	var refusal struct {
		Error  string            `json:"error"`
		Code   string            `json:"code"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil {
		t.Fatalf("the refusal is not the coded envelope: %s", body)
	}
	if refusal.Code != "separator" || refusal.Params["name"] != "Season 1/Episode 2" {
		t.Errorf("the refusal carries code %q about %q, want separator about the name", refusal.Code, refusal.Params["name"])
	}
	code, body = postJSON(t, http.MethodPost, srv.URL+"/api/tasks/options",
		map[string]any{"ids": sel[:1], "name": "..."})
	if code != http.StatusBadRequest || !strings.Contains(string(body), `"code":"dots"`) {
		t.Errorf("renaming a link to dots = %d: %s, want 400 with the dots code", code, body)
	}

	code, body = postJSON(t, http.MethodPost, srv.URL+"/api/tasks/package/rename",
		map[string]any{"ids": sel, "name": "Season 1"})
	if code != http.StatusOK {
		t.Fatalf("renaming a package = %d: %s", code, body)
	}
	if n := touchedCount(t, body); n != 2 {
		t.Errorf("the route reports %d links renamed, want 2", n)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Package != "Season 1" {
			t.Errorf("%s is in package %q, want %q", task.ID, task.Package, "Season 1")
		}
	}
}
