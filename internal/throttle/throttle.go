// Package throttle caps the combined download speed of everything that copies
// bytes through it. One limiter is shared by all transfers, so the configured
// limit is a total for the app rather than a per-connection allowance.
//
// It also owns the pause. Pausing here holds the bytes still instead of
// cancelling anything: the sockets stay open, the servers keep the ranges they
// handed out, and resuming carries on with the same connections. A pause that
// tears the transfers down is a different button, and it costs a re-request per
// file to undo.
package throttle

import (
	"context"
	"io"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// chunk is how much a single copy step moves. Small enough that a limit change
// takes effect within a fraction of a second, large enough not to dominate the
// copy with bookkeeping.
const chunk = 32 * 1024

// Limiter hands out bandwidth. The zero value is not usable; call New.
type Limiter struct {
	mu  sync.RWMutex
	bps int64 // 0 = unlimited
	lim *rate.Limiter

	// resume is nil while transfers may move and a live channel while they may
	// not. Waiters block on the channel and are woken by closing it, so lifting
	// a pause reaches every transfer at once. The alternative - a flag the copy
	// loop checks, or a limit of zero bytes per second - is a goroutine per
	// transfer either spinning or sleeping out a tick before it notices, and the
	// user reads that delay as the resume not having worked.
	resume chan struct{}

	// held is the token balance at the moment of the pause, and heldLim the
	// limiter it belongs to. See SetPaused: the bucket goes on filling while
	// nothing may move, and that credit is not the paused transfer's to spend.
	held    float64
	heldLim *rate.Limiter
}

func New() *Limiter { return &Limiter{} }

// Set changes the total allowance in bytes per second; 0 removes the limit.
// Running transfers pick the new value up on their next chunk.
func (l *Limiter) Set(bps int64) {
	if bps < 0 {
		bps = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bps = bps
	if bps == 0 {
		l.lim = nil
		return
	}
	// The burst has to be at least one chunk, or a single WaitN could never be
	// satisfied and the copy would block forever.
	burst := int(bps)
	if burst < chunk {
		burst = chunk
	}
	if l.lim == nil {
		l.lim = rate.NewLimiter(rate.Limit(bps), burst)
		return
	}
	l.lim.SetLimit(rate.Limit(bps))
	l.lim.SetBurst(burst)
}

// Limit reports the current allowance in bytes per second (0 = unlimited).
// A pause does not change it: the limit the user configured is still the limit
// they configured, and it is what the transfers go back to on resume.
func (l *Limiter) Limit() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.bps
}

// SetPaused holds every transfer still, or lets them run again. It is
// idempotent, because the caller is a switch and a switch gets clicked twice.
func (l *Limiter) SetPaused(paused bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if paused == (l.resume != nil) {
		return
	}
	if paused {
		l.resume = make(chan struct{})
		// The rate limiter has no idea a pause is on and keeps filling its
		// bucket. Left alone, an hour of pause would be paid out on resume as
		// one instant burst up to the burst size, which is the exact spike the
		// limit exists to prevent. So the balance is remembered here and the
		// difference is spent on resume: the pause earns nothing, and the
		// allowance that had genuinely accrued before it is not lost either.
		l.heldLim = l.lim
		if l.lim != nil {
			l.held = l.lim.TokensAt(time.Now())
		}
		return
	}
	l.spendPauseCreditLocked()
	close(l.resume)
	l.resume = nil
}

// Paused reports whether transfers are being held.
func (l *Limiter) Paused() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.resume != nil
}

// spendPauseCreditLocked burns the tokens that accrued while nothing was
// allowed to move. Caller holds l.mu.
func (l *Limiter) spendPauseCreditLocked() {
	lim := l.lim
	// A limit set or cleared during the pause replaces the bucket the balance
	// was taken from, and there is nothing sensible to reconcile it against;
	// the new limit is what the user asked for, so it starts clean.
	if lim == nil || lim != l.heldLim {
		l.heldLim = nil
		return
	}
	l.heldLim = nil
	now := time.Now()
	extra := int(lim.TokensAt(now) - l.held)
	if extra <= 0 {
		return
	}
	// WaitN can leave the balance negative, which would make the difference
	// exceed the burst; AllowN refuses anything larger and would then burn
	// nothing at all, handing back the full bucket.
	if burst := lim.Burst(); extra > burst {
		extra = burst
	}
	lim.AllowN(now, extra)
}

// wait blocks while the limiter is paused. It returns as soon as the pause is
// lifted, or with the context's error if the caller gives up first - a paused
// transfer that is cancelled must still come apart.
func (l *Limiter) wait(ctx context.Context) error {
	l.mu.RLock()
	ch := l.resume
	l.mu.RUnlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Limiter) current() *rate.Limiter {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.lim
}

// Copy moves src into dst, waiting for allowance between chunks. With no limit
// set it degrades to a plain copy, so the unlimited path costs nothing.
func (l *Limiter) Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, chunk)
	var total int64
	for {
		// The pause is checked before the tokens are taken, so a held transfer
		// does not sit on an allowance it cannot use. A pause that lands after
		// this check still lets the chunk already in flight land: stopping that
		// one means abandoning a read that is halfway through, which is the
		// connection loss the pause exists to avoid.
		if err := l.wait(ctx); err != nil {
			return total, err
		}
		if lim := l.current(); lim != nil {
			if err := lim.WaitN(ctx, chunk); err != nil {
				return total, err
			}
		}
		n, err := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
			if f, ok := dst.(interface{ Flush() error }); ok {
				_ = f.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}
