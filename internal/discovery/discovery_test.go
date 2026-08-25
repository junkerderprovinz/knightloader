package discovery

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTwoInstancesFindEachOther is the whole point of the package, over real
// sockets: two Services on one host announce and see each other, with nothing
// configured between them.
//
// Skipped rather than failed when the host cannot join a multicast group -
// a CI container without multicast is a real, ordinary environment, and the
// package's own contract is that discovery failing costs discovery and
// nothing else.
func TestTwoInstancesFindEachOther(t *testing.T) {
	a := New(Peer{ID: "id-a", Name: "Cellar", URL: "http://192.168.1.10:8749", Deployment: "container"})
	b := New(Peer{ID: "id-b", Name: "Laptop", URL: "http://192.168.1.11:8749", Deployment: "desktop"})
	a.Start()
	b.Start()
	defer a.Close()
	defer b.Close()

	if a.conn == nil || b.conn == nil {
		t.Skip("this host cannot join a multicast group; discovery is a no-op here by design")
	}

	deadline := time.Now().Add(10 * time.Second)
	var seenByA, seenByB []Peer
	for time.Now().Before(deadline) {
		seenByA, seenByB = a.Peers(), b.Peers()
		if len(seenByA) > 0 && len(seenByB) > 0 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	gotB := only(seenByA, "id-b")
	if len(gotB) != 1 || gotB[0].Name != "Laptop" || gotB[0].Deployment != "desktop" {
		t.Errorf("A sees %+v, want the other instance with its announced name and deployment", seenByA)
	}
	gotA := only(seenByB, "id-a")
	if len(gotA) != 1 || gotA[0].URL != "http://192.168.1.10:8749" {
		t.Errorf("B sees %+v, want A with its announced address", seenByB)
	}
}

// TestAnInstanceNeverSeesItself: multicast loops a datagram back to the
// sender by default, so without an explicit check every instance would list
// itself as a peer - and then offer to pair with itself.
func TestAnInstanceNeverSeesItself(t *testing.T) {
	s := New(Peer{ID: "id-solo", Name: "Solo", URL: "http://192.168.1.12:8749"})
	s.Start()
	defer s.Close()
	if !s.listening() {
		t.Skip("this host cannot join a multicast group")
	}
	// Long enough for several of its own announces to have looped back.
	time.Sleep(2 * time.Second)
	// Its OWN id, specifically - not "the list is empty", which is a claim
	// about the whole network rather than about this instance.
	if self := only(s.Peers(), "id-solo"); len(self) != 0 {
		t.Errorf("got %+v, want none - an instance is not its own peer", self)
	}
}

// TestAPeerExpires: peers are ephemeral. An instance that stops announcing
// has to leave the list on its own, or a machine that was switched off weeks
// ago stays offered forever.
func TestAPeerExpires(t *testing.T) {
	s := New(Peer{ID: "id-watcher", Name: "Watcher", URL: "http://192.168.1.13:8749"})
	s.peers["id-gone"] = Peer{ID: "id-gone", Name: "Gone", URL: "http://x", LastSeen: time.Now().Add(-peerTTL - time.Second)}
	s.peers["id-here"] = Peer{ID: "id-here", Name: "Here", URL: "http://y", LastSeen: time.Now()}

	got := s.Peers()
	if len(got) != 1 || got[0].ID != "id-here" {
		t.Fatalf("got %+v, want only the peer still announcing", got)
	}
	if _, still := s.peers["id-gone"]; still {
		t.Error("the expired peer is still in the map - Peers must drop it, not just hide it")
	}
}

// TestCloseWithoutStartReturns pins that Close on a Service that was never
// started comes back instead of blocking forever.
//
// Nothing reaches that today - startDiscovery always calls Start before the
// Service is handed to anything that could close it - but the failure mode
// makes it worth nailing down: Close waits on a channel that only Start
// arranges to have closed, so getting the order wrong once would hang the
// whole application's shutdown, with no error and nothing in a log to point
// at the cause.
//
// The timeout is what makes this a test rather than a hang: without the
// guard, this goroutine never sends and the test fails on the deadline
// instead of taking the package's whole timeout budget with it.
func TestCloseWithoutStartReturns(t *testing.T) {
	s := New(Peer{ID: "id-never-started", Name: "Unstarted"})

	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close on a Service that was never started did not return - shutdown would hang here")
	}

	// And again, because Close documents itself as safe to repeat.
	done2 := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(3 * time.Second):
		t.Fatal("second Close did not return")
	}
}

// only keeps the peers this test is about. These tests use the real multicast
// group, so anything else announcing on the network - another developer's
// instance, a leftover process, a concurrent run of this same package - lands
// in the list too. Asserting on the raw list makes the test fail for reasons
// that have nothing to do with the code, which is the fastest way to teach
// people to ignore it.
func only(peers []Peer, ids ...string) []Peer {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := []Peer{}
	for _, p := range peers {
		if want[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// TestAFloodCannotGrowTheMapWithoutBound: the peer map is keyed by an id the
// SENDER chooses, and pruning only ever happened on read - from GET
// /api/discovery, an authenticated route a headless instance may never see.
// So anything on the network could grow this map for as long as it liked,
// with several kilobytes per entry, and nothing would ever sweep it.
func TestAFloodCannotGrowTheMapWithoutBound(t *testing.T) {
	s := New(Peer{ID: "id-victim", Name: "Victim", URL: "http://192.168.1.30:8749"})
	s.Start()
	defer s.Close()
	if !s.listening() {
		t.Skip("this host cannot join a multicast group")
	}

	// Straight into readLoop's own path rather than over the wire: this is
	// about what the map does with announces, not about whether multicast
	// delivers them, and 2000 real datagrams would be a slow, flaky way to ask
	// the same question.
	huge := strings.Repeat("A", 4000)
	for i := 0; i < 2000; i++ {
		s.absorb(Peer{ID: fmt.Sprintf("flood-%d", i), Name: huge, URL: huge, Deployment: huge})
	}

	s.mu.Lock()
	n := len(s.peers)
	var widest int
	for _, p := range s.peers {
		if len(p.Name) > widest {
			widest = len(p.Name)
		}
	}
	s.mu.Unlock()

	if n > maxPeers {
		t.Errorf("map holds %d entries after a 2000-entry flood, want at most %d", n, maxPeers)
	}
	if widest > fieldLimit {
		t.Errorf("a retained name is %d bytes, want at most %d - announced strings are attacker-controlled", widest, fieldLimit)
	}
}

// TestCloseRacingStartLeavesNothingRunning hammers the one ordering the
// lifecycle here has to survive: Close arriving while Start is still opening
// the socket. The window is small - between Start's own guard and the moment it
// publishes the connection - so this runs the pair many times rather than once,
// and under -race, where a leaked goroutine touching a closed socket shows up.
//
// Nothing reaches this today: startDiscovery calls Start synchronously before
// the Service is handed anywhere. It is pinned because the failure is invisible
// (a socket, a group membership and a parked readLoop, all silent) and because
// the whole reason this lifecycle lives in one place is so it does not depend
// on every future caller getting the order right.
func TestCloseRacingStartLeavesNothingRunning(t *testing.T) {
	for i := 0; i < 40; i++ {
		s := New(Peer{ID: fmt.Sprintf("id-race-%d", i), Name: "Racer", URL: "http://192.168.1.40:8749"})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.Start() }()
		go func() { defer wg.Done(); _ = s.Close() }()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Start and Close deadlocked", i)
		}

		// Whatever order they landed in, the service must be shut: no live
		// connection left behind for a goroutine nobody can reach.
		if s.listening() && !s.isClosing() {
			t.Fatalf("iteration %d: still listening after Close", i)
		}
	}
}

// TestARenameReachesTheNetwork: the announce used to be marshalled once,
// before the loop, and re-sent unchanged forever. So renaming an instance left
// every other machine's "Found on your network" card showing the old name
// until the process restarted - and one click there stored the peer under a
// name that no longer existed, which then became the key every
// /api/instances/{name}/... call used.
//
// The relay announce already had this fixed (routes_settings.go calls
// applyRelay on an InstanceName change for exactly this reason); this is the
// same fix for the other announce.
func TestARenameReachesTheNetwork(t *testing.T) {
	a := New(Peer{ID: "id-before", Name: "Before", URL: "http://192.168.1.50:8749"})
	a.Start()
	defer a.Close()
	watcher := New(Peer{ID: "id-watcher", Name: "Watcher"})
	watcher.Start()
	defer watcher.Close()
	if !a.listening() || !watcher.listening() {
		t.Skip("this host cannot join a multicast group")
	}

	waitFor := func(want string) bool {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			for _, p := range only(watcher.Peers(), "id-before") {
				if p.Name == want {
					return true
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}

	if !waitFor("Before") {
		t.Fatal("the watcher never saw the original announce, so this test proves nothing")
	}

	a.SetSelf(Peer{ID: "id-before", Name: "After", URL: "http://192.168.1.50:8749"})
	if !waitFor("After") {
		t.Error("the rename never reached the network - peers keep showing the old name until this process restarts")
	}
}
