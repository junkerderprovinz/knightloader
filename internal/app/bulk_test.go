package app

import (
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

// TestDisabledAndHeldLinksAreNotDispatched is what the Enabled checkbox and the
// hold action actually promise. Both are stored, both are shown, and both are
// reachable in bulk — so a dispatcher that does not consult them is a control
// that appears to work, persists across restarts, and downloads the link anyway.
// "Start all" is the case that matters: it takes every collected link, including
// the ones somebody deliberately switched off.
//
// The two are checked together because they fail together: they are the only
// fields between a queued task and the network that mean "not this one".
func TestDisabledAndHeldLinksAreNotDispatched(t *testing.T) {
	a := newQueueApp(t)

	off := putTask(t, a, core.Task{URL: "https://host.example/off.bin", Name: "off.bin",
		Status: core.StatusCollected, Enabled: false})
	held := putTask(t, a, core.Task{URL: "https://host.example/held.bin", Name: "held.bin",
		Status: core.StatusCollected, Enabled: true, Hold: true})
	// The control. Without it the test would still pass if the dispatcher simply
	// stopped starting anything at all.
	on := putTask(t, a, core.Task{URL: "https://host.example/on.bin", Name: "on.bin",
		Status: core.StatusCollected, Enabled: true})

	a.StartTasks(nil) // no ids at all: "start everything"

	a.mu.Lock()
	defer a.mu.Unlock()
	queued := map[string]bool{}
	for _, id := range a.queue {
		queued[id] = true
	}
	for _, c := range []struct {
		id, why string
	}{
		{off.ID, "a link switched off"},
		{held.ID, "a link on hold"},
	} {
		if a.active[c.id] {
			t.Errorf("%s was dispatched by \"start everything\"", c.why)
		}
		// Held in place rather than dropped: the flag is not a refusal, and the
		// link has to go on its own when it is switched back on.
		if !queued[c.id] {
			t.Errorf("%s lost its place in the queue instead of waiting there", c.why)
		}
	}
	// The enabled link was acted on: either it is running, or it was settled with
	// a reason (there is no network in a test). Either way it left the queue,
	// which the two above did not.
	if queued[on.ID] {
		t.Error("an enabled link was left sitting in the queue; the dispatcher skipped everything")
	}
}

// TestForcedLinksSortToTheFront pins the one thing the context menu entry
// promises in all 42 locales ("Force to the front"). Forced is stored and shown,
// so a queue order that ignores it is a menu entry that appears to work and
// changes nothing — and the link stays exactly where it was, behind whatever the
// user was trying to get past.
func TestForcedLinksSortToTheFront(t *testing.T) {
	a := newQueueApp(t)

	// Deliberately the worst case for the forced link: it is the newest, it has
	// the lowest priority, and it is last in the queue. Only Forced can lift it.
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
	// The rest keeps the order it had, or "force one link" would quietly reshuffle
	// everything the user arranged around it.
	if got[1] != old.ID || got[2] != mid.ID {
		t.Errorf("queue order is %v; forcing one link disturbed the others", got)
	}
}

// TestCleanupClassesSelectWhatTheySay is the confirmation dialog's contract.
// Every one of these entries removes rows in bulk, and a class that selects one
// row more than the user pictured is a class that deletes something they wanted.
func TestCleanupClassesSelectWhatTheySay(t *testing.T) {
	a := newQueueApp(t)

	done := putTask(t, a, core.Task{URL: "https://host.example/done.bin", Name: "done.bin", Status: core.StatusDone, Enabled: true})
	gone := putTask(t, a, core.Task{URL: "https://host.example/gone.bin", Name: "gone.bin", Status: core.StatusCollected, Online: core.AvailOffline, Enabled: true})
	off := putTask(t, a, core.Task{URL: "https://host.example/off.bin", Name: "off.bin", Status: core.StatusCollected})
	// Uncheckable is deliberately not offline: one hoster refusing a probe must
	// never be the reason a package disappears.
	shy := putTask(t, a, core.Task{URL: "https://host.example/shy.bin", Name: "shy.bin", Status: core.StatusCollected, Online: core.AvailUncheckable, Enabled: true})

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
	if len(a.Tasks()) != 4 {
		t.Errorf("a preview removed something; %s should still be there", shy.ID)
	}
}

// TestCleanupDuplicatesKeepsTheBestCopy is the class that can lose work. A
// finished or failed download deliberately stops blocking its own re-add, so a
// second row for the same file is normal — and the copy that is halfway
// downloaded must be the one that survives, not whichever was added last.
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

// TestCleanupIncompleteArchivesTakesTheWholeSet is what makes the class worth
// having. One dead volume means the other nine will never open, and leaving them
// is how a download folder fills with archives nobody can extract — but a set
// that is merely still downloading must be left alone.
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

// TestBulkRemoveUnfilesTheLink is the trap in doing this without going through
// Remove: a task taken out of the map but left in the mirror set goes on
// refusing its own link for the life of the process, and re-pasting it does
// nothing at all with no message anywhere.
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

// TestHoldIsNotPaused pins the ruling that keeps "resume everything" honest: a
// parked link is parked because somebody parked it, and the one button that
// starts everything must not undo that.
func TestHoldIsNotPaused(t *testing.T) {
	a := newQueueApp(t)

	created := a.AddLinks([]string{"https://host.example/one.bin"}, "Batch")
	if len(created) != 1 {
		t.Fatalf("staged %d links", len(created))
	}
	a.SetHold([]string{created[0].ID}, true)

	a.mu.Lock()
	task := a.tasks[created[0].ID]
	held, status := task.Hold, task.Status
	a.mu.Unlock()
	if !held {
		t.Error("the hold was not recorded")
	}
	if status == core.StatusPaused {
		t.Error("holding a link paused it; resumeAll would then start exactly the links somebody parked")
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
