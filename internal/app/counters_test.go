package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// A disabled link counts as a file but not in the bytes or the ETA, which it
// would keep from ever arriving.
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
	// Settled, so no longer owed.
	put(&core.Task{ID: "done", URL: "https://host.example/d", Status: core.StatusDone,
		Enabled: true, Size: 500, Loaded: 500})
	put(&core.Task{ID: "failed", URL: "https://host.example/e", Status: core.StatusError,
		Enabled: true, Size: 700})
	// Staged but not in the queue.
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
	// 600 on the running one, 600 on the waiting one, nothing of the disabled
	// one.
	if c.Remaining != 1200 {
		t.Errorf("Remaining = %d, want 1200: the disabled link's bytes leaked into the total", c.Remaining)
	}
	if c.ETA == nil || *c.ETA != 12 {
		t.Errorf("ETA = %v, want 12 seconds at 100 B/s", c.ETA)
	}
}

// The captchas waiting ride along with the figures, so an overview of several
// instances counts them without downloading every picture.
func TestCountersCountTheCaptchasWaiting(t *testing.T) {
	a := newQueueApp(t)
	if c := a.Counters(); c.Captchas != 0 {
		t.Errorf("Captchas = %d with nothing waiting, want 0", c.Captchas)
	}
	a.captchaStateFor().store.Sync([]captcha.Challenge{
		{ID: "c1", Host: "host.example", Kind: captcha.KindImage},
		{ID: "c2", Host: "host.example", Kind: captcha.KindWidget},
	})
	if c := a.Counters(); c.Captchas != 2 {
		t.Errorf("Captchas = %d, want the 2 waiting", c.Captchas)
	}
}

// With nothing moving there is no ETA; zero seconds would read as "done in a
// moment".
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

// A download of unknown size adds nothing to the remaining bytes.
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
