package api

// The options route's own half of pinning a task to a backend: that the field
// arrives, and that a backend this instance does not have is refused BEFORE
// any of the other fields in the same request have been applied.

import (
	"encoding/json"
	"net/http"
	"testing"

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

// TestTheOptionsRoutePinsABackend is the wiring: the field on the request
// reaches the task, and it comes back on the list so the row can say which
// backend it is nailed to.
func TestTheOptionsRoutePinsABackend(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	id := stage(t, a, "https://host.example/one.bin")[0].ID

	// "direct" is registered by every App at construction, so this is a
	// backend the instance genuinely has rather than a name that happens to
	// look plausible.
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

// TestAnUnknownBackendIsRefusedBeforeAnythingIsApplied is the order the
// handler is written in, pinned down. One request carries a rename, a comment
// and a pin; if the pin is checked after the rest have landed, the caller gets
// a 400 and a selection that was edited anyway, which is the half-applied edit
// SetTaskOptions' own contract exists to rule out.
func TestAnUnknownBackendIsRefusedBeforeAnythingIsApplied(t *testing.T) {
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
