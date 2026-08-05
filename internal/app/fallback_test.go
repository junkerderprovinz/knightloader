package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestFallbackOnlyOnUnsupported is the guard that keeps the chain honest: a
// backend that failed to download must NOT hand the link to the next one,
// because a hoster page fetched as a plain file is a garbage download. Only an
// explicit "this is not mine" advances the chain.
func TestFallbackOnlyOnUnsupported(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// A media page: yt-dlp claims it first, the plain-HTTP fallback sits behind.
	const url = "https://example.com/watch/some-video"
	task := &core.Task{ID: "1", URL: url, Resolver: "ytdlp", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	// A plain failure settles the task where it is.
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "HTTP 403"})
	a.mu.Lock()
	status, res := task.Status, task.Resolver
	a.mu.Unlock()
	if res != "ytdlp" {
		t.Errorf("a failed download changed backend to %q; it must stay put", res)
	}
	if status != core.StatusError {
		t.Errorf("status = %q, want error", status)
	}

	// The same failure marked unsupported hands it on.
	a.mu.Lock()
	task.Status, task.Resolver = core.StatusRunning, "ytdlp"
	a.active[task.ID] = true
	a.mu.Unlock()
	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "yt-dlp: Unsupported URL", Unsupported: true})

	a.mu.Lock()
	res = task.Resolver
	a.mu.Unlock()
	// Which backend it lands on depends on what is installed (yt-dlp may or may
	// not be present here); what must hold is that it moved on.
	if res == "ytdlp" {
		t.Error("an unsupported link stayed on the backend that rejected it")
	}
	if res == "" {
		t.Error("an unsupported link was left without any backend")
	}
}

// TestChainTerminates makes sure the last backend in the chain settles instead
// of looping.
func TestChainTerminates(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	task := &core.Task{ID: "1", URL: "https://example.com/watch/x", Resolver: "http", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "nope", Unsupported: true})
	a.mu.Lock()
	status := task.Status
	a.mu.Unlock()
	if status == core.StatusQueued {
		t.Error("the last backend in the chain re-queued the task instead of settling it")
	}
}
