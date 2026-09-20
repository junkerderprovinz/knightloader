package app

// These tests run against a real App and the real two-second poll; the state
// machine's own timing is covered by internal/idleaction's fake-clock tests.
// DelaySeconds stays at the floor of 5 to keep them short.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// armWindow is how long a test waits for the controller to notice an idle
// queue. Arming takes under a second on a healthy machine, but this package
// runs under -race on shared CI runners.
const armWindow = 60 * time.Second

// pollUntil checks cond every 100ms until it holds or timeout passes.
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

	if !pollUntil(t, armWindow, func() bool { return a.IdleActionState().Armed }) {
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

	if !pollUntil(t, armWindow, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("did not arm within the expected window")
	}
	a.CancelIdleAction()
	if a.IdleActionState().Armed {
		t.Fatal("still armed immediately after CancelIdleAction")
	}

	// Past the point where the cancelled countdown would have fired.
	time.Sleep(6 * time.Second)
	if a.Queue().Halted {
		t.Error("the queue was halted despite the countdown having been cancelled")
	}
	if a.IdleActionState().Armed {
		t.Error("re-armed on its own after being cancelled, within the same idle stretch")
	}
}

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

	// Long enough for a poll and a countdown.
	time.Sleep(9 * time.Second)
	if a.IdleActionState().Idle {
		t.Error("queueIdleForAction reported idle while a task is actively running")
	}
	if a.Queue().Halted {
		t.Error("the idle action fired while a download was in flight")
	}
}

// TestSeedingTorrentDoesNotBlockTheIdleAction: a torrent that is only seeding
// is not owed work, or one perpetually seeding torrent would disable the
// feature.
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
	// With the poll a minute away, arming at all proves the refresh did it, so
	// the test needs no tight deadline that a loaded runner could miss.
	orig := idleActionPoll
	idleActionPoll = time.Minute
	t.Cleanup(func() { idleActionPoll = orig })

	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(t, 10*time.Second, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("ApplySettings did not refresh idleAction: it never armed, and the poll was a minute away")
	}
}
