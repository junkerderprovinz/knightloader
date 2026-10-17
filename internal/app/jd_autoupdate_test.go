package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJD stands in for the JDownloader backend: a check answers available,
// and both calls are counted.
type fakeJD struct {
	available bool
	checks    atomic.Int32
	restarts  atomic.Int32
}

func (f *fakeJD) wire(a *App) {
	a.checkJD = func(context.Context) (bool, error) {
		f.checks.Add(1)
		return f.available, nil
	}
	a.restartJD = func() error {
		f.restarts.Add(1)
		return nil
	}
}

// jdAutoUpdateApp builds an App with the daily JD update switched on or off.
func jdAutoUpdateApp(t *testing.T, on bool) *App {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	s := a.Settings.Get()
	s.JDAutoUpdate = on
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	return a
}

func setStatus(a *App, id string, s core.Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks[id].Status = s
}

func TestTheDailyJDUpdateInstallsWhenNoJDDownloadRuns(t *testing.T) {
	a := jdAutoUpdateApp(t, true)
	f := &fakeJD{available: true}
	f.wire(a)
	putTask(t, a, core.Task{ID: "done", Resolver: "jd", Status: core.StatusDone, Enabled: true})
	putTask(t, a, core.Task{ID: "engine", Resolver: "http", Status: core.StatusRunning, Enabled: true})

	if held := a.autoUpdateJD(); held {
		t.Error("the update was held back although no JD download runs")
	}
	if f.restarts.Load() != 1 {
		t.Fatalf("JD was asked to install %d times, want once", f.restarts.Load())
	}
}

func TestTheDailyJDUpdateWaitsWhileAJDDownloadRuns(t *testing.T) {
	a := jdAutoUpdateApp(t, true)
	f := &fakeJD{available: true}
	f.wire(a)
	putTask(t, a, core.Task{ID: "jd", Resolver: "jd", Status: core.StatusRunning, Enabled: true})

	if held := a.autoUpdateJD(); !held {
		t.Error("the update was not held back while a JD download runs")
	}
	if f.restarts.Load() != 0 {
		t.Fatal("JD was restarted in the middle of a download")
	}

	setStatus(a, "jd", core.StatusDone)
	a.autoUpdateJD()
	if f.restarts.Load() != 1 {
		t.Fatalf("after the download JD was asked to install %d times, want once", f.restarts.Load())
	}
}

func TestTheDailyJDUpdateDoesNothingWhenSwitchedOff(t *testing.T) {
	a := jdAutoUpdateApp(t, false)
	f := &fakeJD{available: true}
	f.wire(a)

	a.autoUpdateJD()

	if f.checks.Load() != 0 || f.restarts.Load() != 0 {
		t.Fatalf("switched off, the update asked JD %d times and restarted it %d times", f.checks.Load(), f.restarts.Load())
	}
}

func TestTheDailyJDUpdateLeavesJDAloneWithoutAnUpdate(t *testing.T) {
	a := jdAutoUpdateApp(t, true)
	f := &fakeJD{}
	f.wire(a)

	if held := a.autoUpdateJD(); held {
		t.Error("with nothing to install the run asked to come back early")
	}
	if f.checks.Load() != 1 || f.restarts.Load() != 0 {
		t.Fatalf("asked JD %d times and restarted it %d times, want one question and no restart", f.checks.Load(), f.restarts.Load())
	}
}

func TestAHeldJDUpdateIsTriedAgainSoonAndTheLoopStopsOnClose(t *testing.T) {
	a := jdAutoUpdateApp(t, true)
	f := &fakeJD{available: true}
	f.wire(a)
	putTask(t, a, core.Task{ID: "jd", Resolver: "jd", Status: core.StatusRunning, Enabled: true})

	a.spawn(func() { a.jdAutoLoop(time.Millisecond, time.Hour, time.Millisecond) })
	waitFor(t, "a second question while the download runs", func() bool { return f.checks.Load() >= 2 })
	if f.restarts.Load() != 0 {
		t.Fatal("JD was restarted in the middle of a download")
	}
	setStatus(a, "jd", core.StatusDone)
	waitFor(t, "the install once the download is over", func() bool { return f.restarts.Load() == 1 })

	closed := make(chan struct{})
	go func() {
		a.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return while the daily JD update was waiting")
	}
}
