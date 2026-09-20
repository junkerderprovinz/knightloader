package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// Wiring across file boundaries that would otherwise break without a sound.

// A category's folder applies in dirFor.
func TestACategoryDecidesTheFolder(t *testing.T) {
	a := newAccountsTestApp(t)

	s := a.Settings.Get()
	s.DownloadDir = t.TempDir()
	s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Dir: t.TempDir()}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	want := a.Settings.Get().Categories[0].Dir

	task := &core.Task{ID: "c", URL: "https://host.example/a.mkv", Name: "a.mkv", Category: "serien"}
	if got := a.dirFor(task); got != want {
		t.Fatalf("dirFor = %q for a task in the Serien drawer, want the drawer's own folder %q", got, want)
	}

	// The task's own folder still wins.
	own := t.TempDir()
	task.Dir = own
	if got := a.dirFor(task); got != own {
		t.Fatalf("dirFor = %q when the task names its own folder, want %q", got, own)
	}
}

// Category and extract folder are settings, not transfer readings, so the
// store must keep them.
func TestTheCategoryAndExtractFolderSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/kl.db")
	if err != nil {
		t.Fatal(err)
	}
	want := core.Task{ID: "k", URL: "https://host.example/x.rar", Name: "x.rar",
		Category: "filme", ExtractDir: `D:\ziel\filme`}
	if err := st.Save(&want); err != nil {
		t.Fatal(err)
	}
	st.Close()

	again, err := store.Open(dir + "/kl.db")
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d tasks back, want 1", len(all))
	}
	if all[0].Category != want.Category {
		t.Errorf("Category = %q after a restart, want %q", all[0].Category, want.Category)
	}
	if all[0].ExtractDir != want.ExtractDir {
		t.Errorf("ExtractDir = %q after a restart, want %q", all[0].ExtractDir, want.ExtractDir)
	}
}

// Quiet mode limits the slot count as well as the speed. The test goes through
// the dispatcher and reads the waiting reason, since calling cfgInForceLocked
// directly would pass even if dispatchLocked ignored it.
func TestQuietModeReachesTheSlotCount(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	// Eight slots normally, one under quiet; a zero quiet limit would mean no
	// opinion.
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 8, MaxPerHost: 8, DownloadDir: t.TempDir(),
		Quiet: settings.QuietLimits{MaxConcurrent: 1},
	}); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	a.tasks["r1"] = &core.Task{ID: "r1", URL: "https://host.example/r1", Status: core.StatusRunning, Enabled: true}
	a.active["r1"] = true
	a.started["r1"] = true
	for _, id := range []string{"w1", "w2"} {
		a.tasks[id] = &core.Task{ID: id, URL: "https://host.example/" + id, Status: core.StatusQueued, Enabled: true}
		a.queue = append(a.queue, id)
	}
	a.dispatchLocked()
	lautGrund := a.tasks["w1"].Waiting
	a.mu.Unlock()

	if lautGrund == core.WaitingSlot {
		t.Fatalf("a queued task waits for a slot with one of eight in use; this test cannot see quiet mode from here")
	}

	// Fresh queue: the first pass handed the two out, so put two more in.
	a.SetQuiet(true)
	a.mu.Lock()
	for _, id := range []string{"q1", "q2"} {
		a.tasks[id] = &core.Task{ID: id, URL: "https://host.example/" + id, Status: core.StatusQueued, Enabled: true}
		a.queue = append(a.queue, id)
	}
	a.dispatchLocked()
	leiseGrund := a.tasks["q1"].Waiting
	a.mu.Unlock()

	if leiseGrund != core.WaitingSlot {
		t.Fatalf("waiting reason = %q under quiet mode with one slot and transfers already running, want %q; the dispatcher is reading the saved settings instead of the ones in force",
			leiseGrund, core.WaitingSlot)
	}
}
