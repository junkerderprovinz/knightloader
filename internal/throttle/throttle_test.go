package throttle

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// TestCopyRespectsLimit proves the limiter actually slows a transfer down: a
// quarter megabyte at 128 KiB/s cannot arrive in under a second.
func TestCopyRespectsLimit(t *testing.T) {
	l := New()
	l.Set(128 * 1024)

	src := bytes.NewReader(make([]byte, 256*1024))
	start := time.Now()
	n, err := l.Copy(context.Background(), io.Discard, src)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if n != 256*1024 {
		t.Fatalf("copied %d bytes, want %d", n, 256*1024)
	}
	// The first burst is free, so only the second half is actually paced.
	if took < 700*time.Millisecond {
		t.Errorf("256 KiB at 128 KiB/s took %v, expected to be paced", took)
	}
}

// TestUnlimitedIsFast pins the other direction: with no limit the same copy must
// not be paced at all.
func TestUnlimitedIsFast(t *testing.T) {
	l := New()
	src := bytes.NewReader(make([]byte, 4*1024*1024))
	start := time.Now()
	if _, err := l.Copy(context.Background(), io.Discard, src); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("unlimited copy took %v", took)
	}
	if l.Limit() != 0 {
		t.Errorf("Limit() = %d, want 0", l.Limit())
	}
}

// TestSetIsLive checks a limit can be lifted mid-flight, which is what makes the
// settings page take effect on running downloads.
func TestSetIsLive(t *testing.T) {
	l := New()
	l.Set(64 * 1024)
	if l.Limit() != 64*1024 {
		t.Fatalf("Limit() = %d", l.Limit())
	}
	l.Set(0)
	if l.current() != nil {
		t.Error("clearing the limit left a limiter behind")
	}
	l.Set(-5)
	if l.Limit() != 0 {
		t.Errorf("negative limit became %d, want 0", l.Limit())
	}
}

// TestCopyHonoursCancel makes sure a cancelled download does not sit in the
// limiter forever waiting for allowance.
func TestCopyHonoursCancel(t *testing.T) {
	l := New()
	l.Set(1024) // slow enough that the second chunk has to wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := l.Copy(ctx, io.Discard, bytes.NewReader(make([]byte, 1024*1024)))
	if err == nil {
		t.Error("copy with a cancelled context returned no error")
	}
}

// countingWriter is read by the test goroutine while the copy it belongs to is
// still running, which is why the counter is behind a mutex.
type countingWriter struct {
	mu    sync.Mutex
	n     int64
	first time.Time // when the first byte landed
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.n == 0 {
		w.first = time.Now()
	}
	w.n += int64(len(p))
	w.mu.Unlock()
	return len(p), nil
}

func (w *countingWriter) count() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

func (w *countingWriter) firstAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.first
}

// TestPauseHoldsBytesAndResumesAtOnce is the whole pause contract in one run:
// nothing moves while it is on, the transfer is still there afterwards, and the
// first byte follows the resume immediately. The last part is the one worth
// pinning - a pause built as a flag somebody polls passes the first two checks
// and still leaves the user staring at a dead progress bar for a tick.
func TestPauseHoldsBytesAndResumesAtOnce(t *testing.T) {
	const size = 64 * 1024
	l := New()
	l.SetPaused(true)
	if !l.Paused() {
		t.Fatal("SetPaused(true) did not take")
	}

	w := &countingWriter{}
	done := make(chan error, 1)
	go func() {
		_, err := l.Copy(context.Background(), w, bytes.NewReader(make([]byte, size)))
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)
	if got := w.count(); got != 0 {
		t.Fatalf("%d bytes moved while paused", got)
	}
	select {
	case err := <-done:
		t.Fatalf("the copy ran to completion while paused (err %v)", err)
	default:
	}

	// No limit is set, so the only thing between the resume and the first byte
	// is the wake-up itself.
	resumedAt := time.Now()
	l.SetPaused(false)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if l.Paused() {
		t.Error("still paused after the resume")
	}
	if got := w.count(); got != size {
		t.Errorf("copied %d bytes after the resume, want %d", got, size)
	}
	if lag := w.firstAt().Sub(resumedAt); lag > 100*time.Millisecond {
		t.Errorf("the first byte came %v after the resume; waking a transfer must not wait for a tick", lag)
	}
}

// TestResumeDoesNotPayOutThePause guards the other half of that trap: holding
// the bytes is easy, not banking the time is not. The rate limiter fills its
// bucket while nothing may move, and handing that back turns every resume into
// a spike straight through the configured limit.
func TestResumeDoesNotPayOutThePause(t *testing.T) {
	const bps = 128 * 1024 // burst is one second's worth, so a 1.1s pause fills it
	l := New()
	l.Set(bps)
	ctx := context.Background()

	// Spend the free opening burst, so the pause starts from an empty bucket and
	// anything available afterwards was earned by the pause itself.
	if _, err := l.Copy(ctx, io.Discard, bytes.NewReader(make([]byte, bps))); err != nil {
		t.Fatal(err)
	}

	l.SetPaused(true)
	time.Sleep(1100 * time.Millisecond)
	l.SetPaused(false)

	start := time.Now()
	if _, err := l.Copy(ctx, io.Discard, bytes.NewReader(make([]byte, 32*1024))); err != nil {
		t.Fatal(err)
	}
	// From an empty bucket this chunk has to be waited for. Arriving instantly
	// means the pause was banked and paid out.
	if took := time.Since(start); took < 300*time.Millisecond {
		t.Errorf("a chunk arrived %v after a 1.1s pause, so the pause was banked as credit", took)
	}
}

// TestPauseKeepsTheConfiguredLimit: the pause is not a limit of its own, so it
// must not overwrite the one the user set. It is driven by a switch, so it also
// has to survive being clicked twice.
func TestPauseKeepsTheConfiguredLimit(t *testing.T) {
	l := New()
	l.Set(64 * 1024)
	l.SetPaused(true)
	l.SetPaused(true)
	if !l.Paused() {
		t.Fatal("not paused")
	}
	if l.Limit() != 64*1024 {
		t.Errorf("the pause rewrote the configured limit to %d", l.Limit())
	}
	l.SetPaused(false)
	l.SetPaused(false)
	if l.Paused() {
		t.Error("still paused")
	}
	if l.Limit() != 64*1024 {
		t.Errorf("the resume rewrote the configured limit to %d", l.Limit())
	}
	if l.current() == nil {
		t.Error("the resume threw the rate limiter away")
	}
}

// TestPausedCopyHonoursCancel: a paused transfer is still cancellable. Without
// this, closing the app or removing a task while paused would block on a copy
// nothing is ever going to wake.
func TestPausedCopyHonoursCancel(t *testing.T) {
	l := New()
	l.SetPaused(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := l.Copy(ctx, io.Discard, bytes.NewReader(make([]byte, 1024)))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled paused copy returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a paused copy ignored its cancelled context")
	}
}
