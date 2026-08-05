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
func (s *stubBackend) Remove(string)                                         {}

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
