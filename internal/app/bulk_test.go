package app

import (
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// putTask files a task directly, for the cases a paste cannot produce: a
// finished download, a link the host has since taken down, a second copy of
// something that was already settled.
func putTask(t *testing.T, a *App, task core.Task) *core.Task {
	t.Helper()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	a.mu.Lock()
	if task.ID == "" {
		task.ID = a.freshIDLocked()
	}
	c := task
	a.tasks[c.ID] = &c
	a.mu.Unlock()
	return &c
}

// "Start everything" must not dispatch a disabled link.
func TestADisabledLinkIsNotDispatched(t *testing.T) {
	a := newQueueApp(t)

	off := putTask(t, a, core.Task{URL: "https://host.example/off.bin", Name: "off.bin",
		Status: core.StatusCollected, Enabled: false})
	// The control, so the test fails if the dispatcher starts nothing at all.
	on := putTask(t, a, core.Task{URL: "https://host.example/on.bin", Name: "on.bin",
		Status: core.StatusCollected, Enabled: true})

	a.StartTasks(nil) // no ids at all: "start everything"

	a.mu.Lock()
	defer a.mu.Unlock()
	queued := map[string]bool{}
	for _, id := range a.queue {
		queued[id] = true
	}
	if a.active[off.ID] {
		t.Error("a disabled link was dispatched by \"start everything\"")
	}
	// A disabled link stays in the collector; in the download list it would
	// look like a stuck queue.
	if queued[off.ID] {
		t.Error("a disabled link was moved into the download queue")
	}
	if a.tasks[off.ID].Status != core.StatusCollected {
		t.Errorf("a disabled link left the collector: status %q", a.tasks[off.ID].Status)
	}
	// The enabled link left the queue, running or settled with a reason (there
	// is no network in a test).
	if queued[on.ID] {
		t.Error("an enabled link was left sitting in the queue; the dispatcher skipped everything")
	}
}

// A forced link sorts to the front of the queue.
func TestForcedLinksSortToTheFront(t *testing.T) {
	a := newQueueApp(t)

	// The worst case for the forced link: newest, lowest priority and last in
	// the queue, so only Forced can lift it.
	old := putTask(t, a, core.Task{ID: "old", Priority: 2, Position: 0,
		CreatedAt: time.Now().Add(-time.Hour), Enabled: true})
	mid := putTask(t, a, core.Task{ID: "mid", Priority: 1, Position: 1,
		CreatedAt: time.Now().Add(-time.Minute), Enabled: true})
	forced := putTask(t, a, core.Task{ID: "forced", Priority: -2, Position: 9,
		CreatedAt: time.Now(), Enabled: true, Forced: true})

	a.mu.Lock()
	a.queue = []string{old.ID, mid.ID, forced.ID}
	a.sortQueueLocked()
	got := append([]string(nil), a.queue...)
	a.mu.Unlock()

	if got[0] != forced.ID {
		t.Errorf("queue order is %v; the forced link is not at the front", got)
	}
	// The rest keep their order.
	if got[1] != old.ID || got[2] != mid.ID {
		t.Errorf("queue order is %v; forcing one link disturbed the others", got)
	}
}

// Each cleanup class selects exactly the rows it names, since it removes them
// in bulk.
func TestCleanupClassesSelectWhatTheySay(t *testing.T) {
	a := newQueueApp(t)

	done := putTask(t, a, core.Task{URL: "https://host.example/done.bin", Name: "done.bin", Status: core.StatusDone, Enabled: true})
	gone := putTask(t, a, core.Task{URL: "https://host.example/gone.bin", Name: "gone.bin", Status: core.StatusCollected, Online: core.AvailOffline, Enabled: true})
	off := putTask(t, a, core.Task{URL: "https://host.example/off.bin", Name: "off.bin", Status: core.StatusCollected})
	// Uncheckable is not offline, so a hoster refusing a probe removes nothing.
	shy := putTask(t, a, core.Task{URL: "https://host.example/shy.bin", Name: "shy.bin", Status: core.StatusCollected, Online: core.AvailUncheckable, Enabled: true})
	// Switched off by a hoster preset rather than by anybody's hand, and out of
	// view, so it is not a disabled link.
	putTask(t, a, core.Task{URL: "https://youtube.com/watch?v=aside", Name: "aside", Status: core.StatusCollected,
		Variant: "thumbnail", VariantOff: true})

	cases := []struct {
		class CleanupClass
		want  []string
	}{
		{CleanupFinished, []string{done.ID}},
		{CleanupOffline, []string{gone.ID}},
		{CleanupDisabled, []string{off.ID}},
	}
	for _, c := range cases {
		t.Run(string(c.class), func(t *testing.T) {
			got, err := a.CleanupPreview(c.class)
			if err != nil {
				t.Fatal(err)
			}
			if !sameIDs(got, c.want) {
				t.Errorf("%s selects %v, want %v", c.class, got, c.want)
			}
		})
	}
	if _, err := a.CleanupPreview("nonsense"); err == nil {
		t.Error("an unknown cleanup class was accepted")
	}
	if len(a.Tasks()) != 5 {
		t.Errorf("a preview removed something; %s should still be there", shy.ID)
	}
}

// A settled download no longer blocks its own re-add, so duplicates are normal;
// the copy with bytes on disk survives, not whichever was added last.
func TestCleanupDuplicatesKeepsTheBestCopy(t *testing.T) {
	a := newQueueApp(t)

	older := putTask(t, a, core.Task{
		ID: "older", URL: "https://host.example/film.mkv", Name: "film.mkv", Size: 1000,
		Status: core.StatusError, Enabled: true, CreatedAt: time.Now().Add(-time.Hour),
	})
	halfway := putTask(t, a, core.Task{
		ID: "halfway", URL: "https://mirror.example/film.mkv", Name: "film.mkv", Size: 1000,
		Loaded: 400, Status: core.StatusPaused, Enabled: true, CreatedAt: time.Now(),
	})
	putTask(t, a, core.Task{
		ID: "different", URL: "https://host.example/other.mkv", Name: "other.mkv", Size: 2000,
		Status: core.StatusCollected, Enabled: true,
	})

	got, err := a.CleanupPreview(CleanupDuplicates)
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(got, []string{older.ID}) {
		t.Fatalf("duplicates selects %v, want only the copy with nothing downloaded", got)
	}
	if halfway.Loaded != 400 {
		t.Fatal("the copy with bytes on disk was selected for removal")
	}
}

// One dead volume means the rest of the set will never open, so the whole set
// is selected; a set still downloading is left alone.
func TestCleanupIncompleteArchivesTakesTheWholeSet(t *testing.T) {
	a := newQueueApp(t)

	broken := []string{}
	for _, name := range []string{"Film.part01.rar", "Film.part02.rar", "Film.part03.rar"} {
		task := core.Task{URL: "https://host.example/" + name, Name: name, Status: core.StatusDone, Enabled: true}
		if name == "Film.part03.rar" {
			task.Status = core.StatusError
			task.Online = core.AvailOffline
		}
		broken = append(broken, putTask(t, a, task).ID)
	}
	for _, name := range []string{"Show.part01.rar", "Show.part02.rar"} {
		putTask(t, a, core.Task{URL: "https://host.example/" + name, Name: name, Status: core.StatusRunning, Enabled: true})
	}

	got, err := a.CleanupPreview(CleanupIncompleteArchives)
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(got, broken) {
		t.Errorf("incomplete archives selects %v, want the whole broken set %v", got, broken)
	}
}

// A removed task leaves the mirror set too, so its link can be pasted again.
func TestBulkRemoveUnfilesTheLink(t *testing.T) {
	a := newQueueApp(t)

	created := a.AddLinks([]string{"https://host.example/one.bin"}, "Batch")
	if len(created) != 1 {
		t.Fatalf("staged %d links", len(created))
	}
	if removed := a.RemoveTasks([]string{created[0].ID}, false); len(removed) != 1 {
		t.Fatalf("removed %d tasks, want 1", len(removed))
	}
	again := a.AddLinks([]string{"https://host.example/one.bin"}, "Batch")
	if len(again) != 1 {
		t.Fatal("the link is still filed as a duplicate of a task that no longer exists")
	}
}

// Disabling is not pausing: "resume everything" puts a paused link that is
// disabled back in its place in the queue, and it stays there without starting.
func TestResumingEverythingLeavesADisabledLinkParked(t *testing.T) {
	a := newQueueApp(t)

	off := putTask(t, a, core.Task{URL: "https://host.example/off.bin", Name: "off.bin",
		Status: core.StatusPaused, Enabled: false})
	a.ResumeTasks([]string{off.ID})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active[off.ID] {
		t.Fatal("\"resume everything\" started a disabled link")
	}
	if w := a.tasks[off.ID].Waiting; w != core.WaitingDisabled {
		t.Errorf("the disabled link waits with %q, want %q", w, core.WaitingDisabled)
	}
	if !slices.Contains(a.queue, off.ID) {
		t.Error("the disabled link lost its place in the queue")
	}
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}
