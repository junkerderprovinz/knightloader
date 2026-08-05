// Package throttle caps the combined download speed of everything that copies
// bytes through it. One limiter is shared by all transfers, so the configured
// limit is a total for the app rather than a per-connection allowance.
package throttle

import (
	"context"
	"io"
	"sync"

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
func (l *Limiter) Limit() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.bps
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
