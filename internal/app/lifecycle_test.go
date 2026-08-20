package app

// Coverage for the one contract App.spawn and App.Close share: everything
// spawn accepts is finished before Close returns, and everything Close has
// already refused never starts at all. What makes that worth its own file is
// that the two halves used to be able to disagree - spawn checked the context
// and registered on a.wg as two separate steps, so a Close landing between
// them returned while a goroutine it had just promised to wait for was still
// on its way in. See track's own comment in app.go for the full shape.

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/store"
)

// lifecycleApp is an App with exactly the parts spawn and Close touch: the
// context pair, the WaitGroup (zero value), the close flag (zero value) and a
// store, because Close's last act is closing it. Every other subsystem Close
// walks is nil-checked and stays nil.
//
// Deliberately not New(t.TempDir()): a full boot opens the store, the settings
// file, the federation file and an embedded Gopeed downloader, and the store
// alone costs roughly 0.4s under -race. The race below needs hundreds of
// rounds to be worth running at all, and paying that per round would price it
// out of the suite - 200 rounds took 102s that way. The deterministic test
// further down uses a real App instead, so the booted path is not left
// unexercised.
//
// The rounds share one store for the same reason, and it is sound rather than
// merely cheap: nothing in this test reads or writes it, closing it is the
// last thing Close does, and database/sql makes DB.Close idempotent in so many
// words ("Make DB.Close idempotent"), so every round after the first closes an
// already-closed handle and gets the same nil back.
func lifecycleApp(st *store.Store) *App {
	a := &App{Store: st}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	return a
}

// TestApp_SpawnRacingCloseNeverMisusesTheWaitGroup hammers the window that
// used to be open between spawn's shutdown check and its a.wg.Add(1). With
// those two apart, a Close landing in the gap could cancel, find the WaitGroup
// counter at zero, return - and only then would the spawn register and start a
// goroutine nothing was waiting for any more, against an app whose store has
// just been closed underneath it. Both of that bug's outcomes fail this test:
// the goroutine that starts late trips the counter below, and the WaitGroup's
// own "Add called concurrently with Wait" panic takes the binary down.
//
// The exposure is real rather than theoretical here: a.spawn has live callers
// on goroutines nothing serialises against a shutdown - the availability
// probe, the checksum pass, a watch-folder job, the settled-task publish, the
// captcha and account-health loops - so an App.Close during ordinary operation
// is exactly the interleaving this covers.
//
// Probabilistic on purpose, and worth saying plainly rather than dressing up:
// the window is two adjacent statements wide, so no amount of test-side
// scheduling makes hitting it a certainty - the same honest limit the sibling
// fix in internal/script ran into with this class of race. What the rounds buy
// is repeated exposure under -race; what the invariant buys is that any hit at
// all is a hard, non-flaky failure rather than a judgement call.
//
// That it detects the real thing was established rather than assumed: with a
// throwaway 2ms sleep dropped between the old code's check and its Add - the
// window widened, nothing else changed - this failed on round 0 in five runs
// out of five, each time on the count below, with two to four goroutines
// having started after Close had already returned. With the same 2ms sleep
// moved inside track's lock on the fixed code, it passes: the width of the
// window stopped mattering once the check and the register became one step.
//
// The count is the detector rather than the WaitGroup's own misuse panic
// because that is what the probe actually produced - a late Add lands after
// Wait has returned, not during it. The panic is the same window's other
// documented outcome (sync annotates "Add called concurrently with Wait" as a
// data race, and -race reports it), reached when Wait is still in progress; no
// test can force which of the two a given interleaving gives, which is another
// reason to detect the goroutine itself and not the crash.
func TestApp_SpawnRacingCloseNeverMisusesTheWaitGroup(t *testing.T) {
	const (
		rounds     = 400
		spawners   = 4
		spawnsEach = 25
	)
	st, err := store.Open(filepath.Join(t.TempDir(), "lifecycle.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	for i := 0; i < rounds; i++ {
		a := lifecycleApp(st)

		var (
			closed     atomic.Bool
			afterClose atomic.Int64
			wg         sync.WaitGroup
			barrier    sync.WaitGroup
			start      = make(chan struct{})
		)
		barrier.Add(spawners + 1)
		wg.Add(spawners + 1)
		for j := 0; j < spawners; j++ {
			go func() {
				defer wg.Done()
				barrier.Done()
				<-start
				for k := 0; k < spawnsEach; k++ {
					// closed is set only after Close has returned, so a
					// goroutine that reads it as true really did start too
					// late; one that starts late but reads a hair before the
					// Store is simply missed. False negatives cost detection
					// rate, and there are no false positives.
					a.spawn(func() {
						if closed.Load() {
							afterClose.Add(1)
						}
					})
				}
			}()
		}
		go func() {
			defer wg.Done()
			barrier.Done()
			<-start
			if err := a.Close(); err != nil {
				t.Errorf("round %d: Close: %v", i, err)
			}
			closed.Store(true)
		}()
		// Released together rather than one after the other: the spawn stream
		// has to already be running when Close flips, or every call lands on
		// the same side of it and the window never gets exercised.
		barrier.Wait()
		close(start)
		wg.Wait()
		// A second Close, purely to drain: on a build where spawn could still
		// register past the first one, this waits for that stray goroutine so
		// the check below actually sees it instead of the test finishing
		// first. On a correct build there is nothing left to wait for and this
		// returns at once.
		if err := a.Close(); err != nil {
			t.Fatalf("round %d: second Close: %v", i, err)
		}
		if n := afterClose.Load(); n != 0 {
			t.Fatalf("round %d: %d goroutine(s) started after Close() returned - spawn registered past the shutdown", i, n)
		}
	}
}

// TestApp_CloseWaitsForWhatItAcceptedAndRefusesTheRest pins both halves of the
// contract on a real, fully booted App, deterministically - no racing, no
// rounds. It is the standing guard the probabilistic test above cannot be: it
// fails outright if the flag flip is ever taken back out of Close (track would
// then never refuse anything, so the spawn after Close both runs and Adds to a
// drained WaitGroup), and equally if a later change lets Close return before
// the work it accepted is done.
//
// It does not, on its own, catch the original bug: with the old check-then-act
// code a spawn issued after Close has already returned finds the context
// cancelled and refuses too. Catching the gap between those two steps is the
// race test's job above; keeping the two ends honest afterwards is this one's.
func TestApp_CloseWaitsForWhatItAcceptedAndRefusesTheRest(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var finished atomic.Bool
	release := make(chan struct{})
	a.spawn(func() {
		<-release
		finished.Store(true)
	})
	// Held open until after Close is under way, so "Close waited" is a real
	// observation and not one that would hold just as well if it had not.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !finished.Load() {
		t.Fatal("Close returned while a goroutine it had accepted through spawn was still running")
	}

	var ran atomic.Bool
	a.spawn(func() { ran.Store(true) })
	// Second Close for the same reason as the race test's: if that spawn had
	// been let through, it registered on a drained WaitGroup and this is what
	// gives it time to run and be seen.
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if ran.Load() {
		t.Fatal("a goroutine spawned after Close returned ran anyway - Close is no longer refusing new work")
	}
}
