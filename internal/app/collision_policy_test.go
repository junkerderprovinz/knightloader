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

// elsewhereResolver stands for backends that fetch outside this process (JD,
// TorBox, yt-dlp) and name the file themselves.
type elsewhereResolver struct{}

func (elsewhereResolver) Info() resolver.Info { return resolver.Info{ID: "elsewhere", Prio: 90} }

func (elsewhereResolver) Match(raw string) bool { return strings.Contains(raw, "elsewhere.example") }

func (elsewhereResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// delegated wires a task's resolver to such a backend and returns the folder
// its downloads should land in.
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

// Only the engine can be told a file name, and the app says so, so the
// interface offers the setting only where it works.
func TestOnlyTheEngineIsHeldToTheCollisionPolicy(t *testing.T) {
	a, _, _ := delegated(t, collide.Rename)

	if !a.HonoursCollisionPolicy("direct") {
		t.Error("the embedded engine is the one backend that can be told a name, and it reports that it cannot")
	}
	if a.HonoursCollisionPolicy("elsewhere") {
		t.Error("a backend that fetches in another process claims to honour the collision policy")
	}
}

// No name is reserved for a delegated backend, which would never use it and
// leave an empty placeholder file behind.
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

// Skip still applies to a delegated task: it refuses to start before the
// handover and needs nothing from the backend.
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
