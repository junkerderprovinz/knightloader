package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stubBackend records dispatched downloads.
type stubBackend struct{ got chan string }

func (s *stubBackend) Download(taskID, _ string, _ map[string]string, _ int) { s.got <- taskID }
func (s *stubBackend) Pause(string)                                          {}
func (s *stubBackend) Resume(taskID string)                                  { s.got <- taskID }
func (s *stubBackend) Remove(string, bool)                                   {}

func collect(t *testing.T, ch chan string, n int) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for len(out) < n {
		select {
		case id := <-ch:
			out[id] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %d dispatches, got %v", n, out)
		}
	}
	return out
}

func expectNone(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case id := <-ch:
		t.Fatalf("unexpected dispatch %s", id)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestCollectorStaging pins the JD-style flow: AddLinks stages tasks (collected,
// not dispatched); only StartTasks moves them into the download pipeline.
func TestCollectorStaging(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	stub := &stubBackend{got: make(chan string, 8)}
	a.jd = stub
	a.Registry.Register(jd.Resolver{})
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 4, MaxPerHost: 4, Extract: false}); err != nil {
		t.Fatal(err)
	}

	created := a.AddLinks([]string{"https://h.example/a", "https://h.example/b"}, "grab")
	if len(created) != 2 {
		t.Fatalf("created %d, want 2", len(created))
	}
	for _, c := range a.Tasks() {
		if c.Status != core.StatusCollected {
			t.Fatalf("task %s status = %s, want collected", c.ID, c.Status)
		}
	}
	expectNone(t, stub.got) // nothing dispatched while merely collected

	a.StartTasks([]string{created[0].ID})
	first := collect(t, stub.got, 1)
	if !first[created[0].ID] {
		t.Fatalf("started set = %v, want the one requested id", first)
	}
	expectNone(t, stub.got) // the other stays collected until started
}

// TestDedupOnAdd pins the collector's duplicate guard: the same URL is only
// staged once, whether repeated within one paste or across two.
func TestDedupOnAdd(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Registry.Register(jd.Resolver{})

	first := a.AddLinks([]string{
		"https://h.example/a",
		"https://h.example/a", // duplicate inside the same paste
		"https://h.example/b",
	}, "p")
	if len(first) != 2 {
		t.Fatalf("staged %d, want 2 (in-paste duplicate skipped)", len(first))
	}

	second := a.AddLinks([]string{"https://h.example/a", "https://h.example/c"}, "p")
	if len(second) != 1 || second[0].URL != "https://h.example/c" {
		t.Fatalf("second add = %d task(s) %v, want only the new URL", len(second), second)
	}
	if got := len(a.Tasks()); got != 3 {
		t.Fatalf("total tasks = %d, want 3", got)
	}

	// A settled task must not block a deliberate second attempt at the same URL.
	a.onUpdate(first[0].ID, core.Update{Status: core.StatusError, Err: "boom"})
	again := a.AddLinks([]string{"https://h.example/a"}, "p")
	if len(again) != 1 {
		t.Fatalf("re-adding a failed URL staged %d, want 1", len(again))
	}
}

// TestRestartFailed pins retry: an errored task re-enters the pipeline when
// restarted.
func TestRestartFailed(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	stub := &stubBackend{got: make(chan string, 8)}
	a.jd = stub
	a.Registry.Register(jd.Resolver{})
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 4, MaxPerHost: 4, Extract: false}); err != nil {
		t.Fatal(err)
	}

	created := a.AddLinks([]string{"https://h.example/x"}, "p")
	id := created[0].ID
	a.StartTasks(nil)
	collect(t, stub.got, 1) // dispatched once

	a.onUpdate(id, core.Update{Status: core.StatusError, Err: "boom"})
	a.RestartTasks(nil)
	again := collect(t, stub.got, 1)
	if !again[id] {
		t.Fatalf("restart set = %v, want the errored id re-dispatched", again)
	}
}

// TestScheduler pins the M4 dispatch rules: global and per-host slots, FIFO
// with per-host skip-ahead, slot release on completion, queue-aware pause.
func TestScheduler(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	stub := &stubBackend{got: make(chan string, 16)}
	a.jd = stub
	a.Registry.Register(jd.Resolver{}) // catch-all -> routes to the stub

	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, Extract: false}); err != nil {
		t.Fatal(err)
	}

	created := a.AddLinks([]string{
		"https://hosta.example/one",
		"https://hosta.example/two",
		"https://hostb.example/one",
		"https://hostb.example/two",
	}, "test")
	if len(created) != 4 {
		t.Fatalf("created %d tasks, want 4", len(created))
	}
	byURL := map[string]string{}
	for _, c := range created {
		byURL[c.URL] = c.ID
	}
	// Links stage in the collector; start them to enter the scheduler.
	a.StartTasks(nil)

	// Per-host=1 forces skip-ahead: one task per host starts, not two of hosta.
	first := collect(t, stub.got, 2)
	if !first[byURL["https://hosta.example/one"]] || !first[byURL["https://hostb.example/one"]] {
		t.Fatalf("first wave = %v, want hosta/one + hostb/one (skip-ahead)", first)
	}
	expectNone(t, stub.got)

	// Completing hosta/one frees its host slot: hosta/two starts.
	a.onUpdate(byURL["https://hosta.example/one"], core.Update{Status: core.StatusDone})
	second := collect(t, stub.got, 1)
	if !second[byURL["https://hosta.example/two"]] {
		t.Fatalf("second wave = %v, want hosta/two", second)
	}

	// Pausing the still-queued hostb/two removes it from the queue...
	a.Pause(byURL["https://hostb.example/two"])
	a.onUpdate(byURL["https://hostb.example/one"], core.Update{Status: core.StatusDone})
	expectNone(t, stub.got)

	// ...and Resume re-queues it (dispatch = fresh Download, it never started).
	a.Resume(byURL["https://hostb.example/two"])
	third := collect(t, stub.got, 1)
	if !third[byURL["https://hostb.example/two"]] {
		t.Fatalf("after resume = %v, want hostb/two", third)
	}
}
