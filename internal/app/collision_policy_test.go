package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// elsewhereResolver stands for every backend that fetches somewhere this process
// cannot reach into: headless JD, TorBox, yt-dlp. What they have in common is
// that they name the file themselves.
type elsewhereResolver struct{}

func (elsewhereResolver) Info() resolver.Info { return resolver.Info{ID: "elsewhere", Prio: 90} }

func (elsewhereResolver) Match(raw string) bool { return strings.Contains(raw, "elsewhere.example") }

func (elsewhereResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// delegated wires a task's resolver to a backend that runs out of reach, and
// hands back the folder its downloads are supposed to land in.
func delegated(t *testing.T, policy collide.Policy) (*App, *stubBackend, string) {
	t.Helper()
	dir := t.TempDir()
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: dir,
		CollisionPolicy: string(policy),
	}); err != nil {
		t.Fatal(err)
	}
	stub := &stubBackend{got: make(chan string, 4)}
	a.bmu.Lock()
	a.debrid["elsewhere"] = stub
	a.bmu.Unlock()
	a.Registry.Register(elsewhereResolver{})
	if err := os.WriteFile(filepath.Join(dir, "clash.bin"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, stub, dir
}

func dispatchOne(a *App, id, url string) *core.Task {
	task := &core.Task{
		ID: id, URL: url, Name: "clash.bin", Resolver: "elsewhere",
		Status: core.StatusQueued, Enabled: true,
	}
	a.mu.Lock()
	a.tasks[id] = task
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.mu.Unlock()
	return task
}

// The policy reaches the file only on the engine path, and the app has to be
// able to say so. If this fails, the interface shows a destination control on a
// row it cannot govern: the user sets "rename", watches the file get overwritten
// anyway, and stops believing the setting on the rows where it does work.
func TestOnlyTheEngineIsHeldToTheCollisionPolicy(t *testing.T) {
	a, _, _ := delegated(t, collide.Rename)

	if !a.HonoursCollisionPolicy("direct") {
		t.Error("the embedded engine is the one backend that can be told a name, and it reports that it cannot")
	}
	if a.HonoursCollisionPolicy("elsewhere") {
		t.Error("a backend that fetches in another process claims to honour the collision policy")
	}
}

// A delegated backend is handed a task and nothing else. If this fails, the
// dispatcher has reserved a name on behalf of a downloader that will never use
// it: an empty "clash (2).bin" is left in the folder for good, the real download
// lands beside it under whatever name the other process chose, and the next
// attempt at the same link renames itself out of the way of our own litter.
func TestADelegatedBackendIsNeverHandedAReservedName(t *testing.T) {
	a, stub, dir := delegated(t, collide.Rename)

	dispatchOne(a, "d1", "https://elsewhere.example/clash.bin")
	collect(t, stub.got, 1)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "clash.bin" {
			t.Fatalf("%q was reserved for a backend that cannot be told a name", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("the folder holds %d entries, want only the file that was already there", len(entries))
	}
}

// Skip is the other half of the same rule, and it is the reason the rule is not
// simply "delegated backends get no policy at all". Skip is a refusal to start,
// decided before the handover, so it asks nothing of whoever would have fetched
// the bytes. If this fails, choosing skip quietly downloads over finished files
// for every link that does not go through the embedded engine.
func TestSkipStillSettlesADelegatedTask(t *testing.T) {
	a, stub, dir := delegated(t, collide.Skip)

	task := dispatchOne(a, "d2", "https://elsewhere.example/clash.bin")
	expectNone(t, stub.got)

	a.mu.Lock()
	status, msg := task.Status, task.Error
	a.mu.Unlock()
	if status != core.StatusError {
		t.Fatalf("status = %q, want the task settled rather than started", status)
	}
	if !strings.Contains(msg, filepath.Join(dir, "clash.bin")) {
		t.Fatalf("error %q does not name the file that is in the way", msg)
	}
}
