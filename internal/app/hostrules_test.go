package app

// The per-host table, read where the dispatcher actually decides: the
// simultaneous-download ceiling in dispatchLocked, and the connection count in
// connsFor. settings' own tests pin what a pattern matches and how the values
// resolve; this file pins that the answer reaches the queue.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	looseHost  = "loose.example"  // an entry of its own, three at a time
	strictHost = "strict.example" // no entry, so the global 1 applies
)

// hostRuleApp registers one fake resolver per host and one backend behind both,
// so a dispatch pass ends in a channel rather than on somebody's server.
func hostRuleApp(t *testing.T, mutate func(*settings.Settings)) (*App, *capBackend) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent = 8
	s.MaxPerHost = 1
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	mutate(&s)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be := &capBackend{got: make(chan int, 16)}
	a.bmu.Lock()
	a.debrid[looseResolver] = be
	a.debrid[strictResolver] = be
	a.bmu.Unlock()
	// One resolver id per host, because Registry.Register REPLACES an existing
	// entry with the same id: registering both hosts under one id would leave
	// the first host matched by nothing but resolver.Direct, and the test would
	// quietly be making real requests to loose.example instead of driving the
	// fake backend.
	a.Registry.Register(hostResolver{id: looseResolver, host: looseHost})
	a.Registry.Register(hostResolver{id: strictResolver, host: strictHost})
	return a, be
}

const (
	looseResolver  = "hosted-loose"
	strictResolver = "hosted-strict"
)

func queueOn(a *App, host, resolverID, id string) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + host + "/" + id + ".bin", Name: id + ".bin",
		Resolver: resolverID, Status: core.StatusQueued, Enabled: true,
	}
	a.queue = append(a.queue, id)
	a.mu.Unlock()
}

// TestPerHostLimitBeatsTheGlobalOneForThatHostAlone is the whole of point 3's
// first half. One number for every hoster on the internet means the only safe
// value is the strictest host's, which throttles every other download on the
// box; the table has to lift THIS host without lifting any other.
//
// Two hosts on purpose. With one, "the table was read" and "the global was
// raised" are indistinguishable, and a test that cannot tell them apart would
// pass on either.
func TestPerHostLimitBeatsTheGlobalOneForThatHostAlone(t *testing.T) {
	a, _ := hostRuleApp(t, func(s *settings.Settings) {
		s.HostRules = map[string]settings.HostRule{looseHost: {MaxPerHost: 3}}
	})
	for _, id := range []string{"l1", "l2", "l3"} {
		queueOn(a, looseHost, looseResolver, id)
	}
	for _, id := range []string{"s1", "s2"} {
		queueOn(a, strictHost, strictResolver, id)
	}

	a.mu.Lock()
	a.dispatchLocked()
	running := map[string]bool{}
	for id := range a.active {
		running[id] = true
	}
	waiting := a.tasks["s2"].Waiting
	a.mu.Unlock()

	for _, id := range []string{"l1", "l2", "l3"} {
		if !running[id] {
			t.Errorf("%s is not running; the host's own ceiling of 3 was not read", id)
		}
	}
	if !running["s1"] {
		t.Error("s1 is not running; the host with no entry lost its global slot")
	}
	if running["s2"] {
		t.Error("s2 is running; a host with no entry must keep the global ceiling of 1")
	}
	if waiting != core.WaitingHost {
		t.Errorf("s2 waits with %q, want %q - it is this host's limit and not the global one", waiting, core.WaitingHost)
	}
}

// TestPerHostLimitCanAlsoBeLowerThanTheGlobal is the other direction, and it is
// the one people will actually reach for: a hoster that blocks from two
// connections, on an instance whose global limit is comfortable.
func TestPerHostLimitCanAlsoBeLowerThanTheGlobal(t *testing.T) {
	a, _ := hostRuleApp(t, func(s *settings.Settings) {
		s.MaxPerHost = 4
		s.HostRules = map[string]settings.HostRule{strictHost: {MaxPerHost: 1}}
	})
	for _, id := range []string{"s1", "s2"} {
		queueOn(a, strictHost, strictResolver, id)
	}

	a.mu.Lock()
	a.dispatchLocked()
	second := a.active["s2"]
	waiting := a.tasks["s2"].Waiting
	a.mu.Unlock()

	if second {
		t.Error("a second transfer started on a host the table limits to one")
	}
	if waiting != core.WaitingHost {
		t.Errorf("s2 waits with %q, want %q", waiting, core.WaitingHost)
	}
}

// TestPerHostChunksOutrankTheGlobalCount is point 3's second half, and the
// assertion is deliberately a number LARGER than the global: the table is a
// value in connsFor's precedence chain, not one more ceiling. As a ceiling it
// could only ever say "at most", which cannot express the case it was asked for
// - one hoster tolerating eight connections while the box's own default is
// four.
func TestPerHostChunksOutrankTheGlobalCount(t *testing.T) {
	a, be := hostRuleApp(t, func(s *settings.Settings) {
		s.MaxPerHost = 4
		s.Chunks = 4
		s.HostRules = map[string]settings.HostRule{looseHost: {Chunks: 8}}
	})
	queueOn(a, looseHost, looseResolver, "l1")
	queueOn(a, strictHost, strictResolver, "s1")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	got := map[int]int{}
	for i := 0; i < 2; i++ {
		select {
		case n := <-be.got:
			got[n]++
		case <-time.After(2 * time.Second):
			t.Fatal("the backend was handed fewer than two downloads")
		}
	}
	if got[8] != 1 {
		t.Errorf("no download opened the host table's 8 connections (saw %v)", got)
	}
	if got[4] != 1 {
		t.Errorf("the host with no entry did not open the global 4 (saw %v)", got)
	}
}

// TestPerHostChunksStillPassThroughEveryCeiling: the table says what the person
// wants, and a resolver that KNOWS the host permits two still wins. Otherwise
// the entry would be an override of a fact rather than of a preference, and a
// hopeful eight would be sent to a host that bans multi-connection downloads.
func TestPerHostChunksStillPassThroughEveryCeiling(t *testing.T) {
	cfg := settings.Settings{
		Chunks:    4,
		HostRules: map[string]settings.HostRule{looseHost: {Chunks: 8}},
	}
	task := &core.Task{URL: "https://" + looseHost + "/f.bin"}
	if got := connsFor(task, cfg); got != 8 {
		t.Errorf("connsFor = %d, want the table's 8", got)
	}
	if got := connsFor(task, cfg, 2); got != 2 {
		t.Errorf("connsFor with a resolver ceiling of 2 = %d, want 2", got)
	}
	// And a count typed on the row itself still outranks the table, the same way
	// it outranks the global setting: the person editing one link is being more
	// specific than the person who filled in the host table.
	task.Chunks = 3
	if got := connsFor(task, cfg); got != 3 {
		t.Errorf("connsFor = %d, want the 3 typed on the task", got)
	}
}

// TestAnEmptyHostTableChangesNothing is the guarantee that lets this ship
// switched on: an install that never writes a row must dispatch exactly as it
// did before the table existed.
func TestAnEmptyHostTableChangesNothing(t *testing.T) {
	cfg := settings.Settings{MaxPerHost: 2, Chunks: 6}
	if got := maxPerHostFor(cfg, looseHost); got != 2 {
		t.Errorf("maxPerHostFor = %d, want the global 2", got)
	}
	if got := connsFor(&core.Task{URL: "https://" + looseHost + "/f.bin"}, cfg); got != 6 {
		t.Errorf("connsFor = %d, want the global 6", got)
	}
}
