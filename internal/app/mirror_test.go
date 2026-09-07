package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// mirrorApp is an app that merges on the file name alone, which is the policy
// that catches the case people actually paste: one release, two hosters, no
// byte count known yet because nothing has been downloaded.
func mirrorApp(t *testing.T, keep bool) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		s.MirrorPolicy = string(dedupe.PolicyFilenameOnly)
		s.KeepMirrors = keep
	})
	return a
}

// mirrorPair stages the same file from two hosters and returns what the second
// paste produced.
func mirrorPair(t *testing.T, a *App) (first, second []*core.Task) {
	t.Helper()
	first = a.AddLinks([]string{"https://one.example/film.rar"}, "Release")
	if len(first) != 1 {
		t.Fatalf("the first link staged %d tasks, want 1", len(first))
	}
	return first, a.AddLinks([]string{"https://two.example/film.rar"}, "Release")
}

// TestAMirrorIsStillDroppedByDefault pins the behaviour every existing install
// has. Two copies of one file is two downloads of one file, and the list is long
// enough already; keeping the second one is a choice somebody makes.
func TestAMirrorIsStillDroppedByDefault(t *testing.T) {
	a := mirrorApp(t, false)
	_, second := mirrorPair(t, a)
	if len(second) != 0 {
		t.Fatalf("the mirror staged %d tasks, want none by default", len(second))
	}
	if skipped := a.SkippedLinks(); len(skipped) != 1 || skipped[0].Kind != "mirror" {
		t.Errorf("the folded link left the trace %+v, want one mirror entry", skipped)
	}
}

// TestAKeptMirrorNamesWhatItIsACopyOf is the field's reason to exist. Until
// this, Task.MirrorOf was persisted, read back and rendered, and nothing in the
// tree ever wrote it - a column promising an answer it could not have.
func TestAKeptMirrorNamesWhatItIsACopyOf(t *testing.T) {
	a := mirrorApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	sib := second[0]
	if sib.MirrorOf != first[0].ID {
		t.Errorf("mirrorOf = %q, want the task it is a copy of (%q)", sib.MirrorOf, first[0].ID)
	}
	if sib.URL != "https://two.example/film.rar" {
		t.Errorf("the sibling carries %q, want the second hoster's link", sib.URL)
	}
	if !sib.Hold {
		t.Error("the sibling is not on hold, so the queue would fetch the same file twice")
	}
	if !sib.Enabled {
		t.Error("the sibling was switched off; Enabled is the user's own switch and nothing here may write it")
	}
	if skipped := a.SkippedLinks(); len(skipped) != 0 {
		t.Errorf("the kept link was also recorded as skipped: %+v", skipped)
	}
}

// TestAKeptMirrorIsNotDispatched is what Hold is doing there. A sibling that the
// queue picks up is not a spare copy, it is the same file downloaded twice -
// which is the behaviour the whole mirror set exists to prevent.
func TestAKeptMirrorIsNotDispatched(t *testing.T) {
	a := mirrorApp(t, true)
	_, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	a.StartTasks(nil)
	a.mu.Lock()
	dispatched := a.active[second[0].ID]
	a.mu.Unlock()
	if dispatched {
		t.Error("\"start everything\" handed the sibling to a backend")
	}
}

// TestTheSameLinkTwiceIsStillARefusal is the line the setting does not move. The
// same URL a second time is a fact rather than a guess about two files, and
// staging it as a "sibling" would put two rows in the list pointing at the same
// bytes on the same hoster.
func TestTheSameLinkTwiceIsStillARefusal(t *testing.T) {
	a := mirrorApp(t, true)
	if created := a.AddLinks([]string{"https://one.example/film.rar"}, "Release"); len(created) != 1 {
		t.Fatalf("the first paste staged %d tasks", len(created))
	}
	again := a.AddLinks([]string{"https://one.example/film.rar"}, "Release")
	if len(again) != 0 {
		t.Fatalf("the same URL pasted twice staged %d more tasks, want none", len(again))
	}
	if skipped := a.SkippedLinks(); len(skipped) != 1 || skipped[0].Kind != "duplicate" {
		t.Errorf("the second paste left %+v, want one duplicate entry", skipped)
	}
}

// TestAThirdCopyIsMeasuredAgainstTheSiblingToo is why putSibling files the link
// in the mirror set. Left out, the set would still only know the original, and
// re-pasting the sibling's own URL would stage a second sibling of the same
// download every time.
func TestAThirdCopyIsMeasuredAgainstTheSiblingToo(t *testing.T) {
	a := mirrorApp(t, true)
	if _, second := mirrorPair(t, a); len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	again := a.AddLinks([]string{"https://two.example/film.rar"}, "Release")
	if len(again) != 0 {
		t.Errorf("the sibling's own URL pasted again staged %d tasks, want none", len(again))
	}
}

// TestAKeptMirrorSurvivesARestart is the difference the setting actually buys.
// A dropped mirror lives on only in an in-memory trace that the next restart
// clears; a sibling is a task, and a task is in the store.
func TestAKeptMirrorSurvivesARestart(t *testing.T) {
	a := mirrorApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range stored {
		if task.ID == second[0].ID {
			if task.MirrorOf != first[0].ID {
				t.Errorf("the stored sibling says mirrorOf %q, want %q", task.MirrorOf, first[0].ID)
			}
			if !task.Hold {
				t.Error("the stored sibling is not on hold, so a restart would start it")
			}
			return
		}
	}
	t.Fatal("the sibling is not in the store at all")
}

// failoverApp is mirrorApp with the handover switched on as well, and with the
// queue stopped.
//
// The stop is what keeps these tests off the network: a released sibling is
// queued, and an unhalted dispatcher would hand two.example straight to a
// backend, so every assertion below would be racing a real DNS lookup and the
// failure it eventually reports - which would come back through onUpdate and
// release the NEXT copy while the test was reading the previous one. Halted, the
// handover is observed exactly where it leaves the task.
func failoverApp(t *testing.T, over bool) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		s.MirrorPolicy = string(dedupe.PolicyFilenameOnly)
		s.KeepMirrors = true
		s.MirrorFailover = over
	})
	a.SetHalted(true)
	return a
}

// killTask settles a task through the ordinary failure path. spent burns the
// retry budget first, which is the state the handover's usual trigger reads:
// every attempt the user allowed has been made.
func killTask(t *testing.T, a *App, id string, spent bool, err string) {
	t.Helper()
	a.mu.Lock()
	task := a.tasks[id]
	if task == nil {
		a.mu.Unlock()
		t.Fatalf("no task %s to fail", id)
	}
	if spent {
		task.Retries = a.Settings.Get().MaxRetries
	}
	task.Status = core.StatusRunning
	a.active[id] = true
	a.started[id] = true
	a.mu.Unlock()
	a.onUpdate(id, core.Update{Status: core.StatusError, Err: err})
}

// releasedMirror is the one copy the handover has let go, or nil. Queued AND
// off hold, both: "start everything" leaves a parked sibling queued with the
// hold still on it, so the status alone would report a release that never
// happened.
func releasedMirror(t *testing.T, a *App) *core.Task {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	var out *core.Task
	for _, task := range a.tasks {
		if task.MirrorOf != "" && task.Status == core.StatusQueued && !task.Hold {
			if out != nil {
				t.Fatalf("two copies were released at once: %s and %s", out.ID, task.ID)
			}
			out = task
		}
	}
	return out
}

// TestTheParkedMirrorStaysParkedByDefault is the switch's default, and it is the
// one behaviour the rest of this feature must not be able to change by accident:
// an install that never opened the settings page keeps its spare copy on hold
// and downloads nothing from a hoster nobody chose.
func TestTheParkedMirrorStaysParkedByDefault(t *testing.T) {
	a := failoverApp(t, false)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	killTask(t, a, first[0].ID, true, "rapidgator: error code 7731")
	a.mu.Lock()
	held, status := a.tasks[second[0].ID].Hold, a.tasks[second[0].ID].Status
	a.mu.Unlock()
	if !held || status != core.StatusCollected {
		t.Errorf("the sibling is hold=%v status=%q after the source died; with the switch off it may not move", held, status)
	}
}

// TestTheMirrorTakesOverWhenTheBackoffIsSpent is the feature. The source has
// used every attempt it was given, so the spare copy stops being a spare: it is
// released, and it takes over the three things that were about the file rather
// than about the link.
func TestTheMirrorTakesOverWhenTheBackoffIsSpent(t *testing.T) {
	a := failoverApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	// Set on the original only. A Packagizer rule or a hand-typed folder reaches
	// the row that was running, never the copy parked behind it, so a handover
	// that does not carry them lands the file somewhere nobody was looking.
	a.mu.Lock()
	dead := a.tasks[first[0].ID]
	dead.Dir, dead.Package, dead.Priority = t.TempDir(), "Film night", 4
	wantDir, wantPkg := dead.Dir, dead.Package
	a.mu.Unlock()

	killTask(t, a, first[0].ID, true, "rapidgator: error code 7731")

	sib := releasedMirror(t, a)
	if sib == nil {
		t.Fatal("the source used its last attempt and the parked copy was left parked")
	}
	if sib.ID != second[0].ID {
		t.Fatalf("released %s, want the staged sibling %s", sib.ID, second[0].ID)
	}
	a.mu.Lock()
	got := *sib
	queued := false
	for _, id := range a.queue {
		if id == sib.ID {
			queued = true
		}
	}
	a.mu.Unlock()
	if got.Hold {
		t.Error("the released copy is still on hold, so the dispatcher will walk straight past it")
	}
	if !queued {
		t.Error("the released copy is not in the queue, so nothing will ever pick it up")
	}
	if got.Dir != wantDir || got.Package != wantPkg || got.Priority != 4 {
		t.Errorf("the copy runs as dir=%q package=%q priority=%d, want the dead task's %q/%q/4",
			got.Dir, got.Package, got.Priority, wantDir, wantPkg)
	}
	if got.MirrorOf != first[0].ID {
		t.Errorf("mirrorOf = %q, want the copy to keep saying what it is a copy of (%q)", got.MirrorOf, first[0].ID)
	}
	// The handover is not another go at anything: this URL has never been tried,
	// so it gets the whole budget rather than the dead link's spent one.
	if got.Retries != 0 {
		t.Errorf("the copy starts with %d retries used, want a fresh budget", got.Retries)
	}
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range stored {
		if task.ID != sib.ID {
			continue
		}
		if task.Hold || task.Status != core.StatusQueued {
			t.Errorf("the stored copy is hold=%v status=%q, so a restart would park it again", task.Hold, task.Status)
		}
		return
	}
	t.Error("the released copy never reached the store")
}

// TestAStartedQueueStillHasACopyToHandOver is the shape most installs are
// actually in when a download dies. "Start everything" reaches a held sibling
// and moves it to StatusQueued - startTasks never looks at Hold, the dispatcher
// does - so a handover that only recognised a collected task would find nothing
// for anybody who has pressed start with a full collector. The copy also has to
// end up in the queue ONCE: it is already in there, and a second entry is a
// second Start for one file the moment a slot frees.
func TestAStartedQueueStillHasACopyToHandOver(t *testing.T) {
	a := failoverApp(t, true)
	first, second := mirrorPair(t, a)
	if len(second) != 1 {
		t.Fatalf("the mirror staged %d tasks, want the sibling", len(second))
	}
	a.StartTasks(nil)
	a.mu.Lock()
	status, held := a.tasks[second[0].ID].Status, a.tasks[second[0].ID].Hold
	a.mu.Unlock()
	if status != core.StatusQueued || !held {
		t.Fatalf("the sibling is status=%q hold=%v after a start; this test is not exercising the shape it is about", status, held)
	}

	killTask(t, a, first[0].ID, true, "rapidgator: error code 7731")

	sib := releasedMirror(t, a)
	if sib == nil || sib.ID != second[0].ID {
		t.Fatal("the copy was queued behind a hold and the handover walked past it")
	}
	a.mu.Lock()
	entries := 0
	for _, id := range a.queue {
		if id == sib.ID {
			entries++
		}
	}
	held = sib.Hold
	a.mu.Unlock()
	if held {
		t.Error("the copy is still on hold, so the dispatcher will keep skipping it")
	}
	if entries != 1 {
		t.Errorf("the copy sits in the queue %d times, want once: each entry is another Start for the same file", entries)
	}
}

// TestTheFailedSourceStaysOnTheList is the half of the handover that is about
// the person, not the file. The dead link keeps its row, its sentence and its
// reason; a list that tidies it away tells somebody a story in which nothing
// went wrong, and they never learn that the hoster they use is dead.
func TestTheFailedSourceStaysOnTheList(t *testing.T) {
	a := failoverApp(t, true)
	first, _ := mirrorPair(t, a)
	killTask(t, a, first[0].ID, true, "rapidgator: error code 7731")
	if releasedMirror(t, a) == nil {
		t.Fatal("nothing was released, so this test is not looking at a handover")
	}
	a.mu.Lock()
	dead := a.tasks[first[0].ID]
	a.mu.Unlock()
	if dead == nil {
		t.Fatal("the failed source was removed from the list")
	}
	if dead.Status != core.StatusError {
		t.Errorf("the source now reads %q, want it left as the failure it was", dead.Status)
	}
	if dead.Error == "" {
		t.Error("the source lost its error sentence, so the row cannot say what happened")
	}
}

// TestADeadLinkDoesNotWaitOutItsBackoff is the second trigger. Every remaining
// attempt would ask the same host about the same missing file, so the copy takes
// their place - and the attempt the backoff had just counted goes with them,
// because a row promising a retry it will never make is a row people wait on.
func TestADeadLinkDoesNotWaitOutItsBackoff(t *testing.T) {
	a := failoverApp(t, true)
	first, _ := mirrorPair(t, a)
	// Not spent: this link has its whole budget left, and the point is that a 404
	// makes spending it pointless.
	killTask(t, a, first[0].ID, false, "jd /downloads: HTTP 404")

	if releasedMirror(t, a) == nil {
		t.Fatal("a link the host says is gone waited out its backoff instead of handing over")
	}
	a.mu.Lock()
	dead := *a.tasks[first[0].ID]
	a.mu.Unlock()
	if dead.Reason != core.ReasonGone {
		t.Fatalf("reason = %q, want %q: this test is not exercising the dead-link path", dead.Reason, core.ReasonGone)
	}
	if !dead.NextTry.IsZero() {
		t.Errorf("a retry is still armed for %v while the copy is already running", dead.NextTry)
	}
	if dead.Retries != 0 {
		t.Errorf("retries = %d, want the counted attempt taken back with the retry that was armed for it", dead.Retries)
	}
}

// TestTheChainEndsWhenTheCopiesRunOut answers "what if the mirror dies too".
// Every hop consumes one parked copy, so three pasted links allow two handovers
// and then the last failure simply stands - it cannot hand back to a link that
// already failed, and it cannot find a fourth copy that was never pasted.
func TestTheChainEndsWhenTheCopiesRunOut(t *testing.T) {
	a := failoverApp(t, true)
	first, second := mirrorPair(t, a)
	third := a.AddLinks([]string{"https://three.example/film.rar"}, "Release")
	if len(second) != 1 || len(third) != 1 {
		t.Fatalf("staged %d and %d copies, want one each", len(second), len(third))
	}
	killTask(t, a, first[0].ID, true, "rapidgator: error code 7731")
	hop1 := releasedMirror(t, a)
	if hop1 == nil {
		t.Fatal("the first failure released nothing")
	}
	killTask(t, a, hop1.ID, true, "rapidgator: error code 7731")
	hop2 := releasedMirror(t, a)
	if hop2 == nil {
		t.Fatal("the copy died and the second copy was left parked: the chain stopped one hop early")
	}
	if hop2.ID == hop1.ID {
		t.Fatal("the same copy was released twice")
	}
	// Both siblings, in whichever order their timestamps put them.
	if got := map[string]bool{hop1.ID: true, hop2.ID: true}; !got[second[0].ID] || !got[third[0].ID] {
		t.Errorf("the chain ran through %v, want both staged copies", got)
	}
	killTask(t, a, hop2.ID, true, "rapidgator: error code 7731")
	if left := releasedMirror(t, a); left != nil {
		t.Fatalf("a third handover released %s, but every copy had already had its turn", left.ID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, task := range a.tasks {
		if task.Status != core.StatusError {
			t.Errorf("task %s reads %q once the copies ran out, want the last failure to stand", id, task.Status)
		}
	}
}

// TestAFullDiskDoesNotChangeHoster is the veto. The second hoster writes to the
// same disk, so the handover would spend a copy to reach the identical wall and
// bury the one failure somebody could actually have fixed.
func TestAFullDiskDoesNotChangeHoster(t *testing.T) {
	for _, tc := range []struct{ name, err string }{
		{"a full disk", "write /data/film.rar.part: no space left on device"},
		{"a cancelled run", "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := failoverApp(t, true)
			first, _ := mirrorPair(t, a)
			killTask(t, a, first[0].ID, true, tc.err)
			if sib := releasedMirror(t, a); sib != nil {
				t.Errorf("%s released copy %s, and a second hoster cannot mend it", tc.name, sib.ID)
			}
		})
	}
}

// TestMirrorCanHelp pins the veto list the way TestAddressMayHelp pins the
// reconnect's. Anything unclassified has to answer yes: a taxonomy that grows a
// name next year must not quietly switch the handover off for it.
func TestMirrorCanHelp(t *testing.T) {
	for _, r := range []core.Reason{core.ReasonDiskFull, core.ReasonCancelled} {
		if mirrorCanHelp(r) {
			t.Errorf("%q would spend a spare copy on a failure a second hoster cannot mend", r)
		}
	}
	canHelp := []core.Reason{
		core.ReasonGone, core.ReasonAuth, core.ReasonLimit, core.ReasonUnavailable,
		core.ReasonNetwork, core.ReasonUnsupported, core.ReasonCaptcha, core.ReasonUnknown,
	}
	for _, r := range canHelp {
		if !mirrorCanHelp(r) {
			t.Errorf("%q leaves a usable spare copy parked", r)
		}
	}
}
