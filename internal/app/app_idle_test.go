package app

// End-to-end coverage for app_idle.go's seam into the real App: the state
// machine itself (rising edge, suppression, cancel) is already pinned by
// internal/idleaction's own tests against a fake clock; what only a real App
// can prove is that queueIdleForAction reads the actual task list correctly
// (a disabled link must not block it, a running one must) and that firing
// ActionPause actually halts the real queue through SetHalted.
//
// idleAction polls on a real two-second timer inside App.New (idleaction.
// defaultPoll) rather than an injectable one, the same as every other real
// App integration test in this package accepts real timing for what a fake
// clock cannot stand in for. DelaySeconds is kept at the package's own floor
// (5) throughout, so each of these costs a single-digit number of seconds
// rather than minutes.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// pollUntil polls cond every 100ms for up to timeout, so a test does not sleep
// the full worst case when the answer arrives sooner - the idle poll interval
// (2s) plus the countdown (5s) is already the slow part.
func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

func TestIdleActionPausesTheQueueAfterItsCountdown(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	// Nothing was ever added: the app starts idle, so this should arm within
	// one poll interval and fire once the countdown elapses.
	if !pollUntil(t, 15*time.Second, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("did not arm within the expected window")
	}
	if !pollUntil(t, 10*time.Second, func() bool { return a.Queue().Halted }) {
		t.Fatal("the queue was never halted by the idle action")
	}
}

func TestIdleActionCanBeCancelled(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, 15*time.Second, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("did not arm within the expected window")
	}
	a.CancelIdleAction()
	if a.IdleActionState().Armed {
		t.Fatal("still armed immediately after CancelIdleAction")
	}

	// Waited out past where the original countdown would have fired: a
	// cancelled countdown must not pause the queue on its own schedule.
	time.Sleep(6 * time.Second)
	if a.Queue().Halted {
		t.Error("the queue was halted despite the countdown having been cancelled")
	}
	if a.IdleActionState().Armed {
		t.Error("re-armed on its own after being cancelled, within the same idle stretch")
	}
}

// TestDisabledLinkDoesNotBlockTheIdleAction pins the exact reasoning in
// queueIdleForAction's own doc comment: a link the user has switched off is
// never going to run on its own, so it must not be the one thing standing
// between the box and going quiet.
func TestDisabledLinkDoesNotBlockTheIdleAction(t *testing.T) {
	a := newQueueApp(t)

	created := a.AddLinks([]string{"https://host.example/parked.bin"}, "Batch")
	if len(created) != 1 {
		t.Fatalf("staged %d links, want 1", len(created))
	}
	a.SetEnabled([]string{created[0].ID}, false)

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, 15*time.Second, func() bool { return a.IdleActionState().Idle }) {
		t.Fatal("queueIdleForAction reported busy while the only task in the list is disabled")
	}
	if !pollUntil(t, 10*time.Second, func() bool { return a.Queue().Halted }) {
		t.Fatal("the idle action never fired despite nothing enabled being left to do")
	}
}

// TestRunningTaskBlocksTheIdleAction is the other direction of the same
// check: a transfer actually in flight is exactly the case the feature must
// never fire underneath.
func TestRunningTaskBlocksTheIdleAction(t *testing.T) {
	a := newQueueApp(t)

	running := &core.Task{ID: "r1", URL: "https://host.example/big.bin", Status: core.StatusRunning, Enabled: true}
	a.mu.Lock()
	a.tasks[running.ID] = running
	a.active[running.ID] = true
	a.mu.Unlock()

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	// Long enough to cover a poll and a countdown, both of which must not
	// have happened.
	time.Sleep(9 * time.Second)
	if a.IdleActionState().Idle {
		t.Error("queueIdleForAction reported idle while a task is actively running")
	}
	if a.Queue().Halted {
		t.Error("the idle action fired while a download was in flight")
	}
}

// TestSeedingTorrentDoesNotBlockTheIdleAction pins decision 4 of
// docs/torrent-support.md: a torrent that is only seeding, not downloading
// and not queued, must not be read as work still owed. queueIdleForAction's
// own doc comment explains why no new exclusion was written for this
// (Seeding rides along on Status == core.StatusDone, which Counters already
// treats as not owed) - this test is what actually proves that reasoning
// against a real App and a real idleaction.Controller, rather than leaving it
// as an unverified claim in a comment.
func TestSeedingTorrentDoesNotBlockTheIdleAction(t *testing.T) {
	a := newQueueApp(t)

	seeding := &core.Task{
		ID: "torrent1", URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		Status: core.StatusDone, Enabled: true, Size: 500, Loaded: 500,
		Seeding: true, Peers: 4, Seeds: 2, Ratio: 0.4, Uploaded: 200,
	}
	a.mu.Lock()
	a.tasks[seeding.ID] = seeding
	a.mu.Unlock()

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, 15*time.Second, func() bool { return a.IdleActionState().Idle }) {
		t.Fatal("queueIdleForAction reported busy while the only task left is seeding, not downloading")
	}
	if !pollUntil(t, 10*time.Second, func() bool { return a.Queue().Halted }) {
		t.Fatal("the idle action never fired despite nothing but a seeding torrent remaining")
	}
}

func TestApplySettingsRefreshesIdleActionWithoutWaitingForThePoll(t *testing.T) {
	a := newQueueApp(t)
	// Enabling the action while the queue is already idle (newQueueApp starts
	// with nothing in it) should arm well inside one poll interval - Refresh
	// is the whole point of this test, so the window is a fraction of
	// idleaction.defaultPoll rather than a multiple of it.
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(t, 1*time.Second, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("ApplySettings did not nudge idleAction into arming promptly")
	}
}
