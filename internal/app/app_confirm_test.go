package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// confirmApp returns a queue app with the given global onDupes/onOffline pair.
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
	// Without a network the dispatch may settle as an error as fast as it
	// queues, so only leaving the collector is asserted.
	if liveStatus == core.StatusCollected {
		t.Errorf("the live link's status = %q, want it no longer sitting in the collector", liveStatus)
	}
}

// TestConfirmTasksNeverExcludesUnknownOrUncheckable: only a definite offline
// result may exclude a link, or one hoster declining a probe drops a package.
func TestConfirmTasksNeverExcludesUnknownOrUncheckable(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	unknown := putTask(t, a, collectedTask("unknown", func(c *core.Task) { c.Online = core.AvailUnknown }))
	uncheckable := putTask(t, a, collectedTask("uncheckable", func(c *core.Task) { c.Online = core.AvailUncheckable }))

	res := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)

	if !sameIDs(res.Start, []string{unknown.ID, uncheckable.ID}) {
		t.Errorf("Start = %v, want both unknown and uncheckable links started", res.Start)
	}
}

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

// TestConfirmTasksCatchesADuplicateViaTheExistingCleanupEngine checks that a
// collected copy of a finished task is caught by duplicatesLocked.
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

// TestConfirmTasksAskFallsBackToGlobalWhenNotInteractive: auto-confirm, the
// watch folder and Click'n'Load run with nobody watching, so "ask" must
// resolve to something concrete.
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
		// A global Ask falls back to confirm.DefaultPolicy, which excludes.
		if len(res.Start) != 0 {
			t.Errorf("trigger %s: Start = %v, want the offline link still held back", trig, res.Start)
		}
	}
}

func TestConfirmTasksBatchOverridesGlobal(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.Include)
	putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))

	res := a.ConfirmTasks(nil, confirm.Config{OnOffline: confirm.Exclude}, confirm.TriggerManual)

	if len(res.Start) != 0 {
		t.Errorf("Start = %v, want the batch's own exclude to win over a global that includes", res.Start)
	}
}

// TestConfirmTasksOnlyTouchesRequestedIDs guards against an empty Result.Start
// reaching StartTasks, which reads an empty list as "everything collected".
func TestConfirmTasksOnlyTouchesRequestedIDs(t *testing.T) {
	a := confirmApp(t, confirm.DefaultPolicy, confirm.DefaultPolicy)
	dead := putTask(t, a, collectedTask("dead", func(c *core.Task) { c.Online = core.AvailOffline }))
	untouched := putTask(t, a, collectedTask("untouched", nil))

	res := a.ConfirmTasks([]string{dead.ID}, confirm.Config{}, confirm.TriggerManual)

	if len(res.Start) != 0 {
		t.Fatalf("Start = %v, want none since the only requested id was held back", res.Start)
	}
	a.mu.Lock()
	status := a.tasks[untouched.ID].Status
	a.mu.Unlock()
	if status != core.StatusCollected {
		t.Errorf("an id nobody named was moved to %q by an empty Start list", status)
	}
}

// TestStartTasksAddAtTopPlaysNext tests StartTasks directly, since every route
// that starts tasks goes through it.
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
