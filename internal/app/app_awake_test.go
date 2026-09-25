package app

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func TestTheWorkAfterATransferKeepsTheComputerAwake(t *testing.T) {
	soon := time.Now().Add(time.Hour)
	for _, c := range []struct {
		what string
		task core.Task
		want bool
	}{
		{"a transfer", core.Task{Status: core.StatusRunning, Enabled: true}, true},
		{"an archive being unpacked", core.Task{Status: core.StatusExtracting, Enabled: true}, true},
		{"a retry waiting to start again", core.Task{Status: core.StatusError, Enabled: true, NextTry: soon}, true},
		{"a failure with no retry left", core.Task{Status: core.StatusError, Enabled: true}, false},
		{"a retry on a switched-off link", core.Task{Status: core.StatusError, NextTry: soon}, false},
		{"a paused download", core.Task{Status: core.StatusPaused, Enabled: true}, false},
		{"a finished download", core.Task{Status: core.StatusDone, Enabled: true}, false},
	} {
		a := newQueueApp(t)
		c.task.URL, c.task.Name = "https://host.example/a.bin", "a.bin"
		putTask(t, a, c.task)
		if got := a.Working(); got != c.want {
			t.Errorf("Working() = %v with %s, want %v", got, c.what, c.want)
		}
	}
}

func TestAStoppedQueueLetsTheComputerSleepThroughARetryWait(t *testing.T) {
	a := newQueueApp(t)
	putTask(t, a, core.Task{URL: "https://host.example/a.bin", Name: "a.bin", Status: core.StatusError,
		Enabled: true, NextTry: time.Now().Add(time.Hour)})
	a.SetHalted(true)
	if a.Working() {
		t.Error("a retry that a stopped queue will not start keeps the computer awake")
	}
}

func TestAFileBeingCheckedOrMovedKeepsTheComputerAwake(t *testing.T) {
	a := newQueueApp(t)
	a.delivering.Add(1)
	if !a.Working() {
		t.Error("a finished file on its way out of the working folder does not count")
	}
	a.delivering.Add(-1)
	if a.Working() {
		t.Error("the computer is kept awake after the move ended")
	}
}

func TestARunningEventProgramKeepsTheComputerAwake(t *testing.T) {
	a := newQueueApp(t)
	started, release := make(chan struct{}), make(chan struct{})
	d := a.newEventPrograms(func(ctx context.Context, _ string, _, _ []string) (string, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "", nil
	})
	t.Cleanup(func() { _ = d.Close() })
	a.EventPrograms = d
	a.Events.Subscribe("test", d.On)
	withProgram(t, a, nil)

	tv := script.TaskView{ID: "x", Name: "a.mkv"}
	a.publishEvent(script.Firing{Trigger: script.TriggerTaskDone, Task: &tv})
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the program never started")
	}
	if !a.Working() {
		t.Error("a program working on a finished download does not count")
	}
	close(release)
	deadline := time.Now().Add(30 * time.Second)
	for a.Working() {
		if time.Now().After(deadline) {
			t.Fatal("the computer is kept awake after the program ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
