package keepawake

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// machine is a fake operating system that counts holds and releases.
type machine struct {
	mu       sync.Mutex
	holds    int
	releases int
	refuse   error
}

func (m *machine) hold() (func() error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refuse != nil {
		return nil, m.refuse
	}
	m.holds++
	return func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.releases++
		return nil
	}, nil
}

func (m *machine) counts() (holds, releases int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holds, m.releases
}

// queue is the busy and enabled answers a test flips between passes.
type queue struct {
	busy, enabled atomic.Bool
}

func newGuard(q *queue, m *machine) *Guard {
	return New(Options{Busy: q.busy.Load, Enabled: q.enabled.Load, Hold: m.hold})
}

func TestSleepIsHeldOffOnlyWhileADownloadRuns(t *testing.T) {
	var q queue
	var m machine
	q.enabled.Store(true)
	g := newGuard(&q, &m)

	g.tick()
	if h, _ := m.counts(); h != 0 || g.Held() {
		t.Fatal("the machine is kept awake with nothing running")
	}

	q.busy.Store(true)
	g.tick()
	g.tick()
	if h, r := m.counts(); h != 1 || r != 0 || !g.Held() {
		t.Fatalf("holds, releases = %d, %d while a download runs, want one hold kept", h, r)
	}

	q.busy.Store(false)
	g.tick()
	g.tick()
	if h, r := m.counts(); h != 1 || r != 1 || g.Held() {
		t.Fatalf("holds, releases = %d, %d once the queue is idle, want the hold given back once", h, r)
	}

	q.busy.Store(true)
	g.tick()
	if h, _ := m.counts(); h != 2 {
		t.Errorf("the next download took %d holds in total, want a fresh one", h)
	}
}

func TestSwitchingTheSettingOffLetsTheMachineSleepMidDownload(t *testing.T) {
	var q queue
	var m machine
	q.enabled.Store(true)
	q.busy.Store(true)
	g := newGuard(&q, &m)

	g.tick()
	q.enabled.Store(false)
	g.tick()
	if _, r := m.counts(); r != 1 || g.Held() {
		t.Errorf("the hold is still there after the setting went off")
	}
	g.tick()
	if h, _ := m.counts(); h != 1 {
		t.Errorf("a hold was taken with the setting off")
	}
}

func TestARefusedHoldIsAskedForAgainOnlyInTheNextBusyStretch(t *testing.T) {
	var q queue
	m := machine{refuse: errors.New("no system bus")}
	q.enabled.Store(true)
	q.busy.Store(true)
	g := newGuard(&q, &m)

	asked := 0
	g.hold = func() (func() error, error) {
		asked++
		return m.hold()
	}
	g.tick()
	g.tick()
	g.tick()
	if asked != 1 {
		t.Fatalf("asked %d times during one download, want once", asked)
	}

	m.mu.Lock()
	m.refuse = nil
	m.mu.Unlock()
	q.busy.Store(false)
	g.tick()
	q.busy.Store(true)
	g.tick()
	if asked != 2 || !g.Held() {
		t.Errorf("asked %d times, held %v; the next download should have tried again and got it", asked, g.Held())
	}
}

func TestQuittingMidDownloadGivesTheHoldBack(t *testing.T) {
	var q queue
	var m machine
	q.enabled.Store(true)
	q.busy.Store(true)
	g := New(Options{Busy: q.busy.Load, Enabled: q.enabled.Load, Hold: m.hold, Poll: time.Millisecond})
	g.Start()
	deadline := time.Now().Add(10 * time.Second)
	for !g.Held() {
		if time.Now().After(deadline) {
			t.Fatal("the running guard never took a hold")
		}
		time.Sleep(time.Millisecond)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if h, r := m.counts(); h != 1 || r != 1 {
		t.Errorf("holds, releases = %d, %d after Close, want the one hold given back", h, r)
	}
	if err := g.Close(); err != nil {
		t.Errorf("a second Close failed: %v", err)
	}
}
