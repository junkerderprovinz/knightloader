package app

// Coverage for the contract App.spawn and App.Close share: work spawn accepts
// finishes before Close returns, and work Close has refused never starts.

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/store"
)

// lifecycleApp is an App with only the parts spawn and Close touch: the context
// pair, the WaitGroup, the close flag and a store, since Close's last act is
// closing it. Every other subsystem Close walks is nil-checked and stays nil.
//
// New(t.TempDir()) would cost about 0.4s a round under -race, which prices the
// race below out of the suite; the deterministic test further down boots a real
// App. The rounds share one store because database/sql documents DB.Close as
// idempotent and nothing here reads or writes it.
func lifecycleApp(st *store.Store) *App {
	a := &App{Store: st}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	return a
}

// A spawn that checks for shutdown and registers on a.wg as two steps leaves a
// window in which Close cancels, sees the counter at zero and returns, after
// which the spawn starts a goroutine nothing waits for against a closed store.
// The window is two statements wide, so the rounds buy repeated exposure under
// -race rather than certainty; the counter below turns any hit into a hard
// failure instead of a judgement call.
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
					// late. One that reads a hair before the Store is missed,
					// which costs detection rate but never a false positive.
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
		// A second Close drains a goroutine that registered past the first one,
		// so the check below sees it instead of the test finishing first.
		if err := a.Close(); err != nil {
			t.Fatalf("round %d: second Close: %v", i, err)
		}
		if n := afterClose.Load(); n != 0 {
			t.Fatalf("round %d: %d goroutine(s) started after Close() returned; spawn registered past the shutdown", i, n)
		}
	}
}

// Both halves of the contract on a fully booted App, without racing: Close
// waits for the work it accepted, and refuses everything after it.
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
	// Held open until Close is under way, so the check below observes a wait
	// rather than a goroutine that had already finished on its own.
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
	// Second Close as in the race test: a spawn that was let through gets time
	// to run and be seen.
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if ran.Load() {
		t.Fatal("a goroutine spawned after Close returned ran anyway")
	}
}
