package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestCountersKeepDisabledLinksInTheFileCountOnly is the one rule the figures
// under the list turn on.
//
// A link that is switched off is still a file the user added, so dropping it
// from the count would make the list shorter than the list. What it is not is
// work the queue is going to do: leaving its bytes in the total puts a number
// in front of somebody that no amount of waiting ever works off, and an ETA
// built on it never arrives.
func TestCountersKeepDisabledLinksInTheFileCountOnly(t *testing.T) {
	a := newQueueApp(t)

	put := func(t2 *core.Task) {
		a.mu.Lock()
		a.tasks[t2.ID] = t2
		a.mu.Unlock()
	}
	put(&core.Task{ID: "run", URL: "https://host.example/a", Status: core.StatusRunning,
		Enabled: true, Size: 1000, Loaded: 400, Speed: 100})
	put(&core.Task{ID: "wait", URL: "https://host.example/b", Status: core.StatusQueued,
		Enabled: true, Size: 600})
	put(&core.Task{ID: "off", URL: "https://host.example/c", Status: core.StatusQueued,
		Enabled: false, Size: 9_000_000})
	// Neither of these is owed any more, so neither is a file the list is still
	// counting down.
	put(&core.Task{ID: "done", URL: "https://host.example/d", Status: core.StatusDone,
		Enabled: true, Size: 500, Loaded: 500})
	put(&core.Task{ID: "failed", URL: "https://host.example/e", Status: core.StatusError,
		Enabled: true, Size: 700})
	// Staged but never started: it is not in the queue at all, and counting it
	// would move the ETA every time somebody pasted something.
	put(&core.Task{ID: "staged", URL: "https://host.example/f", Status: core.StatusCollected,
		Enabled: true, Size: 4000})

	c := a.Counters()

	if c.Files != 3 {
		t.Errorf("Files = %d, want 3: the two live links plus the one switched off", c.Files)
	}
	if c.Disabled != 1 {
		t.Errorf("Disabled = %d, want 1", c.Disabled)
	}
	if c.Running != 1 {
		t.Errorf("Running = %d, want 1", c.Running)
	}
	// 600 still to fetch on the running one, 600 on the waiting one, and not one
	// byte of the nine megabytes that are switched off.
	if c.Remaining != 1200 {
		t.Errorf("Remaining = %d, want 1200: the disabled link's bytes leaked into the total", c.Remaining)
	}
	if c.ETA == nil || *c.ETA != 12 {
		t.Errorf("ETA = %v, want 12 seconds at 100 B/s", c.ETA)
	}
}

// TestCountersHaveNoETAWithNothingMoving keeps the honest gap in the figures. A
// stalled queue reporting zero seconds reads as "done in a moment", which is
// the one thing it is not.
func TestCountersHaveNoETAWithNothingMoving(t *testing.T) {
	a := newQueueApp(t)
	a.mu.Lock()
	a.tasks["idle"] = &core.Task{ID: "idle", URL: "https://host.example/a",
		Status: core.StatusPaused, Enabled: true, Size: 1000, Loaded: 250}
	a.mu.Unlock()

	c := a.Counters()
	if c.Remaining != 750 {
		t.Errorf("Remaining = %d, want 750", c.Remaining)
	}
	if c.ETA != nil {
		t.Errorf("ETA = %d with nothing downloading, want no answer at all", *c.ETA)
	}
}

// TestCountersIgnoreASizeNobodyKnowsYet stops a guess entering the total. A
// link whose size has not come back yet would otherwise contribute its whole
// loaded count as "remaining", and the figure would fall as the file grew.
func TestCountersIgnoreASizeNobodyKnowsYet(t *testing.T) {
	a := newQueueApp(t)
	a.mu.Lock()
	a.tasks["unsized"] = &core.Task{ID: "unsized", URL: "https://host.example/a",
		Status: core.StatusRunning, Enabled: true, Loaded: 2048, Speed: 512}
	a.mu.Unlock()

	c := a.Counters()
	if c.Files != 1 {
		t.Errorf("Files = %d, want the unsized download counted as a file", c.Files)
	}
	if c.Remaining != 0 {
		t.Errorf("Remaining = %d, want nothing guessed for a size nobody knows", c.Remaining)
	}
	if c.ETA != nil {
		t.Errorf("ETA = %d, want no answer while the only download has no known size", *c.ETA)
	}
}
