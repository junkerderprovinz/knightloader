package app

// The stall watcher (app_stallwatch.go). Nothing here waits for a real
// standstill: stallPass takes the clock as an argument precisely so a
// twenty-minute silence can be played out in a millisecond, and so the
// assertions are about the rule rather than about how long the test ran.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// stallHost is the one host every task in this file is on. It has a resolver
// and a backend of its own (below), so a restart re-dispatches into a fake
// instead of opening a socket to somebody's server.
const stallHost = "stalled.example"

// hostResolver matches every link on one host. hostcap_test.go's own
// hostCapResolver matches a single fixed URL, which is not enough here: these
// tests need several links on the same host to exercise a per-host limit.
type hostResolver struct {
	id   string
	host string
}

func (r hostResolver) Info() resolver.Info { return resolver.Info{ID: r.id, Prio: 90} }
func (r hostResolver) Match(raw string) bool {
	return strings.HasPrefix(raw, "https://"+r.host+"/")
}
func (hostResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// stallApp wires that resolver to a capBackend (hostcap_test.go), so a restart
// lands somewhere countable and nothing in this file reaches the network.
func stallApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be := &capBackend{got: make(chan int, 8)}
	a.bmu.Lock()
	a.debrid["stalled"] = be
	a.bmu.Unlock()
	a.Registry.Register(hostResolver{id: "stalled", host: stallHost})
	return a, be
}

// runningTask puts one transfer in flight, exactly the way the dispatcher
// would have left it: in a.active, started, and reporting bytes.
func runningTask(a *App, id string, loaded int64) *core.Task {
	task := &core.Task{
		ID: id, URL: "https://" + stallHost + "/" + id + ".bin", Name: id + ".bin",
		Resolver: "stalled", Status: core.StatusRunning, Enabled: true, Loaded: loaded,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.active[id] = true
	a.started[id] = true
	a.mu.Unlock()
	return task
}

// TestAStandingStillTransferIsMarkedWithWhenItStopped is the mark itself, and
// the assertion that matters is the timestamp: the row has to count up from the
// moment the bytes stopped, not from the moment the watcher got round to
// noticing. Marked at the wrong end, a stall found after a night would read as
// "five seconds" every morning.
func TestAStandingStillTransferIsMarkedWithWhenItStopped(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 120 })
	runningTask(a, "t1", 4096)
	base := time.Now()

	a.stallPass(base)
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Fatalf("marked on the very first look, with nothing to compare against yet (%s)", got)
	}
	a.stallPass(base.Add(119 * time.Second))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked after 119s with a 120s timeout (%s)", got)
	}
	a.stallPass(base.Add(121 * time.Second))
	got := liveTask(a, "t1").StalledSince
	if got.IsZero() {
		t.Fatal("a transfer that has moved no bytes for over two minutes is not marked at all")
	}
	if !got.Equal(base) {
		t.Errorf("StalledSince = %s, want %s - the moment the bytes stopped, not the moment it was noticed", got, base)
	}
}

// TestBytesAgainTakeTheMarkOff is what makes the mark safe to trust: it is
// recomputed rather than remembered, so a connection that comes back on its own
// clears itself and nobody is left reading a warning about something that is
// over. Same property core.Waiting has.
func TestBytesAgainTakeTheMarkOff(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 1000)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(90 * time.Second))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	a.mu.Lock()
	a.tasks["t1"].Loaded = 2000
	a.mu.Unlock()
	a.stallPass(base.Add(95 * time.Second))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("still marked at %s after the bytes started moving again", got)
	}
}

// TestACaptchaWaitIsNotAStall is the distinction the whole feature turns on. A
// download sitting on a captcha moves no bytes for as long as it takes a human
// to answer, and it is the healthiest row in the queue - something is expected
// to happen and there is somebody who can make it happen.
//
// The second half is the part that is easy to leave out: the wait must not
// merely be ignored, it must RESET the clock. Ignored only, the ten minutes
// somebody spent away from the keyboard would still be sitting in the watcher's
// record, and the row would be marked the instant they answered.
func TestACaptchaWaitIsNotAStall(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 512)
	a.captchaStateFor().store.Sync([]captcha.Challenge{
		{ID: "c1", Host: stallHost, TaskID: "t1", Kind: captcha.KindImage},
	})

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(10 * time.Minute))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Fatalf("a link waiting for a human was marked as standing still (%s)", got)
	}

	// Answered: the challenge leaves the store, and the ten minutes it took must
	// not count towards the timeout.
	a.captchaStateFor().store.Sync(nil)
	a.stallPass(base.Add(10*time.Minute + 30*time.Second))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked 30s after the captcha was answered (%s) - the wait was counted towards the timeout", got)
	}
}

// TestNothingIsMarkedWhileTheTimeoutIsOff is the promise every install that
// never opens the settings page relies on. Zero is the off switch, not a very
// short timeout.
func TestNothingIsMarkedWhileTheTimeoutIsOff(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 0 })
	runningTask(a, "t1", 77)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(48 * time.Hour))
	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("marked at %s on an install with the feature switched off", got)
	}
}

// TestSwitchingTheWatcherOffTakesBackWhatItWrote. A reading left on a row after
// the thing that writes it has been turned off is the last reading it ever
// took, sitting there for ever with nothing left to update it.
func TestSwitchingTheWatcherOffTakesBackWhatItWrote(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 10)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	cfg := a.Settings.Get()
	cfg.StallTimeout = 0
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	a.stallPass(base.Add(3 * time.Minute))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("the mark survived the feature being switched off (%s)", got)
	}
}

// TestAMarkComesOffATaskThatStoppedRunning covers the paths this file does not
// own. A hand restart (RestartTasks, app_queue.go) takes a task out of the
// running set and puts it back in the queue without knowing this mark exists,
// and a mark nobody takes off then sits on a queued row for ever. The watcher
// walks what it has marked for exactly this reason, rather than only what is
// running.
func TestAMarkComesOffATaskThatStoppedRunning(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	// What a restart from elsewhere in the package leaves behind: out of the
	// running set, back in the queue, mark untouched.
	a.mu.Lock()
	delete(a.active, "t1")
	a.tasks["t1"].Status = core.StatusQueued
	a.mu.Unlock()

	a.stallPass(base.Add(3 * time.Minute))

	if got := liveTask(a, "t1").StalledSince; !got.IsZero() {
		t.Errorf("a queued task still says it has been standing still since %s", got)
	}
}

// TestPausingAStalledTransferAnswersImmediately: the watcher would clean this
// up within a tick anyway, but the row somebody is looking at has to be right
// in the answer to the button they just pressed.
func TestPausingAStalledTransferAnswersImmediately(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))
	if liveTask(a, "t1").StalledSince.IsZero() {
		t.Fatal("not marked, so this test proves nothing about clearing it")
	}

	a.Pause("t1")

	got := liveTask(a, "t1")
	if got.Status != core.StatusPaused {
		t.Fatalf("status = %q, want paused", got.Status)
	}
	if !got.StalledSince.IsZero() {
		t.Errorf("a paused row still claims to be standing still since %s", got.StalledSince)
	}
}

// TestTheMarkAloneRestartsNothing separates the two halves deliberately: seeing
// a stall costs nothing and says only what is true, while restarting throws
// away the bytes the attempt did fetch. One must never imply the other.
func TestTheMarkAloneRestartsNothing(t *testing.T) {
	a, _ := stallApp(t, func(s *settings.Settings) { s.StallTimeout = 60 })
	runningTask(a, "t1", 4096)
	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(5 * time.Minute))

	got := liveTask(a, "t1")
	if got.StalledSince.IsZero() {
		t.Fatal("not marked at all")
	}
	if got.StallRestarts != 0 {
		t.Errorf("restarted %d times with the restart switch off", got.StallRestarts)
	}
	if got.Status != core.StatusRunning {
		t.Errorf("status = %q, want the transfer left exactly where it was", got.Status)
	}
}

// TestAutomaticRestartIsCounted pins the opt-in half AND its ceiling in one
// run, because the two are the same rule: the restart happens, it is counted on
// the task, and once the count reaches the cap it stops happening. Without the
// cap this is a loop against a host that is refusing, spending the queue's slot
// and the previous attempt's bytes on every pass.
func TestAutomaticRestartIsCounted(t *testing.T) {
	a, be := stallApp(t, func(s *settings.Settings) {
		s.StallTimeout = 60
		s.StallRestart = true
		s.StallMaxRestarts = 1
	})
	runningTask(a, "t1", 4096)

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(2 * time.Minute))

	select {
	case <-be.got:
	case <-time.After(2 * time.Second):
		t.Fatal("the backend was never handed the link again")
	}
	after := liveTask(a, "t1")
	if after.StallRestarts != 1 {
		t.Fatalf("StallRestarts = %d, want 1", after.StallRestarts)
	}
	if !after.StalledSince.IsZero() {
		t.Errorf("still marked as standing still after being started again (%s)", after.StalledSince)
	}

	// Stalled a second time, with the cap already reached: marked again, and
	// left alone.
	a.mu.Lock()
	a.tasks["t1"].Status = core.StatusRunning
	a.tasks["t1"].Loaded = 0
	a.mu.Unlock()
	later := base.Add(10 * time.Minute)
	a.stallPass(later)
	a.stallPass(later.Add(2 * time.Minute))

	final := liveTask(a, "t1")
	if final.StalledSince.IsZero() {
		t.Error("the second standstill was not marked; the cap must stop the restart, not the mark")
	}
	if final.StallRestarts != 1 {
		t.Errorf("StallRestarts = %d, want it held at the configured cap of 1", final.StallRestarts)
	}
	select {
	case <-be.got:
		t.Error("restarted a second time past the cap")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestATorrentIsMarkedButNeverRestarted. A torrent that has stopped moving has
// found nobody to move bytes with, and the restart path deletes the partial
// data before asking again - so an automatic restart hands the identical magnet
// to the identical swarm, minus everything it had already fetched.
func TestATorrentIsMarkedButNeverRestarted(t *testing.T) {
	a, be := stallApp(t, func(s *settings.Settings) {
		s.StallTimeout = 60
		s.StallRestart = true
	})
	task := runningTask(a, "t1", 4096)
	a.mu.Lock()
	task.Resolver = "torrent"
	task.InfoHash = "0123456789abcdef0123456789abcdef01234567"
	a.mu.Unlock()

	base := time.Now()
	a.stallPass(base)
	a.stallPass(base.Add(5 * time.Minute))

	got := liveTask(a, "t1")
	if got.StalledSince.IsZero() {
		t.Error("a torrent with no swarm is exactly what somebody needs to see, and it was not marked")
	}
	if got.StallRestarts != 0 {
		t.Errorf("a torrent was restarted %d times, dropping its partial data for nothing", got.StallRestarts)
	}
	select {
	case <-be.got:
		t.Error("the torrent was handed out again")
	case <-time.After(200 * time.Millisecond):
	}
}
