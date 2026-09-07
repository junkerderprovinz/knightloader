package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// The three wiring lines this batch could not set itself, each guarded where it
// would otherwise fall out silently. Three features were delivered whole and
// inert today at file boundaries (the cookie jars, the waiting reason, the
// header profiles), which is enough repetition to stop writing comments about
// it and start writing tests.

// TestACategoryDecidesTheFolder guards the ladder in dirFor. Without the
// category branch the setting round-trips, the API accepts it, and every
// download lands in the instance-wide folder anyway - a drawer that sorts
// nothing.
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

	// A folder on the task still wins: a rule looked at this link, a category
	// is a label on a batch.
	own := t.TempDir()
	task.Dir = own
	if got := a.dirFor(task); got != own {
		t.Fatalf("dirFor = %q when the task names its own folder, want %q", got, own)
	}
}

// TestTheCategoryAndExtractFolderSurviveARestart guards the two store columns.
// Both are decisions somebody made, not readings from a running transfer, so
// unlike Speed they have to come back - a category that evaporates overnight
// sends the next morning's downloads somewhere else with nothing on screen
// changing.
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

// TestQuietModeReachesTheSlotCount guards the one line in dispatchLocked.
// Without it quiet mode moves the speed and leaves the slots alone, which is
// half a turtle button and the half nobody notices is missing.
//
// It goes THROUGH the dispatcher rather than calling cfgInForceLocked directly.
// The first version of this test did the latter, and it stayed green with the
// wiring removed - a test that cannot reach the gap reports nothing. What it
// reads is the waiting reason: a task turned down for want of a slot says so,
// which is exactly the observation the dispatcher makes with the config it
// actually used.
func TestQuietModeReachesTheSlotCount(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	// Both halves matter: eight slots normally, one under quiet. A zero in the
	// quiet limits means "no opinion" and would leave the eight standing, which
	// is what made the first attempt at this test pass against nothing.
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
