package relay

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// testLimiter returns a limiter whose clock the test drives, so a backoff
// can be waited out in nanoseconds instead of minutes.
func testLimiter() (*limiter, func(time.Duration)) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	l := newLimiter()
	l.now = func() time.Time { return now }
	return l, func(d time.Duration) { now = now.Add(d) }
}

// The limit must never fire on the shape a real person produces: a mistyped
// phrase, corrected, and on with the day.
func TestLimiterLetsOccasionalFailuresThrough(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < failsBeforeBlock-1; i++ {
		l.fail("198.51.100.7")
		if l.blocked("198.51.100.7") {
			t.Fatalf("blocked after only %d failures, threshold is %d", i+1, failsBeforeBlock)
		}
	}
}

func TestLimiterBlocksAfterThreshold(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < failsBeforeBlock; i++ {
		l.fail("198.51.100.7")
	}
	if !l.blocked("198.51.100.7") {
		t.Fatalf("not blocked after %d failures", failsBeforeBlock)
	}
}

// One address failing must not cost anybody else anything - the limiter is
// meant to be surgical, not a switch that takes the relay off the air.
func TestLimiterIsPerAddress(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < failsBeforeBlock*3; i++ {
		l.fail("198.51.100.7")
	}
	if !l.blocked("198.51.100.7") {
		t.Fatal("the offending address is not blocked")
	}
	if l.blocked("203.0.113.9") {
		t.Fatal("an unrelated address was blocked")
	}
}

func TestLimiterBlockExpires(t *testing.T) {
	l, advance := testLimiter()
	for i := 0; i < failsBeforeBlock; i++ {
		l.fail("198.51.100.7")
	}
	if !l.blocked("198.51.100.7") {
		t.Fatal("not blocked")
	}
	advance(baseBlock + time.Second)
	if l.blocked("198.51.100.7") {
		t.Fatal("still blocked after the first backoff elapsed")
	}
}

// Each further failure while already over the threshold has to cost more
// than the last, or a caller simply waits out a fixed penalty forever.
func TestLimiterBackoffGrows(t *testing.T) {
	l, advance := testLimiter()
	for i := 0; i < failsBeforeBlock; i++ {
		l.fail("198.51.100.7")
	}
	first := l.addrs["198.51.100.7"].blockFor

	advance(first + time.Second)
	l.fail("198.51.100.7")
	second := l.addrs["198.51.100.7"].blockFor

	if second <= first {
		t.Fatalf("backoff did not grow: %s then %s", first, second)
	}
}

func TestLimiterBackoffIsCapped(t *testing.T) {
	l, advance := testLimiter()
	for i := 0; i < failsBeforeBlock+40; i++ {
		l.fail("198.51.100.7")
		advance(time.Second)
	}
	if got := l.addrs["198.51.100.7"].blockFor; got > maxBlock {
		t.Fatalf("backoff grew to %s, cap is %s", got, maxBlock)
	}
}

// A handshake that worked proves the caller is real, so what came before it
// must not follow them around - otherwise an instance that reconnects a few
// times during a key change ends up locked out afterwards.
func TestLimiterSuccessClearsTheRecord(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < failsBeforeBlock-1; i++ {
		l.fail("198.51.100.7")
	}
	l.succeed("198.51.100.7")

	for i := 0; i < failsBeforeBlock-1; i++ {
		l.fail("198.51.100.7")
		if l.blocked("198.51.100.7") {
			t.Fatal("old failures still counted after a successful handshake")
		}
	}
}

// Failures spread far enough apart are not an attack, and must not
// accumulate into a block one-per-day.
func TestLimiterFailuresAgeOut(t *testing.T) {
	l, advance := testLimiter()
	for i := 0; i < failsBeforeBlock*2; i++ {
		l.fail("198.51.100.7")
		advance(failWindow + time.Minute)
		if l.blocked("198.51.100.7") {
			t.Fatalf("blocked by failures spaced more than %s apart", failWindow)
		}
	}
}

// The limiter must not become the resource exhaustion it exists to prevent.
func TestLimiterBoundsItsOwnMemory(t *testing.T) {
	l, advance := testLimiter()
	for i := 0; i < maxTrackedAddrs+500; i++ {
		l.fail(strings.Repeat("a", 3) + string(rune('0'+i%10)) + "." + string(rune('a'+i%26)) + itoa(i))
		advance(time.Millisecond)
	}
	if got := len(l.addrs); got > maxTrackedAddrs {
		t.Fatalf("limiter tracks %d addresses, cap is %d", got, maxTrackedAddrs)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// Buckets are per IP, not per connection: a caller gets a fresh source port
// every time, so counting the pair would make every attempt look like a
// first one and the limiter would never fire at all.
func TestClientAddrDropsThePort(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"198.51.100.7:54321", "198.51.100.7"},
		{"198.51.100.7:1", "198.51.100.7"},
		{"[2a01:4f8:c014:3544::1]:44444", "2a01:4f8:c014:3544::1"},
		{"no-port-here", "no-port-here"},
	} {
		got := clientAddr(&http.Request{RemoteAddr: c.in})
		if got != c.want {
			t.Errorf("clientAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
