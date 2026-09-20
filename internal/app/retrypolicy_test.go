package app

// The retry policy: the curve itself, the values that feed it, and the end
// state that is not "failed". A hoster with a one-hour block asked six times
// inside ten minutes and then given up on is worse off than one asked once when
// the block is over.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	slowHost  = "slow.example"  // an hour's block, written into the table
	plainHost = "plain.example" // nothing configured, so the built-in backoff
)

const (
	slowResolverID   = "retry-slow"
	simpleResolverID = "retry-plain"
)

// retryApp gives both hosts a fake resolver and a fake backend, so a failure
// that arms a retry cannot end in a real request to somebody's server.
func retryApp(t *testing.T, mutate func(*settings.Settings)) *App {
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
	a.debrid[slowResolverID] = be
	a.debrid[simpleResolverID] = be
	a.bmu.Unlock()
	a.Registry.Register(hostResolver{id: slowResolverID, host: slowHost})
	a.Registry.Register(hostResolver{id: simpleResolverID, host: plainHost})
	return a
}

func runningOn(a *App, id, host, resolverID string) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + host + "/" + id + ".bin", Name: id + ".bin",
		Resolver: resolverID, Status: core.StatusRunning, Enabled: true,
	}
	a.active[id] = true
	a.started[id] = true
	a.mu.Unlock()
}

// Handed the built-in base and ceiling, retryDelay produces the sequence an
// install that configures nothing relies on.
func TestTheDefaultCurveMatchesTheBuiltInPair(t *testing.T) {
	want := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		60 * time.Second,
		2 * time.Minute,
		4 * time.Minute,
		8 * time.Minute,
		10 * time.Minute, // the ceiling, from here on
		10 * time.Minute,
	}
	for i, w := range want {
		if got := retryDelay(i+1, settings.DefaultRetryDelay, settings.DefaultRetryMax); got != w {
			t.Errorf("retryDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
}

// A configured hour is not cut back to the ten-minute ceiling, which belongs to
// the fifteen-second base.
func TestAnHourMeansAnHour(t *testing.T) {
	s := settings.Defaults()
	s.HostRules = map[string]settings.HostRule{
		slowHost: {Retry: settings.RetryRule{Delay: 3600}},
	}
	plan := s.RetryFor("limit", slowHost)
	for attempt := 1; attempt <= 3; attempt++ {
		if got := retryDelay(attempt, plan.Delay, plan.Max); got != time.Hour {
			t.Errorf("attempt %d waits %s, want the configured hour", attempt, got)
		}
	}
}

// The wiring: until onUpdate reads the host table, it is a settings page that
// does nothing.
func TestAConfiguredHostDelayReachesTheFailedTask(t *testing.T) {
	a := retryApp(t, func(s *settings.Settings) {
		s.HostRules = map[string]settings.HostRule{
			slowHost: {Retry: settings.RetryRule{Delay: 3600}},
		}
	})
	runningOn(a, "slow1", slowHost, slowResolverID)
	runningOn(a, "plain1", plainHost, simpleResolverID)

	before := time.Now()
	a.onUpdate("slow1", core.Update{Status: core.StatusError, Err: "the transfer broke"})
	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})

	slow := liveTask(a, "slow1")
	if slow.Retries != 1 {
		t.Errorf("Retries = %d, want the attempt counted", slow.Retries)
	}
	if d := slow.NextTry.Sub(before); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("next attempt in %s, want about an hour; the host's own rule was not read", d)
	}
	plain := liveTask(a, "plain1")
	if d := plain.NextTry.Sub(before); d > time.Minute {
		t.Errorf("a host with no entry waits %s, want the built-in 15s; one host's rule reached every host", d)
	}
}

// "Failed after three attempts" is mended by allowing more of them, "will not
// be tried again" is not, so a list that shows the two alike sends the reader to
// raise a number that changes nothing.
func TestNeverIsItsOwnEndState(t *testing.T) {
	a := retryApp(t, func(s *settings.Settings) {
		s.HostRules = map[string]settings.HostRule{
			slowHost: {Retry: settings.RetryRule{Never: true}},
		}
	})
	runningOn(a, "slow1", slowHost, slowResolverID)
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("slow1", core.Update{Status: core.StatusError, Err: "the transfer broke"})
	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})

	slow := liveTask(a, "slow1")
	if !slow.GaveUp {
		t.Error("a host marked never settled as an ordinary failure")
	}
	if !slow.NextTry.IsZero() {
		t.Errorf("an automatic retry is pending at %s for a host marked never", slow.NextTry)
	}
	if slow.Retries != 0 {
		t.Errorf("Retries = %d, want an attempt that was never made not to be counted", slow.Retries)
	}
	if plain := liveTask(a, "plain1"); plain.GaveUp {
		t.Error("one host's never reached a host nobody wrote it against")
	}
}

// The other side of that distinction: if every settled failure carried GaveUp,
// the flag would say nothing.
func TestRunningOutOfAttemptsIsNotGivingUp(t *testing.T) {
	a := retryApp(t, func(s *settings.Settings) { s.MaxRetries = 0 })
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})

	got := liveTask(a, "plain1")
	if !got.NextTry.IsZero() {
		t.Errorf("a retry is pending at %s with no attempts allowed", got.NextTry)
	}
	if got.GaveUp {
		t.Error("out of attempts was reported as a deliberate refusal to try again")
	}
}

// Nothing about waiting ten minutes answers a captcha, so the refusal to retry
// is recorded on the task where the row can show it.
func TestACaptchaSettlesAsGivenUp(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "captcha required"})

	got := liveTask(a, "plain1")
	if got.Reason != core.ReasonCaptcha {
		t.Fatalf("reason = %q, want %q; this test cannot reach the branch it is about", got.Reason, core.ReasonCaptcha)
	}
	if !got.GaveUp {
		t.Error("a captcha failure settled as an ordinary retryable failure")
	}
}

// The ceiling is a host rule merged over the per-reason table merged over
// MaxRetries, so only the server can resolve it: reading settings.maxRetries in
// the browser prints the global number over a row the host table gave a
// different one, and the wrong box's number on a peer instance's queue. The
// host rule here says seven against a global three, so a MaxTries of 3 fails.
func TestTheCeilingLeavesTheServer(t *testing.T) {
	a := retryApp(t, func(s *settings.Settings) {
		s.MaxRetries = 3
		s.HostRules = map[string]settings.HostRule{
			slowHost: {Retry: settings.RetryRule{Tries: 7}},
		}
	})
	runningOn(a, "slow1", slowHost, slowResolverID)
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("slow1", core.Update{Status: core.StatusError, Err: "the transfer broke"})
	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})

	slow := liveTask(a, "slow1")
	if slow.Retries != 1 {
		t.Fatalf("Retries = %d, want the attempt counted; this test cannot reach the branch it is about", slow.Retries)
	}
	if slow.MaxTries != 7 {
		t.Errorf("MaxTries = %d, want the host rule's 7: the resolved ceiling never left the dispatcher", slow.MaxTries)
	}
	if plain := liveTask(a, "plain1"); plain.MaxTries != 3 {
		t.Errorf("MaxTries = %d on a host with no entry, want the global 3", plain.MaxTries)
	}
}

// The branch that arms no retry still records the ceiling, because "no retries
// left" is only worth reading beside the count it ran out of.
//
// The task arrives at its ceiling with nothing recorded on it, which is the
// ordinary state after a restart: the spent count is persisted and the ceiling
// is not. Driving two failures through instead would pass with this branch
// deleted, since the first takes the counting branch.
func TestTheLastFailureSaysWhatItRanOutOf(t *testing.T) {
	a := retryApp(t, func(s *settings.Settings) { s.MaxRetries = 2 })
	runningOn(a, "plain1", plainHost, simpleResolverID)
	a.mu.Lock()
	a.tasks["plain1"].Retries = 2
	a.mu.Unlock()

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})

	got := liveTask(a, "plain1")
	if !got.NextTry.IsZero() {
		t.Fatalf("a retry is pending at %s, so this task has not run out of attempts and the test proves nothing", got.NextTry)
	}
	if got.GaveUp {
		t.Fatal("out of attempts settled as a deliberate refusal, which is the other end state")
	}
	if got.MaxTries != 2 {
		t.Errorf("MaxTries = %d, want 2: the exhausted row cannot say what it was out of", got.MaxTries)
	}
	if got.Retries != 2 {
		t.Errorf("Retries = %d, want the two attempts that were allowed and spent", got.Retries)
	}
}

// Retries is reset when a download finishes so a later restart does not begin
// one attempt short of its budget, and the ceiling goes with it rather than
// standing as a denominator over a failure that is over.
func TestAFinishedDownloadKeepsNoCeiling(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "the transfer broke"})
	if got := liveTask(a, "plain1"); got.MaxTries == 0 {
		t.Fatal("the failure recorded no ceiling, so there is nothing here for the finish to clear")
	}
	a.onUpdate("plain1", core.Update{Status: core.StatusDone})

	got := liveTask(a, "plain1")
	if got.MaxTries != 0 {
		t.Errorf("MaxTries = %d on a finished download, want it cleared with Retries", got.MaxTries)
	}
	if got.Retries != 0 {
		t.Errorf("Retries = %d, want the spent attempts cleared", got.Retries)
	}
}

// Every armed retry leaves a time.AfterFunc behind that nothing can cancel, so
// the timer checks its own deadline against the task's. Cutting a wait short,
// which the restart button does, then failing again onto a longer wait would
// otherwise let the first timer restart the download in the middle of the wait
// the row is showing, spending an attempt out of turn.
//
// The second half matters as much: a guard that never fires would pass the
// stale case and switch the automatic retries off altogether.
func TestAnAbandonedRetryTimerIsNotThisTasksRetry(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	abandoned := time.Now().Add(-5 * time.Minute) // what the old timer was armed for
	current := time.Now().Add(10 * time.Minute)   // what the row is counting down to
	a.mu.Lock()
	a.tasks["p1"] = &core.Task{
		ID: "p1", URL: "https://" + plainHost + "/p1.bin", Name: "p1.bin",
		Resolver: simpleResolverID, Status: core.StatusError, Enabled: true,
		Retries: 2, MaxTries: 3, NextTry: current,
	}
	a.mu.Unlock()

	started := func() bool { return liveTask(a, "p1").Status != core.StatusError }

	// Polled rather than read once: the timer runs on its own goroutine, and a
	// restart that has not happened yet looks exactly like one that never will.
	a.retryAfter("p1", 0, abandoned)
	if pollUntil(t, time.Second, started) {
		t.Error("a timer armed for a deadline the task no longer carries restarted it anyway")
	}
	if got := liveTask(a, "p1"); !got.NextTry.Equal(current) {
		t.Errorf("NextTry = %v, want the deadline the row is showing left alone", got.NextTry)
	}

	a.retryAfter("p1", 0, current)
	if !pollUntil(t, 5*time.Second, started) {
		t.Error("the pending retry never ran, so the guard has switched automatic retries off")
	}
}

// The flag is raised where a failure settles and cleared by any dispatch pass
// that meets the task in the wait queue, the one point every path back into the
// queue goes through, RestartTasks included.
func TestGivingUpIsTakenBackWhenTheTaskIsQueuedAgain(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	a.mu.Lock()
	a.tasks["p1"] = &core.Task{
		ID: "p1", URL: "https://" + plainHost + "/p1.bin", Name: "p1.bin",
		Resolver: simpleResolverID, Status: core.StatusError, Enabled: true, GaveUp: true,
	}
	a.mu.Unlock()

	a.RestartTasks([]string{"p1"})

	if liveTask(a, "p1").GaveUp {
		t.Error("a task somebody restarted by hand still says it will not be tried again")
	}
}
