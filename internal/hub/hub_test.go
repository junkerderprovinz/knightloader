package hub

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeConn is a connection whose Write can be held open on demand, which is how
// these tests model a viewer on a bad link. Every field except the channels and
// the atomics is set before Add, so the writer goroutine only ever reads them.
type fakeConn struct {
	writes  chan []byte
	block   chan struct{} // if non-nil, Write waits for it to be closed
	entered chan struct{} // signalled once Write has actually been reached
	err     error
	closed  atomic.Bool
	writeN  atomic.Int64
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		writes:  make(chan []byte, 4*queueDepth),
		entered: make(chan struct{}, 1),
	}
}

func (f *fakeConn) Write(ctx context.Context, _ websocket.MessageType, p []byte) error {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.err != nil {
		return f.err
	}
	f.writeN.Add(1)
	f.writes <- append([]byte(nil), p...)
	return nil
}

func (f *fakeConn) CloseNow() error {
	f.closed.Store(true)
	return nil
}

// waitFor polls cond until it holds. Polling beats a fixed sleep here:
// goroutines wind down on their own schedule, and a sleep long enough to be safe
// on a loaded machine would slow the suite down for everybody.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// recv takes one message off a fake connection or fails, so a missing broadcast
// shows up as a named failure instead of a hung test.
func recv(t *testing.T, f *fakeConn, msg string) []byte {
	t.Helper()
	select {
	case got := <-f.writes:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
		return nil
	}
}

// TestBroadcastDoesNotWaitForASlowClient is the whole point of the per-client
// queue. If it failed, one viewer stuck in Write would add its write timeout to
// every progress update the other viewers are waiting for.
func TestBroadcastDoesNotWaitForASlowClient(t *testing.T) {
	h := New()
	slow := newFakeConn()
	slow.block = make(chan struct{})
	fast := newFakeConn()

	h.Add(slow)
	h.Add(fast)
	t.Cleanup(func() {
		close(slow.block)
		h.Remove(slow)
		h.Remove(fast)
	})

	// Wedge the slow client inside Write before the measured broadcast, so the
	// test cannot pass merely because its writer had not started yet.
	h.Broadcast("task", "warmup")
	select {
	case <-slow.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow client never reached Write")
	}
	recv(t, fast, "healthy client missed the warmup broadcast")

	done := make(chan struct{})
	go func() {
		h.Broadcast("task", "live")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked behind a client stuck in Write")
	}

	got := recv(t, fast, "healthy client did not get the broadcast while a slow client was stuck")
	if !bytes.Contains(got, []byte(`"live"`)) {
		t.Errorf("healthy client received %s, want the live message", got)
	}
	// The slow client is still parked inside Write at this point, which is what
	// makes both assertions above mean anything.
	if n := slow.writeN.Load(); n != 0 {
		t.Errorf("slow client completed %d writes, expected it to still be stuck", n)
	}
}

// TestOverFullClientIsDropped pins the back-pressure decision: a client that
// cannot drain its queue is disconnected, not waited for. Without this the
// queue would only move the stall from the broadcaster into memory growth.
func TestOverFullClientIsDropped(t *testing.T) {
	h := New()
	stuck := newFakeConn()
	stuck.block = make(chan struct{})
	h.Add(stuck)

	// One message ends up in flight inside Write and queueDepth more fit in the
	// queue, so anything past that has nowhere to go.
	for i := 0; i < queueDepth+5; i++ {
		h.Broadcast("task", i)
	}
	if h.Len() != 0 {
		t.Fatalf("hub still holds %d clients, want the over-full one dropped", h.Len())
	}

	// Letting the wedged write finish must make the writer notice it was
	// stopped and tear the socket down instead of draining the leftover queue.
	close(stuck.block)
	waitFor(t, stuck.closed.Load, "dropped client was never closed")

	healthy := newFakeConn()
	h.Add(healthy)
	t.Cleanup(func() { h.Remove(healthy) })
	h.Broadcast("task", "after-drop")
	got := recv(t, healthy, "hub stopped broadcasting after dropping a client")
	if !bytes.Contains(got, []byte("after-drop")) {
		t.Errorf("received %s, want the post-drop message", got)
	}
}

// TestWriteErrorRemovesClient covers the other way a connection dies: the write
// fails outright. A client left registered after that would collect broadcasts
// forever and eventually be dropped for a queue overflow that never mattered.
func TestWriteErrorRemovesClient(t *testing.T) {
	h := New()
	broken := newFakeConn()
	broken.err = errors.New("connection reset")
	h.Add(broken)

	h.Broadcast("task", "one")
	waitFor(t, func() bool { return h.Len() == 0 }, "client with a failing write stayed registered")
	waitFor(t, broken.closed.Load, "client with a failing write was never closed")
}

// TestSendToKeepsOrderWithBroadcast pins that a per-connection message goes
// through the same queue as the fan-out. A snapshot written straight to the
// socket could otherwise be overtaken by a task update the writer already had
// queued, and the UI would show stale state until the next event.
func TestSendToKeepsOrderWithBroadcast(t *testing.T) {
	h := New()
	c := newFakeConn()
	c.block = make(chan struct{})
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	if !h.SendTo(c, "snapshot", "first") {
		t.Fatal("SendTo reported failure for a registered connection")
	}
	h.Broadcast("task", "second")
	close(c.block)

	if got := recv(t, c, "no snapshot arrived"); !bytes.Contains(got, []byte("snapshot")) {
		t.Fatalf("first message was %s, want the snapshot", got)
	}
	if got := recv(t, c, "no task update arrived"); !bytes.Contains(got, []byte("second")) {
		t.Fatalf("second message was %s, want the task update", got)
	}
	if h.SendTo(newFakeConn(), "snapshot", "nobody") {
		t.Error("SendTo reported success for an unregistered connection")
	}
}

// TestAddRemoveUnderConcurrency runs the real access pattern: HTTP handlers add
// and remove connections while the download loop broadcasts. Under -race this
// fails if any of the hub state is touched outside the lock.
func TestAddRemoveUnderConcurrency(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	const workers = 16
	const rounds = 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				c := newFakeConn()
				h.Add(c)
				h.Add(c) // registering twice must not start a second writer
				h.SendTo(c, "snapshot", j)
				h.Len()
				h.Remove(c)
				h.Remove(c) // removing twice must not panic on a closed quit
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < workers*rounds; j++ {
			h.Broadcast("task", j)
		}
	}()
	wg.Wait()

	waitFor(t, func() bool { return h.Len() == 0 }, "connections were left registered")
}

// TestRemoveLeavesNoGoroutine guards the writer goroutines. Every WebSocket
// visitor starts one, so a writer that outlived its connection would leak a
// goroutine and a queue per page load.
func TestRemoveLeavesNoGoroutine(t *testing.T) {
	h := New()
	settle := func(target int) bool {
		return runtime.NumGoroutine() <= target
	}

	// Earlier tests may still be winding down, so the baseline is taken once the
	// process has gone quiet rather than at an arbitrary moment.
	base := runtime.NumGoroutine()
	waitFor(t, func() bool {
		n := runtime.NumGoroutine()
		if n <= base {
			base = n
			return true
		}
		base = n
		return false
	}, "goroutine count never settled before the test started")

	const conns = 50
	cs := make([]*fakeConn, conns)
	for i := range cs {
		cs[i] = newFakeConn()
		h.Add(cs[i])
	}
	h.Broadcast("task", "hello")
	for _, c := range cs {
		recv(t, c, "a registered client missed the broadcast")
	}
	for _, c := range cs {
		h.Remove(c)
	}

	// Wait for BOTH signals, not just the goroutine count: NumGoroutine() is a
	// process-wide counter, so it can coincidentally settle to the baseline
	// from unrelated scheduling even before every one of these 50 connections
	// has actually run its own cleanup and set closed - checking count alone
	// made this test intermittently flaky (github.com/junkerderprovinz/knightloader
	// CI, three unrelated PRs blocked by the same failure the same day).
	waitFor(t, func() bool {
		for _, c := range cs {
			if !c.closed.Load() {
				return false
			}
		}
		return settle(base)
	}, "writer goroutines outlived their connections")
	for _, c := range cs {
		if !c.closed.Load() {
			t.Fatal("a removed connection was never closed")
		}
	}
}

// TestUnsubscribedConnectionStillReceivesEverything pins the default every
// consumer before Subscribe existed was already written against: a
// connection that never sends a subscribe message must keep seeing every
// kind, unchanged, forever.
func TestUnsubscribedConnectionStillReceivesEverything(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Broadcast("task", "a")
	h.Broadcast("queue", "b")
	h.Broadcast("activity", "c")
	for _, want := range []string{"task", "queue", "activity"} {
		if got := recv(t, c, "an unsubscribed connection missed a broadcast"); !bytes.Contains(got, []byte(want)) {
			t.Errorf("got %s, want the %q broadcast next", got, want)
		}
	}
}

// TestSubscribeNarrowsToNamedKinds is the feature itself: a connection that
// only wants "activity" must not be woken for every "task" update in between.
func TestSubscribeNarrowsToNamedKinds(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Subscribe(c, []string{"activity"})
	h.Broadcast("task", "ignored")
	h.Broadcast("queue", "ignored-too")
	h.Broadcast("activity", "wanted")

	got := recv(t, c, "the subscribed kind never arrived")
	if !bytes.Contains(got, []byte("wanted")) {
		t.Fatalf("got %s, want the activity broadcast", got)
	}
	select {
	case extra := <-c.writes:
		t.Fatalf("received an unsubscribed broadcast: %s", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSubscribeCallsAddRatherThanReplace: two subscribe calls compose, so a
// page that asks for "task" and, separately, for "activity" ends up wanting
// both rather than only whichever call happened last.
func TestSubscribeCallsAddRatherThanReplace(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Subscribe(c, []string{"task"})
	h.Subscribe(c, []string{"activity"})
	h.Broadcast("task", "1")
	h.Broadcast("activity", "2")
	h.Broadcast("queue", "not this one")

	for _, want := range []string{"task", "activity"} {
		if got := recv(t, c, "a kind from an earlier subscribe call was dropped"); !bytes.Contains(got, []byte(want)) {
			t.Errorf("got %s, want %q", got, want)
		}
	}
	select {
	case extra := <-c.writes:
		t.Fatalf("received an unsubscribed broadcast: %s", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSubscribeWildcardResetsToEverything is the way back out of a narrowed
// stream without a client having to enumerate every kind this build knows
// about, several of which (like the test-sentinel above) it may not.
func TestSubscribeWildcardResetsToEverything(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Subscribe(c, []string{"activity"})
	h.Subscribe(c, []string{"*"})
	h.Broadcast("task", "a")
	h.Broadcast("queue", "b")

	for _, want := range []string{"task", "queue"} {
		if got := recv(t, c, "a wildcard resubscribe still filtered a kind"); !bytes.Contains(got, []byte(want)) {
			t.Errorf("got %s, want %q", got, want)
		}
	}
}

// TestUnsubscribeRemovesOnlyNamedKinds narrows a connection with Subscribe
// and then trims it further with Unsubscribe, leaving the rest of the
// allowlist in force.
func TestUnsubscribeRemovesOnlyNamedKinds(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Subscribe(c, []string{"task", "activity"})
	h.Unsubscribe(c, []string{"task"})
	h.Broadcast("task", "dropped")
	h.Broadcast("activity", "kept")

	got := recv(t, c, "the still-subscribed kind never arrived")
	if !bytes.Contains(got, []byte("kept")) {
		t.Fatalf("got %s, want the activity broadcast", got)
	}
	select {
	case extra := <-c.writes:
		t.Fatalf("received a kind that was unsubscribed: %s", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestUnsubscribeOnAnUnrestrictedConnectionIsANoOp pins the documented
// limitation: there is no blocklist mode, so unsubscribing before ever
// subscribing changes nothing and the connection goes on receiving
// everything.
func TestUnsubscribeOnAnUnrestrictedConnectionIsANoOp(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Add(c)
	t.Cleanup(func() { h.Remove(c) })

	h.Unsubscribe(c, []string{"task"})
	h.Broadcast("task", "still arrives")
	got := recv(t, c, "unsubscribing from an unrestricted connection suppressed a broadcast")
	if !bytes.Contains(got, []byte("still arrives")) {
		t.Fatalf("got %s, want the task broadcast", got)
	}
}

// TestSubscribeOnAnUnregisteredConnectionIsANoOp: a call racing Remove (the
// WebSocket closed while its own read loop was mid-parse of a subscribe
// frame) must not panic or resurrect a client entry.
func TestSubscribeOnAnUnregisteredConnectionIsANoOp(t *testing.T) {
	h := New()
	c := newFakeConn()
	h.Subscribe(c, []string{"task"}) // never Added
	h.Unsubscribe(c, []string{"task"})
	if h.Len() != 0 {
		t.Fatal("Subscribe on an unregistered connection created a client entry")
	}
}

// TestSubscribeUnderConcurrency runs Subscribe, Unsubscribe and Broadcast
// from many goroutines at once against the same connections: the pattern
// -race exists to catch, matched to TestAddRemoveUnderConcurrency just above
// for the same reason. This package's own history is two real races that
// only ever reproduced in CI (see the Wave 8 commits this comment's sibling
// tests already point at).
func TestSubscribeUnderConcurrency(t *testing.T) {
	h := New()
	const conns = 8
	cs := make([]*fakeConn, conns)
	for i := range cs {
		cs[i] = newFakeConn()
		h.Add(cs[i])
	}
	t.Cleanup(func() {
		for _, c := range cs {
			h.Remove(c)
		}
	})

	var wg sync.WaitGroup
	kinds := []string{"task", "queue", "activity", "*"}
	for _, c := range cs {
		wg.Add(1)
		go func(c *fakeConn) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.Subscribe(c, []string{kinds[i%len(kinds)]})
				h.Unsubscribe(c, []string{kinds[(i+1)%len(kinds)]})
			}
		}(c)
	}
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.Broadcast(kinds[i%len(kinds)], i)
		}(i)
	}
	wg.Wait()
}
