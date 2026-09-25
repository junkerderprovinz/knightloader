package app

// These tests run against a real App with a short poll and a countdown clock
// that only moves when the test moves it; the state machine's own timing is
// covered by internal/idleaction's fake-clock tests.

import (
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// armWindow is how long a test waits for the controller to notice an idle
// queue. Arming takes a poll or two on a healthy machine, but this package
// runs under -race on shared CI runners.
const armWindow = 60 * time.Second

// idleTestPoll is the controller's poll in these tests. idleTestDelay is the
// countdown they configure, the shortest Config accepts.
const (
	idleTestPoll  = 10 * time.Millisecond
	idleTestDelay = 5 * time.Second
)

// pollUntil checks cond every 10ms until it holds or timeout passes.
func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// idleClock stands still until the test moves it, so a countdown runs out
// when the test says and never early on a slow machine.
type idleClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *idleClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *idleClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// newIdleApp is newQueueApp with the idle action polling every idleTestPoll
// on a clock the test moves.
func newIdleApp(t *testing.T) (*App, *idleClock) {
	t.Helper()
	clock := &idleClock{now: time.Now()}
	origClock, origPoll := idleActionClock, idleActionPoll
	idleActionClock, idleActionPoll = clock, idleTestPoll
	t.Cleanup(func() { idleActionClock, idleActionPoll = origClock, origPoll })
	return newQueueApp(t), clock
}

// letItPoll gives the controller many polls, for a check that something did
// not happen.
func letItPoll() { time.Sleep(20 * idleTestPoll) }

func TestIdleActionPausesTheQueueAfterItsCountdown(t *testing.T) {
	a, clock := newIdleApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, armWindow, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("did not arm within the expected window")
	}
	letItPoll()
	if a.Queue().Halted {
		t.Fatal("the queue was halted before the countdown ran out")
	}
	clock.advance(idleTestDelay)
	if !pollUntil(t, armWindow, func() bool { return a.Queue().Halted }) {
		t.Fatal("the queue was never halted by the idle action")
	}
}

func TestIdleActionCanBeCancelled(t *testing.T) {
	a, clock := newIdleApp(t)
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
	clock.advance(idleTestDelay + time.Second)
	letItPoll()
	if a.Queue().Halted {
		t.Error("the queue was halted despite the countdown having been cancelled")
	}
	if a.IdleActionState().Armed {
		t.Error("re-armed on its own after being cancelled, within the same idle stretch")
	}
}

func TestDisabledLinkDoesNotBlockTheIdleAction(t *testing.T) {
	a, clock := newIdleApp(t)

	// Queued rather than staged: Counters leaves collected links out
	// altogether, so a disabled one in the collector would pass this test even
	// if disabled links were counted as work.
	parked := &core.Task{ID: "parked", URL: "https://host.example/parked.bin", Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks[parked.ID] = parked
	a.mu.Unlock()
	a.SetEnabled([]string{parked.ID}, false)

	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		IdleAction: idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, 15*time.Second, func() bool { return a.IdleActionState().Idle }) {
		t.Fatal("queueIdleForAction reported busy while the only task in the list is disabled")
	}
	if !pollUntil(t, armWindow, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("the idle action never armed despite nothing enabled being left to do")
	}
	clock.advance(idleTestDelay)
	if !pollUntil(t, armWindow, func() bool { return a.Queue().Halted }) {
		t.Fatal("the idle action never fired despite nothing enabled being left to do")
	}
}

func TestRunningTaskBlocksTheIdleAction(t *testing.T) {
	a, clock := newIdleApp(t)

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

	// Polls on either side of a whole countdown.
	letItPoll()
	clock.advance(idleTestDelay + time.Second)
	letItPoll()
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
	a, clock := newIdleApp(t)

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
	if !pollUntil(t, armWindow, func() bool { return a.IdleActionState().Armed }) {
		t.Fatal("the idle action never armed despite nothing but a seeding torrent remaining")
	}
	clock.advance(idleTestDelay)
	if !pollUntil(t, armWindow, func() bool { return a.Queue().Halted }) {
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
