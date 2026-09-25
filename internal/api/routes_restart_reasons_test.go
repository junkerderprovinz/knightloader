package api

// The retry button, aimed at one cause. See app.RestartTasksIn for why a list
// of failures is several problems rather than one, and routes_tasks.go for the
// field that carries the answer.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// seedFailed brings up an instance whose list holds one errored task per
// reason given, and hands back the ids in that order.
//
// It writes the rows into the store and only then opens the app on that
// directory, because there is no exported way to drive a link to a chosen
// failure: every http link matches resolver.HTTPFallback, so "unsupported" is
// unreachable from a paste, and a spent allowance, a 404 or a full disk need a
// real host to answer. Boot is the app's supported way of reading a task back
// (app.go's reload, and reviveOnBoot leaves a settled row as it found it), so
// a seeded failure is the object the app would have had after a restart.
//
// The queue is halted before the server is attached. Otherwise a restarted
// task reaches a real backend within milliseconds, fails against host.example
// and settles back to "error", and the test would read that second failure as
// proof the retry never happened.
func seedFailed(t *testing.T, reasons ...core.Reason) (*httptest.Server, *app.App, []string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	seeded := make([]string, 0, len(reasons))
	for i, why := range reasons {
		id := fmt.Sprintf("seed%d", i)
		seeded = append(seeded, id)
		if err := st.Save(&core.Task{
			ID:        id,
			URL:       fmt.Sprintf("https://host.example/%d.bin", i),
			Name:      fmt.Sprintf("%d.bin", i),
			Status:    core.StatusError,
			Error:     "seeded failure",
			Reason:    why,
			CreatedAt: time.Now(),
			Enabled:   true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	a.SetHalted(true)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	return srv, a, seeded
}

// statusOf reads one task's status off the live list.
func statusOf(t *testing.T, a *app.App, id string) core.Status {
	t.Helper()
	for _, task := range a.Tasks() {
		if task.ID == id {
			return task.Status
		}
	}
	t.Fatalf("task %s is not in the list", id)
	return ""
}

// TestRestartOnlyTheNamedCause: two dead links and one spent allowance is the
// small version of what the reason list is for. The allowance is worth trying
// again; throwing the dead links at the host once more only proves what the
// host already said.
func TestRestartOnlyTheNamedCause(t *testing.T) {
	t.Parallel()
	srv, a, seeded := seedFailed(t, core.ReasonGone, core.ReasonLimit, core.ReasonGone)

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/restart",
		map[string]any{"reasons": []string{string(core.ReasonLimit)}})
	if code != http.StatusNoContent {
		t.Fatalf("restart by reason = %d: %s", code, body)
	}

	if got := statusOf(t, a, seeded[1]); got != core.StatusQueued {
		t.Errorf("the spent-allowance failure is %q after a retry aimed at exactly that cause, want queued", got)
	}
	for _, id := range []string{seeded[0], seeded[2]} {
		if got := statusOf(t, a, id); got != core.StatusError {
			t.Errorf("dead link %s is %q after a retry aimed at hoster limits; the cause never narrowed anything", id, got)
		}
	}
}

// TestRestartCanAimAtTheUnclassifiedGroup pins the empty reason as a group in
// its own right. core.ReasonUnknown is the empty string, the value a filter
// written the obvious way throws out, and the rows nothing classified are
// usually the largest pile in the list.
func TestRestartCanAimAtTheUnclassifiedGroup(t *testing.T) {
	t.Parallel()
	srv, a, seeded := seedFailed(t, core.ReasonUnknown, core.ReasonGone)

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/restart",
		map[string]any{"reasons": []string{""}})
	if code != http.StatusNoContent {
		t.Fatalf("restart of the unclassified group = %d: %s", code, body)
	}

	if got := statusOf(t, a, seeded[0]); got != core.StatusQueued {
		t.Errorf("the unclassified failure is %q, want queued: the empty reason is a group, not a gap", got)
	}
	if got := statusOf(t, a, seeded[1]); got != core.StatusError {
		t.Errorf("the dead link is %q after a retry aimed at the unclassified group", got)
	}
}

// TestRestartWithNoReasonsStillTakesEverything: an absent reason list means
// every errored task.
func TestRestartWithNoReasonsStillTakesEverything(t *testing.T) {
	t.Parallel()
	srv, a, seeded := seedFailed(t, core.ReasonGone, core.ReasonLimit)

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/restart", map[string]any{})
	if code != http.StatusNoContent {
		t.Fatalf("restart with no reasons = %d: %s", code, body)
	}
	for _, id := range seeded {
		if got := statusOf(t, a, id); got != core.StatusQueued {
			t.Errorf("task %s is %q after a plain retry-everything, want queued", id, got)
		}
	}
}
