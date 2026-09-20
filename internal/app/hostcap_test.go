package app

// The per-host connection cap reaches connsFor from a real dispatch pass, as
// one more ceiling that can lower the count but never raise it. The arithmetic
// itself is tested in chunks_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// capBackend records how many connections it was asked to open.
type capBackend struct{ got chan int }

func (b *capBackend) Download(_, _ string, _ map[string]string, conns int) { b.got <- conns }
func (b *capBackend) Pause(string)                                         {}
func (b *capBackend) Resume(string)                                        {}
func (b *capBackend) Remove(string, bool)                                  {}

// hostCapResolver matches one fixed host and answers cap as a
// resolver.HostCapper, standing in for debrid.Resolver.HostCap.
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

// capApp wires a hostCapResolver and a capBackend. Tasks are written straight
// into the queue and dispatched under a.mu, as in collision_policy_test.go.
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

// A host cap below the global setting wins.
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

// A host cap above the setting does not raise the count.
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

// A host cap of 0 means no opinion, not no connections.
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

// A resolver that is not a resolver.HostCapper answers 0.
func TestHostCapForIgnoresAResolverWithNoOpinion(t *testing.T) {
	if got := hostCapFor(elsewhereResolver{}, "anyhost.example"); got != 0 {
		t.Errorf("hostCapFor(elsewhereResolver{}, ...) = %d, want 0 (it does not implement resolver.HostCapper at all)", got)
	}
}
