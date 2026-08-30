package api

// The container relay's own count, which is what the status strip renders as
// "Containers - N pending". The count is the relay's map size rather than a
// separate tally kept alongside it, so these tests pin the three moments it
// can change: a handover arrives, a backend collects one, and the TTL runs out
// on one nobody collected.
//
// Written against the relay directly, not through the HTTP surface: handToJD
// needs a configured JD backend to get as far as put(), and what is under test
// here is the bookkeeping, not the routing.

import (
	"sync"
	"testing"
	"time"
)

// countSpy records every count the relay publishes, in order.
type countSpy struct {
	mu   sync.Mutex
	seen []int
}

func (c *countSpy) record(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, n)
}

func (c *countSpy) last() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seen) == 0 {
		return 0, false
	}
	return c.seen[len(c.seen)-1], true
}

func (c *countSpy) all() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.seen...)
}

func TestRelayPublishesCountOnPutAndTake(t *testing.T) {
	spy := &countSpy{}
	cr := newContainerRelay()
	cr.onCount = spy.record

	first, err := cr.put("a.dlc", []byte("one"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if n, ok := spy.last(); !ok || n != 1 {
		t.Fatalf("after one put: got %v (present=%v), want 1", n, ok)
	}

	if _, err := cr.put("b.dlc", []byte("two")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if n, _ := spy.last(); n != 2 {
		t.Fatalf("after two puts: got %d, want 2", n)
	}

	if _, ok := cr.take(first); !ok {
		t.Fatal("take: the token just handed out was not found")
	}
	if n, _ := spy.last(); n != 1 {
		t.Fatalf("after one take: got %d, want 1", n)
	}
}

// A miss still reports, because the sweep inside take may have dropped an
// expired entry - a change the strip has to hear about.
func TestRelayPublishesOnUnknownToken(t *testing.T) {
	spy := &countSpy{}
	cr := newContainerRelay()
	cr.onCount = spy.record

	if _, ok := cr.take("no-such-token"); ok {
		t.Fatal("take reported a hit for a token that was never issued")
	}
	if n, ok := spy.last(); !ok || n != 0 {
		t.Fatalf("after a miss: got %v (present=%v), want a published 0", n, ok)
	}
}

// The regression this whole count exists to avoid: a handover nobody collects
// must not leave the strip reading "1 pending" forever. Nothing else sweeps -
// sweepLocked only runs on the next put or take - so the timer put() arms is
// the only thing that can clear it on a quiet instance.
func TestRelayClearsCountAfterTTL(t *testing.T) {
	restore := relayTTLForTest(80 * time.Millisecond)
	defer restore()

	spy := &countSpy{}
	cr := newContainerRelay()
	cr.onCount = spy.record

	if _, err := cr.put("orphan.dlc", []byte("nobody collects this")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if n, _ := spy.last(); n != 1 {
		t.Fatalf("right after put: got %d, want 1", n)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n, ok := spy.last(); ok && n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the count never fell back to 0 after the TTL expired; saw %v", spy.all())
}

// A relay with no listener must not panic - registerContainers always wires
// one, but put/take are exercised directly by tests and by any future caller.
func TestRelayWithoutListener(t *testing.T) {
	cr := newContainerRelay()
	tok, err := cr.put("quiet.dlc", []byte("x"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, ok := cr.take(tok); !ok {
		t.Fatal("take: token not found")
	}
}
