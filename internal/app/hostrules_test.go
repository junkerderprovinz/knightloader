package app

// The per-host table reaches the dispatcher: the simultaneous-download ceiling
// in dispatchLocked and the connection count in connsFor. Pattern matching is
// tested in internal/settings.

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
// so a dispatch ends in a channel.
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
	// One resolver id per host: Register replaces an entry with the same id,
	// which would leave the first host to resolver.Direct and real requests.
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

// A host's own limit lifts that host alone. Two hosts, so reading the table
// can be told from raising the global limit.
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
		t.Errorf("s2 waits with %q, want %q; it is this host's limit and not the global one", waiting, core.WaitingHost)
	}
}

// A host's limit can also be lower than the global one, for a hoster that
// blocks a second connection.
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

// A host's chunk count is a value in connsFor's precedence, not a ceiling, so
// it can be larger than the global count.
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

// The table is a preference, so a resolver that knows the host permits two
// still wins.
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
	// A count on the task outranks the table, as it outranks the global one.
	task.Chunks = 3
	if got := connsFor(task, cfg); got != 3 {
		t.Errorf("connsFor = %d, want the 3 typed on the task", got)
	}
}

// An empty host table leaves the global values in place.
func TestAnEmptyHostTableChangesNothing(t *testing.T) {
	cfg := settings.Settings{MaxPerHost: 2, Chunks: 6}
	if got := maxPerHostFor(cfg, looseHost); got != 2 {
		t.Errorf("maxPerHostFor = %d, want the global 2", got)
	}
	if got := connsFor(&core.Task{URL: "https://" + looseHost + "/f.bin"}, cfg); got != 6 {
		t.Errorf("connsFor = %d, want the global 6", got)
	}
}
