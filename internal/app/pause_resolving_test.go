package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// A pause that reaches the engine while the link is still resolving holds:
// once the host answers, no transfer starts behind the paused row.
func TestAPauseDuringTheResolveStopsTheTransfer(t *testing.T) {
	gate := make(chan struct{})
	hit := make(chan struct{}, 1)
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		select {
		case hit <- struct{}{}:
		default:
		}
		<-gate
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20)))
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)

	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: srv.URL + "/f.bin", Name: "f.bin",
		Status: core.StatusQueued, Enabled: true,
	})
	a.mu.Lock()
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()

	select {
	case <-hit:
	case <-time.After(10 * time.Second):
		t.Fatal("the dispatched task never asked the host for its link")
	}
	if got := a.PauseTasks([]string{task.ID}); len(got) != 1 {
		t.Fatalf("PauseTasks paused %v, want the resolving task", got)
	}
	open()

	time.Sleep(2 * time.Second)
	mu.Lock()
	n := requests
	mu.Unlock()
	if n > 1 {
		t.Errorf("the host was asked %d times; the paused task started its transfer", n)
	}
	if a.Engine.Reconnectable(task.ID) {
		t.Error("the engine runs a transfer for the paused task")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if got := a.tasks[task.ID].Status; got != core.StatusPaused {
		t.Errorf("status = %q, want %q", got, core.StatusPaused)
	}
}
