package relay

// Backoff for failed handshakes, needed once this relay is operated
// publicly rather than by the person whose instances use it.
//
// This is explicitly NOT what keeps strangers out of a group. The key does
// that, and at 128 bits of entropy (see internal/seedphrase) guessing one
// is not a threat any rate limit needs to help with. What this stops is the
// other thing an open endpoint attracts: a client that fails the handshake
// over and over, each attempt costing a TCP connection, a TLS negotiation
// and a goroutine held for the length of helloTimeout. Left unbounded, that
// is a way to spend the relay's resources for free - and it buries the
// journal so deeply that a real failure becomes invisible in it.
//
// Keyed on the remote address, taken from the connection itself and never
// from a forwarded-for header. The relay is dialled directly (its DNS
// records are deliberately unproxied - see the deployment spec), so the
// socket's own peer IS the client; trusting a header here would instead let
// a caller pick its own bucket and walk straight past the limit.

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// failWindow is how long a failure stays on an address's record. Long
	// enough that a scripted attempt cannot simply pace itself under the
	// threshold, short enough that somebody who fixed a typo half an hour
	// ago is not still paying for it.
	failWindow = 10 * time.Minute

	// failsBeforeBlock is how many failures inside failWindow it takes to
	// start refusing. Set well above what a person fumbling a paste can
	// produce: a mistyped phrase fails once, they correct it and it works.
	// Ten in ten minutes is not a person.
	failsBeforeBlock = 10

	// baseBlock is the first refusal window, doubling with each further
	// failure while blocked. Deliberately short at the start - an address
	// that trips this once may be a person on a bad script, and a minute
	// costs them nothing while a persistent caller reaches maxBlock fast.
	baseBlock = 1 * time.Minute
	maxBlock  = 1 * time.Hour

	// maxTrackedAddrs bounds the limiter's own memory, so the mechanism
	// meant to stop resource exhaustion cannot become the thing causing it.
	// When full, the oldest record is dropped: an attacker can push an
	// address out only by making enough OTHER addresses fail recently, and
	// a real deployment never approaches this.
	maxTrackedAddrs = 4096
)

type attempts struct {
	fails        int
	last         time.Time
	blockedUntil time.Time
	blockFor     time.Duration
}

// limiter tracks failed handshakes per remote address.
//
// now is injectable so the tests can drive the clock instead of sleeping
// through a real backoff - a test that waits out a one-minute block is a
// test nobody runs.
type limiter struct {
	mu        sync.Mutex
	addrs     map[string]*attempts
	now       func() time.Time
	lastSweep time.Time
}

// sweepEvery is how often the request path may walk the whole map to drop
// records that have run out. Once a minute keeps the walk off the hot path of
// a flood while still bounding how long an address outlives its record.
const sweepEvery = time.Minute

func newLimiter() *limiter {
	return &limiter{addrs: map[string]*attempts{}, now: time.Now}
}

// blocked reports whether addr is currently being refused, and is called
// before the connection is even upgraded - a refused address should cost
// the relay a rejected HTTP request, not a live WebSocket.
func (l *limiter) blocked(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.maybeSweepLocked(now)
	a := l.addrs[addr]
	return a != nil && now.Before(a.blockedUntil)
}

// sweep drops every record whose failures have aged out and whose block, if
// it had one, is over.
//
// Without it a record lived until that same address completed a handshake,
// the map filled up, or the process restarted - so a scanner that failed once
// stayed in memory for the life of the relay, while the privacy policy said an
// hour. With it, a record runs out no later than maxBlock (one hour) after the
// address's last failed attempt - failWindow is shorter than that, and a block
// never outlasts maxBlock - and is deleted by the next sweep, so within
// maxBlock + sweepEvery (61 minutes). The privacy policy states that figure.
//
// Called from the request path at most once per sweepEvery, and on a timer by
// the public relay (cmd/knightloader-relay), so a relay that goes quiet
// forgets as well.
func (l *limiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(l.now())
}

func (l *limiter) maybeSweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.sweepLocked(now)
}

func (l *limiter) sweepLocked(now time.Time) {
	l.lastSweep = now
	for k, a := range l.addrs {
		if now.Sub(a.last) > failWindow && !now.Before(a.blockedUntil) {
			delete(l.addrs, k)
		}
	}
}

// tracked is how many addresses have a record right now. For the tests.
func (l *limiter) tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.addrs)
}

// fail records one failed handshake and extends the block if the address
// has now earned one.
func (l *limiter) fail(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.maybeSweepLocked(now)

	a := l.addrs[addr]
	if a == nil {
		l.evictIfFullLocked()
		a = &attempts{}
		l.addrs[addr] = a
	}
	// A gap longer than the window means the previous failures have aged
	// out; start the count again rather than letting an address accumulate
	// one failure a day into a block.
	if !a.last.IsZero() && now.Sub(a.last) > failWindow {
		a.fails = 0
		a.blockFor = 0
	}
	a.fails++
	a.last = now

	if a.fails < failsBeforeBlock {
		return
	}
	if a.blockFor == 0 {
		a.blockFor = baseBlock
	} else if a.blockFor < maxBlock {
		a.blockFor *= 2
		if a.blockFor > maxBlock {
			a.blockFor = maxBlock
		}
	}
	a.blockedUntil = now.Add(a.blockFor)
}

// succeed clears an address's record. A handshake that worked is proof the
// caller is a real client, so the failures that came before it - a
// half-typed phrase, an instance reconnecting during a key change - must
// not follow them around.
func (l *limiter) succeed(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.addrs, addr)
}

// evictIfFullLocked drops the least recently active record to make room.
// Called with the lock held.
func (l *limiter) evictIfFullLocked() {
	if len(l.addrs) < maxTrackedAddrs {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, a := range l.addrs {
		if oldestKey == "" || a.last.Before(oldest) {
			oldestKey, oldest = k, a.last
		}
	}
	delete(l.addrs, oldestKey)
}

// clientAddr is the bucket a request counts against: the peer's IP without
// its port, since a caller gets a fresh source port per connection and
// bucketing on the pair would make the limiter count every attempt as a
// first one.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Not host:port - use it whole rather than dropping the request on
		// the floor. Being unable to parse an address is not a reason to
		// stop limiting it.
		return r.RemoteAddr
	}
	return host
}
