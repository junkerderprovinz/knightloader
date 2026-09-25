package api

// The options route's half of pinning a task to a backend: the field arrives,
// and a backend this instance does not have is refused before any other field
// in the same request has been applied.

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// tasksNow reads the list back through the route the interface uses, so the
// assertions are about what a caller can actually observe.
func tasksNow(t *testing.T, base string) []core.Task {
	t.Helper()
	resp, err := http.Get(base + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []core.Task
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTheOptionsRoutePinsABackend: the field on the request reaches the task
// and comes back on the list, so the row can say which backend it is pinned
// to.
func TestTheOptionsRoutePinsABackend(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	id := stage(t, a, "https://host.example/one.bin")[0].ID

	// "direct" is registered by every App at construction, so this is a
	// backend the instance has rather than a plausible-looking name.
	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/options",
		map[string]any{"ids": []string{id}, "resolver": "direct"})
	if code != http.StatusNoContent {
		t.Fatalf("POST /api/tasks/options = %d (%s), want 204", code, body)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.ID == id && task.ResolverPin != "direct" {
			t.Fatalf("ResolverPin = %q, want %q", task.ResolverPin, "direct")
		}
	}

	// And an empty string takes it off again, which is the only way back to
	// the automatic choice.
	code, body = postJSON(t, http.MethodPost, srv.URL+"/api/tasks/options",
		map[string]any{"ids": []string{id}, "resolver": ""})
	if code != http.StatusNoContent {
		t.Fatalf("clearing the pin answered %d (%s), want 204", code, body)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.ID == id && task.ResolverPin != "" {
			t.Fatalf("ResolverPin = %q after clearing it, want empty", task.ResolverPin)
		}
	}
}

// The properties panel asks which backends a selection can be pinned to. The
// route sits under /api/tasks/ so a peer's rows are answered by the peer.
func TestTheBackendsRouteListsWhatASelectionCanBePinnedTo(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	id := stage(t, a, "https://host.example/three.bin")[0].ID

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/backends", map[string]any{"ids": []string{id}})
	if code != http.StatusOK {
		t.Fatalf("POST /api/tasks/backends = %d (%s), want 200", code, body)
	}
	var got []app.PinChoice
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("answer %s: %v", body, err)
	}
	if !slices.ContainsFunc(got, func(c app.PinChoice) bool { return c.ID == "direct" }) {
		t.Errorf("choices %v, want the direct download among them", got)
	}
	if slices.ContainsFunc(got, func(c app.PinChoice) bool { return c.ID == "http" }) {
		t.Errorf("choices %v offer the HTTP fallback, which only saves a page", got)
	}

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/backends", map[string]any{"ids": []string{}}); code != http.StatusBadRequest {
		t.Errorf("POST without ids = %d, want 400", code)
	}
}

// TestAnUnknownBackendIsRefusedBeforeAnythingIsApplied pins the order the
// handler is written in. One request carries a rename, a comment and a pin; if
// the pin were checked after the rest had landed, the caller would get a 400
// and a selection that was edited anyway.
func TestAnUnknownBackendIsRefusedBeforeAnythingIsApplied(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	id := stage(t, a, "https://host.example/two.bin")[0].ID

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/options", map[string]any{
		"ids":      []string{id},
		"comment":  "this must not land",
		"resolver": "definitely-not-a-backend",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("POST with an unknown backend = %d, want 400", code)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.ID != id {
			continue
		}
		if task.Comment != "" {
			t.Errorf("Comment = %q after a refused request, want the whole request rejected", task.Comment)
		}
		if task.ResolverPin != "" {
			t.Errorf("ResolverPin = %q after a refused request, want it untouched", task.ResolverPin)
		}
	}
}
