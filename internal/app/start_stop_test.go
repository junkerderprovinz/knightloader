package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

// The stop button, all the way down. Pause already wrote "paused"
// (pause_status_test.go) - and a polling backend wrote "running" straight back
// over it a fraction of a second later, which is why the button still looked
// dead on a live instance three fixes in. Measured there: POST /api/queue/stop
// answers `running: 0, halted: true` and the rows say "running" again before
// the answer is on screen.
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

	// Exactly what JD's poller sends on its next 750 ms tick: the link is still
	// in JD's own download list, so it reports it as running.
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
	// The bytes already written are a fact and survive; only the claim about
	// what is happening right now is refused.
	if loaded != 1024 {
		t.Errorf("loaded = %d, want the reported 1024 to be kept", loaded)
	}
}

// The exemption is the other half of the rule, and it has to hold or a download
// that genuinely finishes in the moment between the pause and the backend
// hearing about it would be stuck at "paused" for ever.
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

// Play after stop. StartTasks moved the tasks to "queued" and called a
// dispatcher that returns at its first line while the queue is halted, so
// nothing ran and nothing said why - jdp, four rounds: "es lädt nicht
// herunter".
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

// A task a link-filter rule is holding back is not started - that much is the
// filter working. It used to be reported with a 204 and no body, which is the
// same answer a successful start gave.
func TestStartReportsAFilteredTaskInsteadOfSwallowingIt(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/huge.bin", Name: "huge.bin",
		Status: core.StatusCollected, Enabled: true,
		Skipped: true, SkipReason: `ueber dem Limit (link filter rule "zu gross")`,
	})

	res := a.StartTasksByHand([]string{task.ID})

	if res.Started != 0 {
		t.Errorf("Started = %d, want 0 - the filter still holds", res.Started)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1 so the answer can say why nothing moved", res.Skipped)
	}
}

// Automation does NOT lift a halt, and this is the reason the by-hand variant
// exists at all rather than StartTasks simply releasing.
//
// StartTasks is what auto-confirm, a watch folder and a forced selection call.
// If it released, then stopping the queue would be undone by the next link the
// browser extension sent - somebody stops their downloads, clicks a link on a
// page an hour later, and the whole queue starts again with nothing on screen
// connecting the two. That is a worse bug than the one being fixed, and it
// would have shipped inside the fix.
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

// A schedule window is not the user's own switch and is never overridden - the
// tasks queue and wait, and the answer says so rather than leaving the same
// silence behind a different cause.
func TestAScheduledPauseIsReportedNotOverridden(t *testing.T) {
	a := newCaptchaTestApp(t)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/c.bin", Name: "c.bin",
		Status: core.StatusCollected, Enabled: true,
	})

	// A window covering every minute of every day, so the test never depends on
	// what time it runs at.
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

// Starting nothing must not flip the master switch. Somebody who stops the
// queue and then presses start on an empty collector has said nothing about
// wanting it running again.
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
