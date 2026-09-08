package mediahook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// counting is a far end that only counts. Every runner test is about WHEN and
// HOW OFTEN a call is made rather than about what came back, which call_test.go
// already covers.
type counting struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func countingServer(t *testing.T) *counting {
	t.Helper()
	c := &counting{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// testTick is short enough that a test finishes and long enough that a loaded CI
// box is not spinning. Every wait below is expressed in ticks, never in an
// absolute sleep: a test that sleeps for a fixed time either wastes it or fails
// under load, and this one has a real condition to wait for.
const testTick = 5 * time.Millisecond

// eventually polls cond for up to a second and fails with why when it never
// holds. A poll and not a channel, because what is being waited for is a side
// effect in another goroutine's map rather than a message.
func eventually(t *testing.T, why string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(testTick)
	}
	t.Fatalf("timed out waiting for %s", why)
}

// never asserts cond does NOT come true within a few ticks. Bounded and short:
// it is the only shape available for "this call must not go out yet", and the
// alternative - waiting a second on every such assertion - would put ten seconds
// on the suite for nothing.
func never(t *testing.T, why string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * testTick)
	for time.Now().Before(deadline) {
		if cond() {
			t.Fatalf("%s happened and should not have", why)
		}
		time.Sleep(testTick)
	}
}

func startedRunner(t *testing.T, o Options) *Runner {
	t.Helper()
	if o.Tick == 0 {
		o.Tick = testTick
	}
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	r := New(o)
	r.Start()
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestOnePackageMakesOneCall(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}
	r := startedRunner(t, Options{Hooks: func() []Hook { return []Hook{hook} }})

	r.Enqueue("jellyfin", "Die.Serie.S01E01")

	eventually(t, "the call to go out", func() bool { return srv.hits.Load() == 1 })
	last, ok := r.Last("jellyfin")
	if !ok || !last.OK {
		t.Fatalf("Last = %+v, ok=%v", last, ok)
	}
	if last.Package != "Die.Serie.S01E01" || last.Packages != 1 {
		t.Errorf("the result names %q x%d, want the one package", last.Package, last.Packages)
	}
	if last.Test {
		t.Error("a firing was recorded as a test call")
	}
}

// TestABurstBecomesOneCall is what the wait window is FOR. Twenty packages
// finishing in one 2-second sweep is twenty firings, and a library scan costs the
// media server real work: twenty of them started together is a media server
// unusable for ten minutes because a download finished.
func TestABurstBecomesOneCall(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet, WaitSeconds: 0}
	r := startedRunner(t, Options{Hooks: func() []Hook { return []Hook{hook} }})

	for _, pkg := range []string{"Zweite.Serie", "Andere.Serie", "Erste.Serie"} {
		r.Enqueue("jellyfin", pkg)
	}

	eventually(t, "the one call for the burst", func() bool { return srv.hits.Load() >= 1 })
	last, _ := r.Last("jellyfin")
	if last.Packages != 3 {
		t.Errorf("the call folded %d packages, want 3", last.Packages)
	}
	// The FIRST by name and not by arrival: two packages finishing in one sweep
	// arrive in an order the app went out of its way to make deterministic, and
	// a result that named a different one on every run would be a report nobody
	// could reproduce.
	if last.Package != "Andere.Serie" {
		t.Errorf("the call names %q, want the first package by name", last.Package)
	}
	// And nothing follows it: the whole point is one call, not one plus a
	// straggler.
	never(t, "a second call for the same burst", func() bool { return srv.hits.Load() > 1 })
}

// TestTheWindowIsMeasuredFromTheFirstPackage. Extending it on every later
// package would mean a box that finishes a package a minute for an hour never
// calls at all - the shape of bug that only shows up on the busiest install.
func TestTheWindowIsMeasuredFromTheFirstPackage(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet, WaitSeconds: 0}
	r := startedRunner(t, Options{
		Tick:  time.Hour, // nothing fires on its own; the arming is what is inspected
		Hooks: func() []Hook { return []Hook{hook} },
	})
	pending := map[string]*waiting{}
	first := time.Now()
	r.arm(pending, armed{hookID: "jellyfin", pkg: "a", arrived: first})
	r.arm(pending, armed{hookID: "jellyfin", pkg: "b", arrived: first.Add(time.Minute)})
	w := pending["jellyfin"]
	if w == nil {
		t.Fatal("nothing was armed")
	}
	if !w.fireAt.Equal(first) {
		t.Errorf("the window closes at %v, want the first package's own moment %v", w.fireAt, first)
	}
	if w.count != 2 {
		t.Errorf("the second package was not folded in: count=%d", w.count)
	}
	_ = srv
}

// TestTheCallWaitsForTheFilesToLand is the decision this whole feature turns on.
//
// package.done fires the moment nothing is left to WAIT for, and on an install
// with a working folder the finished file is at that moment still IN the working
// folder - the status is set and then the move is spawned. A media server told to
// scan then finds nothing and never looks again.
func TestTheCallWaitsForTheFilesToLand(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}
	var landed atomic.Bool
	r := startedRunner(t, Options{
		Hooks: func() []Hook { return []Hook{hook} },
		Ready: func(string) bool { return landed.Load() },
	})

	r.Enqueue("jellyfin", "Der.Film")
	never(t, "the call while the file is still in the working folder", func() bool { return srv.hits.Load() > 0 })

	landed.Store(true)
	eventually(t, "the call once the file has been moved", func() bool { return srv.hits.Load() == 1 })
}

// TestTheDeliveryWaitGivesUp. A move can fail permanently - no room on the target
// volume, a collision policy of "skip", a read-only mount - and the app records
// that on the row and leaves the file where it is. Without a ceiling the scan
// would simply never be asked for, which is worse than asking too early: nothing
// on any page would say so.
func TestTheDeliveryWaitGivesUp(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}
	r := startedRunner(t, Options{
		Grace: 10 * testTick,
		Hooks: func() []Hook { return []Hook{hook} },
		Ready: func(string) bool { return false },
	})

	r.Enqueue("jellyfin", "Der.Film")

	eventually(t, "the call to go out anyway once the grace is up", func() bool { return srv.hits.Load() == 1 })
}

// TestAnAddressDeletedDuringTheWaitCallsNothing. Deleting one between a package
// finishing and its window closing is an ordinary thing to do, and a call to
// nowhere is not worth keeping.
func TestAnAddressDeletedDuringTheWaitCallsNothing(t *testing.T) {
	srv := countingServer(t)
	var live atomic.Bool
	live.Store(true)
	r := startedRunner(t, Options{
		Hooks: func() []Hook {
			if !live.Load() {
				return nil
			}
			return []Hook{{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}}
		},
		Ready: func(string) bool { return false },
		Grace: 6 * testTick,
	})

	r.Enqueue("jellyfin", "Der.Film")
	live.Store(false)

	never(t, "a call to an address that is no longer stored", func() bool { return srv.hits.Load() > 0 })
}

// TestTheAddressIsReReadWhenTheCallGoesOut. A window can be an hour long, and an
// address edited during one has to be called as it is NOW.
func TestTheAddressIsReReadWhenTheCallGoesOut(t *testing.T) {
	stale := countingServer(t)
	fresh := countingServer(t)
	var moved atomic.Bool
	r := startedRunner(t, Options{
		Hooks: func() []Hook {
			url := stale.srv.URL
			if moved.Load() {
				url = fresh.srv.URL
			}
			return []Hook{{ID: "jellyfin", URL: url, Method: MethodGet}}
		},
		Ready: func(string) bool { return moved.Load() },
	})

	r.Enqueue("jellyfin", "Der.Film")
	moved.Store(true)

	eventually(t, "the call to the edited address", func() bool { return fresh.hits.Load() == 1 })
	if stale.hits.Load() != 0 {
		t.Errorf("the address as it was when the package finished was called %d times", stale.hits.Load())
	}
}

// TestTwoDrawersWithTwoAddressesBothGetCalled: a package whose links sit in two
// drawers is normal, and each address is called once.
func TestTwoDrawersWithTwoAddressesBothGetCalled(t *testing.T) {
	jelly := countingServer(t)
	plex := countingServer(t)
	r := startedRunner(t, Options{Hooks: func() []Hook {
		return []Hook{
			{ID: "jellyfin", URL: jelly.srv.URL, Method: MethodGet},
			{ID: "plex", URL: plex.srv.URL, Method: MethodGet},
		}
	}})

	r.Enqueue("jellyfin", "Der.Film")
	r.Enqueue("plex", "Der.Film")

	eventually(t, "both addresses to be called", func() bool { return jelly.hits.Load() == 1 && plex.hits.Load() == 1 })
}

// TestEnqueueNeverBlocks is the contract script.Bus states in capitals: delivery
// is synchronous on the publisher's own goroutine, and that publisher is the
// app's package sweep. A full queue has to drop rather than wait.
//
// The runner is deliberately NOT started, so nothing drains the channel and the
// queue is genuinely full by the end.
func TestEnqueueNeverBlocks(t *testing.T) {
	r := New(Options{Hooks: func() []Hook { return []Hook{{ID: "jellyfin", URL: "http://x.lan/", Method: MethodGet}} }})
	done := make(chan struct{})
	go func() {
		for i := 0; i < queueDepth*2; i++ {
			r.Enqueue("jellyfin", "pkg")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full queue; the package sweep would have stalled with it")
	}
}

// TestCloseFlushesNothing. A call held back because its files have not landed is
// a call whose whole point was the files having landed; firing it on the way out
// would tell the media server to scan a folder this shutdown just stopped moving
// things into.
func TestCloseFlushesNothing(t *testing.T) {
	srv := countingServer(t)
	r := New(Options{
		Tick:   testTick,
		Client: http.DefaultClient,
		Hooks:  func() []Hook { return []Hook{{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}} },
		Ready:  func(string) bool { return false },
	})
	r.Start()
	r.Enqueue("jellyfin", "Der.Film")
	time.Sleep(4 * testTick)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if srv.hits.Load() != 0 {
		t.Errorf("Close sent %d held call(s)", srv.hits.Load())
	}
}

// TestCallNowIsRecordedAsATest: the Test button goes out through the same client
// and the same Call a real firing does, and the card has to be able to say which
// of the two it is looking at.
func TestCallNowIsRecordedAsATest(t *testing.T) {
	srv := countingServer(t)
	hook := Hook{ID: "jellyfin", URL: srv.srv.URL, Method: MethodGet}
	r := startedRunner(t, Options{Hooks: func() []Hook { return []Hook{hook} }})

	res := r.CallNow(context.Background(), hook)
	if !res.OK || !res.Test {
		t.Fatalf("CallNow = %+v", res)
	}
	last, ok := r.Last("jellyfin")
	if !ok || !last.Test {
		t.Errorf("Last = %+v, ok=%v, want the test call", last, ok)
	}
	if last.Package != "" {
		t.Errorf("a test call names the package %q", last.Package)
	}
}

// TestLastIsEmptyBeforeAnythingHasBeenCalled, which is what the card draws "not
// called since this server started" from. It has to be told apart from a call
// that failed.
func TestLastIsEmptyBeforeAnythingHasBeenCalled(t *testing.T) {
	r := New(Options{})
	if _, ok := r.Last("jellyfin"); ok {
		t.Error("a runner that has called nothing reports a last call")
	}
	if _, ok := r.Last(""); ok {
		t.Error("an unusable id reports a last call")
	}
}
