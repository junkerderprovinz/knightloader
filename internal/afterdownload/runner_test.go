package afterdownload

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// gate is a Ready the test opens package by package.
type gate struct {
	mu   sync.Mutex
	open map[string]bool
}

func (g *gate) ready(pkg string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open[pkg]
}

func (g *gate) let(pkg string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open == nil {
		g.open = map[string]bool{}
	}
	g.open[pkg] = true
}

func startRunner(t *testing.T, f Fortress, g *gate, grace time.Duration) *Runner {
	t.Helper()
	r := New(Options{
		Fortress: func() Fortress { return f },
		Ready:    g.ready,
		Tick:     5 * time.Millisecond,
		Retry:    20 * time.Millisecond,
		Grace:    grace,
	})
	r.Start()
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func waitForLast(t *testing.T, r *Runner, target Target) Result {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := r.Last(target); ok {
			return res
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nothing was handed to %+v", target)
	return Result{}
}

func TestTheJobStartsOnlyOnceThePackagesFilesHaveLanded(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	g := &gate{}
	r := startRunner(t, f, g, time.Hour)
	photos := Target{Member: "m-tower", Job: "photos"}

	r.Enqueue(photos, "Holiday")
	time.Sleep(100 * time.Millisecond)
	if runs := f.runs("m-tower"); len(runs) != 0 {
		t.Fatalf("the job started before the files landed: %v", runs)
	}
	g.let("Holiday")
	res := waitForLast(t, r, photos)
	if res.Status != StatusStarted || res.Error != "" || res.Package != "Holiday" || res.Job != "photos" {
		t.Errorf("the hand-over reads %+v", res)
	}
	if runs := f.runs("m-tower"); !slices.Equal(runs, []string{"photos"}) {
		t.Errorf("the engine was asked %v", runs)
	}
}

func TestPackagesWaitingForTheSameJobStartItOnce(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	g := &gate{}
	r := startRunner(t, f, g, time.Hour)
	photos := Target{Member: "m-tower", Job: "photos"}

	r.Enqueue(photos, "Holiday")
	r.Enqueue(photos, "Birthday")
	r.Enqueue(photos, "Holiday")
	time.Sleep(50 * time.Millisecond)
	g.let("Holiday")
	time.Sleep(100 * time.Millisecond)
	if runs := f.runs("m-tower"); len(runs) != 0 {
		t.Fatalf("the job started while Birthday was still on its way: %v", runs)
	}
	g.let("Birthday")
	waitForLast(t, r, photos)
	time.Sleep(100 * time.Millisecond)
	if runs := f.runs("m-tower"); !slices.Equal(runs, []string{"photos"}) {
		t.Errorf("the engine was asked %v, want photos once", runs)
	}
}

func TestEachJobAPackageNamesIsStarted(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	f.addEngine("m-attic", "Attic", fakeJob{Name: "films", Mode: "mirror"})
	g := &gate{}
	g.let("Holiday")
	r := startRunner(t, f, g, time.Hour)

	r.Enqueue(Target{Member: "m-tower", Job: "photos"}, "Holiday")
	r.Enqueue(Target{Member: "m-attic", Job: "films"}, "Holiday")
	waitForLast(t, r, Target{Member: "m-tower", Job: "photos"})
	waitForLast(t, r, Target{Member: "m-attic", Job: "films"})
}

// A move that failed for good leaves the files where they are, and the job
// still starts once the grace is over rather than never.
func TestAfterTheGraceTheJobStartsAnyway(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	r := startRunner(t, f, &gate{}, 50*time.Millisecond)
	photos := Target{Member: "m-tower", Job: "photos"}

	r.Enqueue(photos, "Holiday")
	if res := waitForLast(t, r, photos); res.Status != StatusStarted {
		t.Errorf("the hand-over reads %+v", res)
	}
}

func TestAnEngineThatIsAwayIsAskedAgainUntilItAnswers(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	f.setUnreachable("m-tower", true)
	g := &gate{}
	g.let("Holiday")
	r := startRunner(t, f, g, time.Hour)
	photos := Target{Member: "m-tower", Job: "photos"}

	r.Enqueue(photos, "Holiday")
	if res := waitForLast(t, r, photos); res.Error != CodeUnreachable {
		t.Fatalf("the first try reads %+v", res)
	}
	f.setUnreachable("m-tower", false)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if res, _ := r.Last(photos); res.Status == StatusStarted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	res, _ := r.Last(photos)
	t.Fatalf("the engine came back and the hand-over still reads %+v", res)
}

func TestARefusalIsKeptAndNotRetried(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	g := &gate{}
	g.let("Holiday")
	r := startRunner(t, f, g, time.Hour)
	inbox := Target{Member: "m-tower", Job: "inbox"}

	r.Enqueue(inbox, "Holiday")
	if res := waitForLast(t, r, inbox); res.Error != CodeJobPaused || res.Status != "" {
		t.Errorf("the hand-over reads %+v", res)
	}
}

func TestWithoutAFortressTheHandOverSaysSo(t *testing.T) {
	g := &gate{}
	g.let("Holiday")
	r := startRunner(t, nil, g, time.Hour)
	photos := Target{Member: "m-tower", Job: "photos"}

	r.Enqueue(photos, "Holiday")
	if res := waitForLast(t, r, photos); res.Error != CodeNoFortress {
		t.Errorf("the hand-over reads %+v", res)
	}
}
