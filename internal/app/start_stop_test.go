package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

// Pause writes "paused" (pause_status_test.go), and a polling backend would
// write "running" back over it a fraction of a second later, so the queue
// answers `running: 0, halted: true` while the rows say running again.
func TestABackendPollCannotResurrectAPausedTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://rapidgator.example/file/x", Name: "part05.rar",
		Status: core.StatusRunning, Enabled: true, Resolver: "jd",
	})
	id := task.ID

	a.mu.Lock()
	a.active[id] = true
	a.mu.Unlock()

	a.Pause(id)

	// What JD's poller sends on its next tick: the link is still in JD's own
	// download list, so it reports it as running.
	a.onUpdate(id, core.Update{Status: core.StatusRunning, Speed: 4 << 20, Loaded: 1024})

	a.mu.Lock()
	got, speed, loaded := a.tasks[id].Status, a.tasks[id].Speed, a.tasks[id].Loaded
	a.mu.Unlock()

	if got != core.StatusPaused {
		t.Fatalf("status after a poll following Pause = %q, want %q", got, core.StatusPaused)
	}
	if speed != 0 {
		t.Errorf("speed on a paused task = %d, want 0", speed)
	}
	// The bytes already written survive; only the claim about what is happening
	// now is refused.
	if loaded != 1024 {
		t.Errorf("loaded = %d, want the reported 1024 to be kept", loaded)
	}
}

// Stop, then play. The hard stop pauses everything in flight and halts the
// queue, so it also has to leave the tasks in the queue: paused outside it, the
// dispatcher has nothing to hand out and no button gives them back.
func TestReleasingTheHaltStartsWhatTheHardStopStopped(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/big.bin", Name: "big.bin",
		Status: core.StatusRunning, Enabled: true,
	})
	id := task.ID
	// A running task is not put in a.queue, because a running task is never in
	// it: dispatchLocked keeps only what it could not hand out and writes that
	// back as the whole queue. Seeding the queue here would build a state the
	// program cannot reach.
	a.mu.Lock()
	a.active[id] = true
	a.mu.Unlock()

	a.StopAll()

	a.mu.Lock()
	status, inQueue := a.tasks[id].Status, false
	for _, q := range a.queue {
		if q == id {
			inQueue = true
		}
	}
	a.mu.Unlock()

	if status != core.StatusQueued {
		t.Errorf("status after the hard stop = %q, want %q; it stopped and is waiting", status, core.StatusQueued)
	}
	if !inQueue {
		t.Fatal("the task left the wait queue, so releasing the halt can never bring it back")
	}
}

// A task somebody paused by hand is a separate instruction, so the master
// switch leaves it alone: otherwise stopping and starting the queue would put
// that row back into download.
func TestAPerTaskPauseSurvivesTheMasterSwitch(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/one.bin", Name: "one.bin",
		Status: core.StatusRunning, Enabled: true,
	})
	id := task.ID
	a.mu.Lock()
	a.active[id] = true
	a.mu.Unlock()

	a.Pause(id)
	a.StopAll()
	a.SetHalted(false)

	a.mu.Lock()
	status, inQueue := a.tasks[id].Status, false
	for _, q := range a.queue {
		if q == id {
			inQueue = true
		}
	}
	a.mu.Unlock()

	if status != core.StatusPaused {
		t.Errorf("status = %q, want %q; a hand pause outlives the master switch", status, core.StatusPaused)
	}
	if inQueue {
		t.Error("a hand-paused task is back in the wait queue")
	}
}

// The exemption to that refusal: a download that finishes between the pause and
// the backend hearing about it would otherwise stay at "paused" for ever.
func TestATerminalUpdateStillLandsOnAPausedTask(t *testing.T) {
	a := newCaptchaTestApp(t)
	for _, tc := range []struct {
		name string
		want core.Status
	}{
		{"done", core.StatusDone},
		{"error", core.StatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := putTask(t, a, core.Task{
				URL: "https://host.example/" + tc.name, Name: tc.name + ".bin",
				Status: core.StatusRunning, Enabled: true,
			})
			id := task.ID
			a.mu.Lock()
			a.active[id] = true
			a.mu.Unlock()

			a.Pause(id)
			a.onUpdate(id, core.Update{Status: tc.want, Err: "whatever the backend said"})

			a.mu.Lock()
			got := a.tasks[id].Status
			a.mu.Unlock()
			if got != tc.want {
				t.Fatalf("status = %q, want the terminal %q to win over the pause", got, tc.want)
			}
		})
	}
}

// Play after stop. The dispatcher returns at its first line while the queue is
// halted, so a start by hand has to lift the halt or nothing runs and nothing
// says why.
func TestStartReleasesAHaltSetByHand(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/a.bin", Name: "a.bin",
		Status: core.StatusCollected, Enabled: true,
	})

	a.SetHalted(true)

	res := a.StartTasksByHand([]string{task.ID})

	if !res.Released {
		t.Error("Released = false, want the manual halt to be reported as lifted")
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	a.mu.Lock()
	halted, manual := a.halted, a.manualHalt
	a.mu.Unlock()
	if halted || manual {
		t.Fatalf("halted=%v manualHalt=%v after a start, want both false", halted, manual)
	}
}

// A task a link-filter rule holds back is not started, and the answer says so
// rather than reading like a successful start.
func TestStartReportsAFilteredTaskInsteadOfSwallowingIt(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/huge.bin", Name: "huge.bin",
		Status: core.StatusCollected, Enabled: true,
		Skipped: true, SkipReason: `ueber dem Limit (link filter rule "zu gross")`,
	})

	res := a.StartTasksByHand([]string{task.ID})

	if res.Started != 0 {
		t.Errorf("Started = %d, want 0; the filter still holds", res.Started)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1 so the answer can say why nothing moved", res.Skipped)
	}
}

// Automation does not lift a halt, which is why the by-hand variant exists.
// StartTasks is what auto-confirm, a watch folder and a forced selection call,
// so a release here would let the next link from the browser extension start a
// queue somebody stopped an hour earlier.
func TestAnAutomaticStartLeavesTheHaltAlone(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/from-the-extension.bin", Name: "b.bin",
		Status: core.StatusCollected, Enabled: true,
	})

	a.SetHalted(true)
	res := a.StartTasks([]string{task.ID})

	if res.Released {
		t.Error("Released = true, want automation to leave the master switch alone")
	}
	a.mu.Lock()
	halted, manual, active := a.halted, a.manualHalt, len(a.active)
	a.mu.Unlock()
	if !halted || !manual {
		t.Fatalf("halted=%v manualHalt=%v, want the halt untouched", halted, manual)
	}
	if active != 0 {
		t.Errorf("%d tasks dispatched while halted", active)
	}
}

// A schedule window is not the user's own switch and is never overridden: the
// tasks queue and wait, and the answer says so.
func TestAScheduledPauseIsReportedNotOverridden(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/c.bin", Name: "c.bin",
		Status: core.StatusCollected, Enabled: true,
	})

	// A window covering every minute of every day, so the test does not depend
	// on what time it runs at.
	cfg := a.Settings.Get()
	cfg.Schedule = []schedule.Entry{{
		Days:   []time.Weekday{0, 1, 2, 3, 4, 5, 6},
		Start:  "00:00",
		End:    "23:59",
		Action: schedule.ActionPause,
	}}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatalf("settings: %v", err)
	}
	a.mu.Lock()
	a.halted, a.manualHalt = true, true
	a.mu.Unlock()

	res := a.StartTasksByHand([]string{task.ID})

	if res.Released {
		t.Error("Released = true, want a schedule window to hold")
	}
	if !res.Blocked {
		t.Error("Blocked = false, want the reason to reach the caller")
	}
	a.mu.Lock()
	halted := a.halted
	a.mu.Unlock()
	if !halted {
		t.Fatal("halted = false, want the window still in force")
	}
}

// Starting nothing does not flip the master switch: pressing start on an empty
// collector says nothing about wanting the queue running again.
func TestStartWithNothingToStartLeavesTheHaltAlone(t *testing.T) {
	a := newCaptchaTestApp(t)
	a.SetHalted(true)

	res := a.StartTasksByHand([]string{"no-such-id"})

	if res.Released {
		t.Error("Released = true, want an empty start to leave the switch alone")
	}
	a.mu.Lock()
	halted := a.halted
	a.mu.Unlock()
	if !halted {
		t.Fatal("halted = false, want the queue to still be halted")
	}
}
