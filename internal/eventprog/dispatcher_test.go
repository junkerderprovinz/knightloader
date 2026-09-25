package eventprog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func healthOf(d *Dispatcher, id string) Health {
	for _, h := range d.Health() {
		if h.ProgramID == id {
			return h
		}
	}
	return Health{}
}

// fakeRow is a runnable row for a fake runner, which never looks at the
// program.
func fakeRow(id string, triggers ...script.Trigger) Program {
	return Program{
		ID: id, Name: "fake " + id, Enabled: true,
		Command:  idleaction.CommandSpec{Program: "fake", Args: []string{"%%name%%"}, TimeoutSeconds: 30},
		Triggers: triggers,
	}
}

// recorder is a Runner that notes the first argument of every run.
type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) run(_ context.Context, _ string, args, _ []string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(args) > 0 {
		r.seen = append(r.seen, args[0])
	}
	return "", nil
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func TestOnDoesNotWaitForTheProgram(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	d := New(Options{Run: func(ctx context.Context, _ string, _, _ []string) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "", nil
	}})
	defer d.Close()
	defer close(release)
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	bus := script.NewBus()
	bus.Subscribe("eventprograms", d.On)
	begin := time.Now()
	bus.Publish(doneFiring("a.mkv"))
	if took := time.Since(begin); took > time.Second {
		t.Errorf("Publish took %s; the publishing download waited for the program", took)
	}
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the program never started")
	}
}

func TestOnlyTheTickedEventsStartTheProgram(t *testing.T) {
	var r recorder
	d := New(Options{Run: r.run})
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskFailed)})

	d.On(doneFiring("finished.mkv"))
	failed := doneFiring("broken.mkv")
	failed.Trigger = script.TriggerTaskFailed
	d.On(failed)

	waitFor(t, "the failure's run", func() bool { return len(r.names()) > 0 })
	time.Sleep(50 * time.Millisecond)
	if got := r.names(); len(got) != 1 || got[0] != "broken.mkv" {
		t.Errorf("ran for %q, want only the failed download", got)
	}
}

func TestOneAtATimeKeepsTheOrderTheEventsHappenedIn(t *testing.T) {
	var r recorder
	d := New(Options{Run: r.run})
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	want := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	for _, n := range want {
		d.On(doneFiring(n))
	}
	waitFor(t, "every run", func() bool { return len(r.names()) == len(want) })
	for i, got := range r.names() {
		if got != want[i] {
			t.Fatalf("ran in the order %v, want %v", r.names(), want)
		}
	}
}

func TestNoMoreRunsAtOnceThanTheRowAllows(t *testing.T) {
	var running, most atomic.Int32
	release := make(chan struct{})
	d := New(Options{Run: func(ctx context.Context, _ string, _, _ []string) (string, error) {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		running.Add(-1)
		return "", nil
	}})
	defer d.Close()
	row := fakeRow("1", script.TriggerTaskDone)
	row.Parallel = 2
	d.Set([]Program{row})

	for range 6 {
		d.On(doneFiring("a.mkv"))
	}
	waitFor(t, "two runs under way", func() bool { return running.Load() == 2 })
	time.Sleep(50 * time.Millisecond)
	if m := most.Load(); m != 2 {
		t.Errorf("%d runs were under way at once, want 2", m)
	}
	close(release)
	waitFor(t, "all six runs", func() bool { return healthOf(d, "1").Runs == 6 })
}

func TestTheModuleSwitchStopsEveryProgram(t *testing.T) {
	var r recorder
	d := New(Options{Run: r.run})
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	d.SetOff(true)
	d.On(doneFiring("while-off.mkv"))
	d.SetOff(false)
	d.On(doneFiring("back-on.mkv"))

	waitFor(t, "the run after switching back on", func() bool { return len(r.names()) > 0 })
	time.Sleep(50 * time.Millisecond)
	if got := r.names(); len(got) != 1 || got[0] != "back-on.mkv" {
		t.Errorf("ran for %q, want only the event after the switch came back on", got)
	}
}

func TestARowThatCannotRunGetsNoWorker(t *testing.T) {
	d := New(Options{Run: (&recorder{}).run})
	defer d.Close()
	off := fakeRow("1", script.TriggerTaskDone)
	off.Enabled = false
	noEvents := fakeRow("2")
	noProgram := fakeRow("3", script.TriggerTaskDone)
	noProgram.Command.Program = ""
	d.Set([]Program{off, noEvents, noProgram})

	d.mu.Lock()
	n := len(d.workers)
	d.mu.Unlock()
	if n != 0 {
		t.Errorf("%d workers for three rows that can never run", n)
	}
}

func TestCloseKillsARunUnderWay(t *testing.T) {
	started := make(chan struct{})
	d := New(Options{Run: func(ctx context.Context, _ string, _, _ []string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}})
	row := fakeRow("1", script.TriggerTaskDone)
	row.Command.TimeoutSeconds = 3600
	d.Set([]Program{row})
	d.On(doneFiring("a.mkv"))
	<-started

	done := make(chan struct{})
	go func() {
		_ = d.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close waited for a run with an hour left on its limit")
	}
}

func TestAFullQueueDropsAndCounts(t *testing.T) {
	release := make(chan struct{})
	d := New(Options{Run: func(ctx context.Context, _ string, _, _ []string) (string, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "", nil
	}})
	defer d.Close()
	defer close(release)
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	captureLog(t)
	for range queueDepth + 20 {
		d.On(doneFiring("a.mkv"))
	}
	if got := healthOf(d, "1").Dropped; got < 19 {
		t.Errorf("Dropped = %d after %d events with room for %d and one running", got, queueDepth+20, queueDepth)
	}
}

// gate is a Runner that holds every run until it is opened, and counts starts.
type gate struct {
	open    chan struct{}
	started atomic.Int32
}

func newGate() *gate { return &gate{open: make(chan struct{})} }

func (g *gate) run(ctx context.Context, _ string, _, _ []string) (string, error) {
	g.started.Add(1)
	select {
	case <-g.open:
	case <-ctx.Done():
	}
	return "", nil
}

func TestSwitchingTheModuleOffDropsWhatIsAlreadyQueued(t *testing.T) {
	g := newGate()
	d := New(Options{Run: g.run})
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	for range 10 {
		d.On(doneFiring("a.mkv"))
	}
	waitFor(t, "the first run", func() bool { return g.started.Load() == 1 })
	d.SetOff(true)
	close(g.open)
	waitFor(t, "the first run to end", func() bool { return !d.Busy() })
	time.Sleep(50 * time.Millisecond)
	if n := g.started.Load(); n != 1 {
		t.Errorf("%d runs started, %d of them after the module was switched off", n, n-1)
	}
}

func TestARunWaitsUntilTheFilesHaveLanded(t *testing.T) {
	var r recorder
	var landed atomic.Bool
	d := New(Options{Run: r.run, Ready: func(script.Firing) bool { return landed.Load() }})
	d.readyPoll = time.Millisecond
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	d.On(doneFiring("film.mkv"))
	time.Sleep(100 * time.Millisecond)
	if got := r.names(); len(got) != 0 {
		t.Fatalf("the program ran for %q while its file was still on its way", got)
	}
	if !d.Busy() {
		t.Error("a run waiting for its file does not count as work under way")
	}
	landed.Store(true)
	waitFor(t, "the run once the file landed", func() bool { return len(r.names()) == 1 })
}

func TestARunStartsAnywayWhenTheFilesNeverLand(t *testing.T) {
	var r recorder
	d := New(Options{Run: r.run, Ready: func(script.Firing) bool { return false }})
	d.readyPoll, d.readyGrace = time.Millisecond, 50*time.Millisecond
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})

	d.On(doneFiring("film.mkv"))
	waitFor(t, "the run after the grace", func() bool { return len(r.names()) == 1 })
}

func TestARemovedRowsLastRunLeavesNoHealthBehind(t *testing.T) {
	g := newGate()
	d := New(Options{Run: g.run})
	defer d.Close()
	row := fakeRow("1", script.TriggerTaskDone)
	d.Set([]Program{row})
	d.On(doneFiring("a.mkv"))
	waitFor(t, "the run", func() bool { return g.started.Load() == 1 })

	d.Set(nil)
	fresh := fakeRow("1", script.TriggerTaskFailed)
	fresh.Name = "a different program"
	d.Set([]Program{fresh})
	close(g.open)
	waitFor(t, "the removed row's run to end", func() bool { return !d.Busy() })

	if h := healthOf(d, "1"); h.Runs != 0 {
		t.Errorf("the new row shows the removed row's run: %+v", h)
	}
}

func TestARunThatCouldNotStartSaysWhy(t *testing.T) {
	d := New(Options{Run: func(context.Context, string, []string, []string) (string, error) {
		return "", errors.New("the reason it did not start")
	}})
	defer d.Close()
	d.Set([]Program{fakeRow("1", script.TriggerTaskDone)})
	captureLog(t)
	d.On(doneFiring("a.mkv"))
	waitFor(t, "the run to be recorded", func() bool { return healthOf(d, "1").Runs == 1 })
	if h := healthOf(d, "1"); h.LastOutput != "the reason it did not start" {
		t.Errorf("LastOutput = %q, want the reason", h.LastOutput)
	}
}
