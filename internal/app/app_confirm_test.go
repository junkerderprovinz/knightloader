package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// confirmApp is a queue app with a chosen global onDupes/onOffline pair, so
// each test states only the one thing it is about.
func confirmApp(t *testing.T, onDupes, onOffline confirm.Policy) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		OnDupes: string(onDupes), OnOffline: string(onOffline),
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

func collectedTask(id string, mutate func(*core.Task)) core.Task {
	t := core.Task{
		ID: id, URL: "https://host.example/" + id, Name: id + ".bin",
		Status: core.StatusCollected, Enabled: true,
	}
	if mutate != nil {
		mutate(&t)
	}
	return t
}

// TestConfirmTasksExcludesOfflineByDefault is the plain default: nothing has
// asked for anything unusual, and a link a check has already found gone
// stays exactly where it was rather than joining the queue.
func TestConfirmTasksExcludesOfflineByDefault(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))
	live := putTask(t, a, collectedTask("live", nil))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if !sameIDs(res.Start, []string{live.ID}) {
		t.Errorf("Start = %v, want only the live link", res.Start)
	}
	a.mu.Lock()
	deadStatus, liveStatus := a.tasks[dead.ID].Status, a.tasks[live.ID].Status
	a.mu.Unlock()
	if deadStatus != core.StatusCollected {
		t.Errorf("the offline link's status = %q, want it left exactly as it was", deadStatus)
	}
	// Not asserted as specifically StatusQueued: there is no network in a
	// test, so a real dispatch attempt against host.example settles as an
	// error just as fast as it would queue - see
	// TestDisabledAndHeldLinksAreNotDispatched's own comment for the same
	// reasoning. What ConfirmTasks promises is that the live link left the
	// collector at all, which res.Start already pinned above.
	if liveStatus == core.StatusCollected {
		t.Errorf("the live link's status = %q, want it no longer sitting in the collector", liveStatus)
	}
}

// TestConfirmTasksNeverExcludesUnknownOrUncheckable pins the one rule
// section 8 of the build plan calls out by name: only a definite "gone" may
// ever be excluded, or one hoster declining a probe quietly drops a whole
// package.
func TestConfirmTasksNeverExcludesUnknownOrUncheckable(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	unknown := putTask(t, a, collectedTask("unknown", func(c *core.Task) { c.Online = core.AvailUnknown }))
	uncheckable := putTask(t, a, collectedTask("uncheckable", func(c *core.Task) { c.Online = core.AvailUncheckable }))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if !sameIDs(res.Start, []string{unknown.ID, uncheckable.ID}) {
		t.Errorf("Start = %v, want both unknown and uncheckable links started", res.Start)
	}
}

// TestConfirmTasksNeverDeletesByDefault is the default this whole feature
// may never quietly change: an offline link is excluded, never removed,
// until a person deliberately configures otherwise.
func TestConfirmTasksNeverDeletesByDefault(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if len(res.Remove) != 0 {
		t.Fatalf("Remove = %v, want nothing removed by a default policy", res.Remove)
	}
	a.mu.Lock()
	_, stillThere := a.tasks[dead.ID]
	a.mu.Unlock()
	if !stillThere {
		t.Error("the offline link was deleted by the default policy")
	}
}

// TestConfirmTasksExcludeAndRemoveDeletesTheTask is exclude-and-remove
// chosen on purpose, which is the only way it is ever reached - see
// TestConfirmTasksNeverDeletesByDefault for the default it is not.
func TestConfirmTasksExcludeAndRemoveDeletesTheTask(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.ExcludeAndRemove)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if !sameIDs(res.Remove, []string{dead.ID}) {
		t.Fatalf("Remove = %v, want [%s]", res.Remove, dead.ID)
	}
	a.mu.Lock()
	_, stillThere := a.tasks[dead.ID]
	a.mu.Unlock()
	if stillThere {
		t.Error("exclude-and-remove left the task in the list")
	}
}

// TestConfirmTasksCatchesADuplicateViaTheExistingCleanupEngine is the reuse
// this file's own report leans on: duplicatesLocked (app_bulk.go) already
// finds a collected task that is a second copy of one already settled in
// the list, without needing the dedupe-wiring fix (docs/build-plan.md
// section 8's "Dedupe gains a mode", still unlanded) that a fresh paste of
// the same URL twice would need.
func TestConfirmTasksCatchesADuplicateViaTheExistingCleanupEngine(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	putTask(t, a, core.Task{
		ID: "finished", URL: "https://host.example/finished", Name: "film.mkv", Size: 1000,
		Loaded: 1000, Status: core.StatusDone, Enabled: true, CreatedAt: time.Now().Add(-time.Hour),
	})
	again := putTask(t, a, core.Task{
		ID: "again", URL: "https://mirror.example/again", Name: "film.mkv", Size: 1000,
		Status: core.StatusCollected, Enabled: true, CreatedAt: time.Now(),
	})

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if len(res.Start) != 0 {
		t.Errorf("Start = %v, want the second copy held back", res.Start)
	}
	found := false
	for _, o := range res.Outcomes {
		if o.ID == again.ID {
			found = true
			if !sameIDs(reasonsToStrings(o.Reasons), []string{"duplicate"}) {
				t.Errorf("Reasons = %v, want [duplicate]", o.Reasons)
			}
		}
	}
	if !found {
		t.Fatalf("no outcome recorded for %s", again.ID)
	}
}

// TestConfirmTasksCombinesBothReasonsInOneSummary pins the worked example
// from this wave's own report: two independent reasons, reported once.
func TestConfirmTasksCombinesBothReasonsInOneSummary(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	for i := 0; i < 3; i++ {
		putTask(t, a, collectedTask(sprintfID("dead", i), func(c *core.Task) { c.Online = core.AvailOffline }))
	}
	putTask(t, a, core.Task{
		ID: "kept", URL: "https://host.example/kept", Name: "twin.bin", Size: 500,
		Loaded: 500, Status: core.StatusDone, Enabled: true, CreatedAt: time.Now().Add(-time.Hour),
	})
	for i := 0; i < 2; i++ {
		putTask(t, a, core.Task{
			ID: sprintfID("twin", i), URL: "https://mirror.example/" + sprintfID("twin", i),
			Name: "twin.bin", Size: 500, Status: core.StatusCollected, Enabled: true,
			CreatedAt: time.Now(),
		})
	}

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	want := "3 offline and 2 duplicate links were not started."
	if res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}

// TestConfirmTasksAsksWhenInteractive is the "ask" policy doing its one job
// when somebody really is there to answer: the link is settled neither way
// until they do.
func TestConfirmTasksAsksWhenInteractive(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.Ask)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if !sameIDs(res.Ask, []string{dead.ID}) {
		t.Fatalf("Ask = %v, want [%s]", res.Ask, dead.ID)
	}
	if len(res.Start) != 0 || len(res.Remove) != 0 {
		t.Errorf("Start=%v Remove=%v, want neither while the question is unanswered", res.Start, res.Remove)
	}
	a.mu.Lock()
	status := a.tasks[dead.ID].Status
	a.mu.Unlock()
	if status != core.StatusCollected {
		t.Errorf("status = %q, want it left in the collector pending an answer", status)
	}
}

// TestConfirmTasksAskFallsBackToGlobalWhenNotInteractive is the fallback
// this wave's report names explicitly: auto-confirm, the watch folder and
// Click'n'Load all fire with nobody watching, so "ask" has to resolve to
// something concrete instead of stalling the batch forever.
func TestConfirmTasksAskFallsBackToGlobalWhenNotInteractive(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.Ask)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	for _, trig := range []confirm.Trigger{confirm.TriggerAutoConfirm, confirm.TriggerWatch, confirm.TriggerCnL} {
		a.mu.Lock()
		a.tasks[dead.ID].Status = core.StatusCollected
		a.mu.Unlock()

		res := a.ConfirmTasks([]string{dead.ID}, confirm.Config{}, trig)
		if len(res.Ask) != 0 {
			t.Errorf("trigger %s: Ask = %v, want nobody left waiting on an answer", trig, res.Ask)
		}
		// Global onOffline is Ask, and Ask falling back to itself settles on
		// confirm.DefaultPolicy (exclude) - see confirm.Resolve.
		if len(res.Start) != 0 {
			t.Errorf("trigger %s: Start = %v, want the offline link still held back", trig, res.Start)
		}
	}
}

// TestConfirmTasksBatchOverridesGlobal is the per-batch half of the policy:
// a batch that names its own value is not merely a suggestion.
func TestConfirmTasksBatchOverridesGlobal(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.Include)
	putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	res := a.ConfirmTasks(nil, confirm.Config{OnOffline: confirm.Exclude}, confirm.TriggerManual)

	if len(res.Start) != 0 {
		t.Errorf("Start = %v, want the batch's own exclude to win over a global that includes", res.Start)
	}
}

// TestConfirmTasksOnlyTouchesRequestedIDs guards the trap StartTasks itself
// has to be careful about too: an empty Result.Start must never fall
// through to StartTasks's OWN "empty means everything collected" reading,
// or a batch that held every one of its own candidates back would also
// start an unrelated task nobody named.
func TestConfirmTasksOnlyTouchesRequestedIDs(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))
	untouched := putTask(t, a, collectedTask("untouched", nil))

	res := a.ConfirmTasks([]string{dead.ID}, confirm.Config{}, confirm.TriggerManual)

	if len(res.Start) != 0 {
		t.Fatalf("Start = %v, want none - the only requested id was held back", res.Start)
	}
	a.mu.Lock()
	status := a.tasks[untouched.ID].Status
	a.mu.Unlock()
	if status != core.StatusCollected {
		t.Errorf("an id nobody named was moved to %q by an empty Start list", status)
	}
}

// TestStartTasksAddAtTopPlaysNext is placement, the third of 8C's "confirm
// scope/start-mode/placement": a batch leaving the collector with AddAtTop
// on has to sort ahead of whatever was already queued, the same place a
// manual "move to top" would put it. Exercised directly on StartTasks
// (rather than through ConfirmTasks) because every route to StartTasks -
// the manual route, auto-confirm, the watch folder - inherits this from the
// one place it is applied; see this file's own top comment on why none of
// those callers had to change for that to be true.
func TestStartTasksAddAtTopPlaysNext(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(), AddAtTop: true,
	}); err != nil {
		t.Fatal(err)
	}

	already := putTask(t, a, core.Task{
		ID: "already", URL: "https://host.example/already", Name: "already.bin",
		Status: core.StatusQueued, Enabled: true, CreatedAt: time.Now().Add(-time.Hour),
	})
	fresh := putTask(t, a, collectedTask("fresh", nil))

	a.StartTasks([]string{fresh.ID})

	a.mu.Lock()
	alreadyPos, freshPos := a.tasks[already.ID].Position, a.tasks[fresh.ID].Position
	a.mu.Unlock()
	if freshPos >= alreadyPos {
		t.Errorf("fresh.Position = %d, already.Position = %d; want the newly confirmed link ordered ahead (a lower position)", freshPos, alreadyPos)
	}
}

// TestStartTasksAddAtTopOffLeavesTheOrderAlone is the default this feature
// must not disturb: AddAtTop is false everywhere until somebody turns it on,
// and StartTasks must go on appending exactly as it always did.
func TestStartTasksAddAtTopOffLeavesTheOrderAlone(t *testing.T) {
	a := newQueueApp(t)
	already := putTask(t, a, core.Task{
		ID: "already", URL: "https://host.example/already", Name: "already.bin",
		Status: core.StatusQueued, Enabled: true, Position: 0, CreatedAt: time.Now().Add(-time.Hour),
	})
	fresh := putTask(t, a, collectedTask("fresh", nil))

	a.StartTasks([]string{fresh.ID})

	a.mu.Lock()
	alreadyPos, freshPos := a.tasks[already.ID].Position, a.tasks[fresh.ID].Position
	a.mu.Unlock()
	if alreadyPos != 0 || freshPos != 0 {
		t.Errorf("positions changed with AddAtTop off: already=%d fresh=%d, want both left at 0", alreadyPos, freshPos)
	}
}

func reasonsToStrings(in []confirm.Reason) []string {
	out := make([]string, len(in))
	for i, r := range in {
		out[i] = string(r)
	}
	return out
}

func sprintfID(prefix string, i int) string {
	digits := "0123456789"
	return prefix + string(digits[i%10])
}
