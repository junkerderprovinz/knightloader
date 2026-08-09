package app

// The per-host connection ceiling, at the dispatch level: proving hostCapFor
// actually reaches connsFor's ceilings list from a real dispatchLocked pass,
// and that it behaves as ONE MORE CEILING joining the existing clamp chain -
// it can only lower the connection count, never raise it past whatever the
// task, the rule or the global setting already decided - rather than a
// second, competing limit of its own. connsFor's own table test
// (chunks_test.go) already pins the generic multi-ceiling arithmetic; this
// file pins that THIS source of a ceiling is actually wired into it.

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// capBackend is a backend fake that captures how many connections it was
// actually asked to open - the one number this whole file is about.
type capBackend struct{ got chan int }

func (b *capBackend) Download(_, _ string, _ map[string]string, conns int) { b.got <- conns }
func (b *capBackend) Pause(string)                                         {}
func (b *capBackend) Resume(string)                                        {}
func (b *capBackend) Remove(string, bool)                                  {}

// hostCapResolver matches one fixed host and answers cap for
// resolver.HostCapper - a minimal stand-in for debrid.Resolver.HostCap
// without needing a real Real-Debrid round trip to prove the wiring.
type hostCapResolver struct {
	id   string
	host string
	cap  int
}

func (r hostCapResolver) Info() resolver.Info   { return resolver.Info{ID: r.id, Prio: 90} }
func (r hostCapResolver) Match(raw string) bool { return raw == "https://"+r.host+"/f.bin" }
func (r hostCapResolver) HostCap(host string) int {
	if host != r.host {
		return 0
	}
	return r.cap
}
func (hostCapResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// capApp wires a hostCapResolver + capBackend pair, staged the same direct
// way collision_policy_test.go's dispatchOne does: a task written straight
// into a.tasks/a.queue and dispatched under a.mu, which is what lets this
// test drive dispatchLocked without a network-facing crawler or a real
// debrid account.
func capApp(t *testing.T, globalChunks, cap int) (*App, *capBackend, string) {
	t.Helper()
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(), Chunks: globalChunks,
	}); err != nil {
		t.Fatal(err)
	}
	const host = "capped.example"
	be := &capBackend{got: make(chan int, 4)}
	a.bmu.Lock()
	a.debrid["capped"] = be
	a.bmu.Unlock()
	a.Registry.Register(hostCapResolver{id: "capped", host: host, cap: cap})
	return a, be, host
}

func dispatchCapTask(a *App, url string) {
	task := &core.Task{
		ID: "cap-task", URL: url, Name: "f.bin", Resolver: "capped",
		Status: core.StatusQueued, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.queue = append(a.queue, task.ID)
	a.dispatchLocked()
	a.mu.Unlock()
}

// TestHostCapLowersTheConnectionCount is the ceiling half: a host cap smaller
// than the global setting must win, exactly as result.Connections already
// does (connsFor's own doc comment: "the per-host ... caps are the same kind
// of fact and arrive the same way, as one more ceiling").
func TestHostCapLowersTheConnectionCount(t *testing.T) {
	a, be, host := capApp(t, 12, 3)
	dispatchCapTask(a, "https://"+host+"/f.bin")
	select {
	case got := <-be.got:
		if got != 3 {
			t.Errorf("conns = %d, want the host cap's 3 to win over the global 12", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received the download")
	}
}

// TestHostCapNeverRaisesTheConnectionCount is the JOINS-THE-CLAMP half: a
// host cap LARGER than what the task/global setting already decided must not
// raise the count - if it did, this would be a second competing limit
// instead of one more ceiling in the same chain, and connsFor's own "a
// ceiling can only lower the count" contract would be broken from the one
// call site meant to prove it.
func TestHostCapNeverRaisesTheConnectionCount(t *testing.T) {
	a, be, host := capApp(t, 2, 99)
	dispatchCapTask(a, "https://"+host+"/f.bin")
	select {
	case got := <-be.got:
		if got != 2 {
			t.Errorf("conns = %d, want the global 2 unraised by a larger host cap of 99", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received the download")
	}
}

// TestHostCapZeroIsNoOpinion pins the other edge every ceiling in connsFor's
// chain shares: 0 must read as "nothing to say about this host", not as "no
// connections" - the same contract a resolver with Connections unset already
// carries.
func TestHostCapZeroIsNoOpinion(t *testing.T) {
	a, be, host := capApp(t, 5, 0)
	dispatchCapTask(a, "https://"+host+"/f.bin")
	select {
	case got := <-be.got:
		if got != 5 {
			t.Errorf("conns = %d, want the global 5 untouched by a zero host cap", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received the download")
	}
}

// TestHostCapForIgnoresAResolverWithNoOpinion pins hostCapFor itself: a
// resolver that does not implement resolver.HostCapper at all - every
// resolver in this tree except debrid.Resolver over a HostLimiter-backed
// service - must answer 0, never a guess.
func TestHostCapForIgnoresAResolverWithNoOpinion(t *testing.T) {
	if got := hostCapFor(elsewhereResolver{}, "anyhost.example"); got != 0 {
		t.Errorf("hostCapFor(elsewhereResolver{}, ...) = %d, want 0 (it does not implement resolver.HostCapper at all)", got)
	}
}
