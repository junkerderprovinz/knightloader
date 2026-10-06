package app

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// orderBackend records the order in which the app hands it starts, pauses and
// removals. A start takes setup before the backend knows of it, as one in the
// engine does until it is registered, and a pause or removal that comes in
// between cannot reach it.
type orderBackend struct {
	setup      time.Duration
	mu         sync.Mutex
	started    []string
	removed    map[string]bool
	paused     map[string]bool
	afterGone  []string
	afterPause []string
}

func newOrderBackend() *orderBackend {
	return &orderBackend{removed: map[string]bool{}, paused: map[string]bool{}}
}

func (b *orderBackend) Download(id string, _ string, _ map[string]string, _ int) {
	time.Sleep(b.setup)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = append(b.started, id)
	if b.removed[id] {
		b.afterGone = append(b.afterGone, id)
	}
	if b.paused[id] {
		b.afterPause = append(b.afterPause, id)
	}
}

func (b *orderBackend) Pause(id string) {
	b.mu.Lock()
	b.paused[id] = true
	b.mu.Unlock()
}

func (b *orderBackend) Resume(string) {}

func (b *orderBackend) Remove(id string, _ bool) {
	b.mu.Lock()
	b.removed[id] = true
	b.mu.Unlock()
}

func (b *orderBackend) startedIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.started)
}

// startsFor gives an app that hands links of elsewhere.example to be, up to
// slots at once.
func startsFor(t *testing.T, be backend, slots int) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		noUnpacking(s)
		s.MaxConcurrent = slots
		s.MaxPerHost = slots
	})
	withBackend(a, "elsewhere", be)
	a.Registry.Register(elsewhereResolver{})
	return a
}

func queueElsewhere(a *App, id string) {
	queueTask(a, &core.Task{ID: id, URL: "https://elsewhere.example/" + id, Name: id,
		Status: core.StatusQueued, Enabled: true, CreatedAt: time.Now()})
}

func waitForStarts(t *testing.T, a *App) {
	t.Helper()
	waitFor(t, "the starts under way", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.handing) == 0
	})
}

func TestARowRemovedRightAfterItsStartIsNotStartedAfterTheRemoval(t *testing.T) {
	be := newOrderBackend()
	be.setup = 20 * time.Millisecond
	a := startsFor(t, be, 1000)

	for i := range 20 {
		id := fmt.Sprintf("r%03d", i)
		queueElsewhere(a, id)
		a.Remove(id, true)
	}
	waitForStarts(t, a)

	be.mu.Lock()
	defer be.mu.Unlock()
	if len(be.afterGone) > 0 {
		t.Errorf("%d of 20 rows reached the backend after their removal", len(be.afterGone))
	}
}

func TestARowPausedRightAfterItsStartIsNotStartedAfterThePause(t *testing.T) {
	be := newOrderBackend()
	be.setup = 20 * time.Millisecond
	a := startsFor(t, be, 1000)

	for i := range 20 {
		id := fmt.Sprintf("p%03d", i)
		queueElsewhere(a, id)
		a.Pause(id)
	}
	waitForStarts(t, a)

	be.mu.Lock()
	defer be.mu.Unlock()
	if len(be.afterPause) > 0 {
		t.Errorf("%d of 20 rows reached the backend after their pause", len(be.afterPause))
	}
}

func TestRemovingASelectionStartsNoneOfItsRowsAfterTheirRemoval(t *testing.T) {
	be := newOrderBackend()
	be.setup = 20 * time.Millisecond
	a := startsFor(t, be, 1)
	var ids []string
	for i := range 20 {
		id := fmt.Sprintf("q%03d", i)
		ids = append(ids, id)
		queueElsewhere(a, id)
	}

	a.RemoveTasks(ids, true)
	waitForStarts(t, a)

	be.mu.Lock()
	defer be.mu.Unlock()
	if len(be.afterGone) > 0 {
		t.Errorf("the removal started %v after removing them", be.afterGone)
	}
}
