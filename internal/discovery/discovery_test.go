package discovery

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// TestTwoInstancesFindEachOther runs two Services over real sockets. It skips
// on a host without multicast, where discovery is a no-op by design.
func TestTwoInstancesFindEachOther(t *testing.T) {
	testenv.RequireWideListener(t)
	a := New(Peer{ID: peerID("a"), Name: "Cellar", URL: "http://192.168.1.10:8749", Deployment: "container"})
	b := New(Peer{ID: peerID("b"), Name: "Laptop", URL: "http://192.168.1.11:8749", Deployment: "desktop"})
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

	gotB := only(seenByA, peerID("b"))
	if len(gotB) != 1 || gotB[0].Name != "Laptop" || gotB[0].Deployment != "desktop" {
		t.Errorf("A sees %+v, want the other instance with its announced name and deployment", seenByA)
	}
	gotA := only(seenByB, peerID("a"))
	if len(gotA) != 1 || gotA[0].URL != "http://192.168.1.10:8749" {
		t.Errorf("B sees %+v, want A with its announced address", seenByB)
	}
}

// TestAnInstanceNeverSeesItself covers multicast looping a datagram back to
// its sender.
func TestAnInstanceNeverSeesItself(t *testing.T) {
	testenv.RequireWideListener(t)
	s := New(Peer{ID: peerID("solo"), Name: "Solo", URL: "http://192.168.1.12:8749"})
	s.Start()
	defer s.Close()
	if !s.listening() {
		t.Skip("this host cannot join a multicast group")
	}
	// Long enough for several of its own announces to loop back.
	time.Sleep(2 * time.Second)
	if self := only(s.Peers(), peerID("solo")); len(self) != 0 {
		t.Errorf("got %+v, want none; an instance is not its own peer", self)
	}
}

func TestAPeerExpires(t *testing.T) {
	s := New(Peer{ID: peerID("watcher"), Name: "Watcher", URL: "http://192.168.1.13:8749"})
	s.peers[peerID("gone")] = Peer{ID: peerID("gone"), Name: "Gone", URL: "http://x", LastSeen: time.Now().Add(-peerTTL - time.Second)}
	s.peers[peerID("here")] = Peer{ID: peerID("here"), Name: "Here", URL: "http://y", LastSeen: time.Now()}

	got := s.Peers()
	if len(got) != 1 || got[0].ID != peerID("here") {
		t.Fatalf("got %+v, want only the peer still announcing", got)
	}
	if _, still := s.peers[peerID("gone")]; still {
		t.Error("the expired peer is still in the map; Peers must drop it, not just hide it")
	}
}

// TestCloseWithoutStartReturns checks that Close does not wait forever on a
// channel only Start arranges to close, which would hang shutdown silently.
func TestCloseWithoutStartReturns(t *testing.T) {
	s := New(Peer{ID: peerID("never-started"), Name: "Unstarted"})

	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close on a Service that was never started did not return; shutdown would hang here")
	}

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

// only keeps the peers a test is about. The tests use the real multicast
// group, so other instances on the network show up too.
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

// peerID turns a readable label into an id of the shape instances mint, the
// only one absorb keeps.
func peerID(label string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(label)))
}

func TestAnAnnounceUnderAnIDOfAnotherShapeIsIgnored(t *testing.T) {
	s := New(Peer{ID: peerID("self"), URL: "http://192.168.1.2:8749"})
	s.SetGroup(func(Peer) string { return "tag" }, func(Peer) bool { return true })

	for _, id := range []string{"nas", strings.ToUpper(peerID("nas")), peerID("nas") + "0"} {
		s.absorb(Peer{ID: id, URL: "http://192.168.1.66:8749", Tag: "tag"})
	}
	if got := append(s.Members(), s.Peers()...); len(got) != 0 {
		t.Fatalf("kept %+v, want no announce whose id is not shaped like an InstanceID", got)
	}
}

// TestAFloodCannotGrowTheMapWithoutBound feeds sender-chosen ids and oversized
// fields straight into absorb.
func TestAFloodCannotGrowTheMapWithoutBound(t *testing.T) {
	testenv.RequireWideListener(t)
	s := New(Peer{ID: peerID("victim"), Name: "Victim", URL: "http://192.168.1.30:8749"})
	s.Start()
	defer s.Close()
	if !s.listening() {
		t.Skip("this host cannot join a multicast group")
	}

	huge := strings.Repeat("A", 4000)
	for i := 0; i < 2000; i++ {
		s.absorb(Peer{ID: peerID(fmt.Sprintf("flood-%d", i)), Name: huge, URL: huge, Deployment: huge})
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
		t.Errorf("a retained name is %d bytes, want at most %d; announced strings are attacker-controlled", widest, fieldLimit)
	}
}

// TestCloseRacingStartLeavesNothingRunning runs Start and Close concurrently
// many times, under -race, since the window between Start's guard and
// publishing the connection is small.
func TestCloseRacingStartLeavesNothingRunning(t *testing.T) {
	testenv.RequireWideListener(t)
	for i := 0; i < 40; i++ {
		s := New(Peer{ID: peerID(fmt.Sprintf("race-%d", i)), Name: "Racer", URL: "http://192.168.1.40:8749"})
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

		if s.listening() && !s.isClosing() {
			t.Fatalf("iteration %d: still listening after Close", i)
		}
	}
}

func TestARenameReachesTheNetwork(t *testing.T) {
	testenv.RequireWideListener(t)
	a := New(Peer{ID: peerID("before"), Name: "Before", URL: "http://192.168.1.50:8749"})
	a.Start()
	defer a.Close()
	watcher := New(Peer{ID: peerID("watcher"), Name: "Watcher"})
	watcher.Start()
	defer watcher.Close()
	if !a.listening() || !watcher.listening() {
		t.Skip("this host cannot join a multicast group")
	}

	waitFor := func(want string) bool {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			for _, p := range only(watcher.Peers(), peerID("before")) {
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

	a.SetSelf(Peer{ID: peerID("before"), Name: "After", URL: "http://192.168.1.50:8749"})
	if !waitFor("After") {
		t.Error("the rename never reached the network; peers keep showing the old name until this process restarts")
	}
}

func TestAnAnnounceWithTheGroupTagIsAMember(t *testing.T) {
	s := New(Peer{ID: peerID("self"), URL: "http://192.168.1.2:8749"})
	tag := func(p Peer) string { return "tag:" + p.ID + ":" + p.URL }
	s.SetGroup(tag, func(p Peer) bool { return p.Tag == tag(p) })

	s.absorb(Peer{ID: peerID("member"), URL: "http://192.168.1.3:8749", Sent: 10, Tag: "tag:" + peerID("member") + ":http://192.168.1.3:8749"})
	s.absorb(Peer{ID: peerID("stranger"), URL: "http://192.168.1.4:8749"})
	// A stranger announcing the member's id with a forged tag stays a
	// stranger and leaves the member's address alone.
	s.absorb(Peer{ID: peerID("member"), URL: "http://192.168.1.66:8749", Sent: 11, Tag: "forged"})
	// An older announce played back does not move the member either.
	s.absorb(Peer{ID: peerID("member"), URL: "http://192.168.1.5:8749", Sent: 9, Tag: "tag:" + peerID("member") + ":http://192.168.1.5:8749"})

	members := s.Members()
	if len(members) != 1 || members[0].URL != "http://192.168.1.3:8749" {
		t.Fatalf("members = %+v, want the tagged announce at its own address", members)
	}
	if got := s.Peers(); len(got) != 2 {
		t.Fatalf("peers = %+v, want the stranger and the forged announce", got)
	}

	s.SetGroup(nil, nil)
	if got := s.Members(); len(got) != 0 {
		t.Fatalf("members after leaving = %+v, want none", got)
	}
}

func TestOnlyAMemberKeepsItsAddress(t *testing.T) {
	s := New(Peer{ID: peerID("self"), URL: "http://192.168.1.2:8749"})
	tag := func(p Peer) string { return "tag:" + p.ID + ":" + p.Address }
	s.SetGroup(tag, func(p Peer) bool { return p.Tag == tag(p) })

	s.absorb(Peer{ID: peerID("member"), URL: "http://192.168.1.3:8749", Address: "https://kl.example.org", Sent: 10, Tag: "tag:" + peerID("member") + ":https://kl.example.org"})
	s.absorb(Peer{ID: peerID("stranger"), URL: "http://192.168.1.4:8749", Address: "https://elsewhere.example.org"})
	long := "https://" + strings.Repeat("a", addressLimit)
	s.absorb(Peer{ID: peerID("long"), URL: "http://192.168.1.5:8749", Address: long, Sent: 10, Tag: "tag:" + peerID("long") + ":" + long})

	byID := map[string]Peer{}
	for _, p := range append(s.Members(), s.Peers()...) {
		byID[p.ID] = p
	}
	if got := byID[peerID("member")].Address; got != "https://kl.example.org" {
		t.Errorf("the member's address = %q", got)
	}
	if got := byID[peerID("stranger")].Address; got != "" {
		t.Errorf("a stranger's address was kept: %q", got)
	}
	if p, ok := byID[peerID("long")]; !ok || p.Address != "" {
		t.Errorf("a member with a long address = %+v, want it listed without the address", p)
	}
}
