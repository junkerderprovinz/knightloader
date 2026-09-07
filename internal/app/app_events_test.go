package app

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// TestScriptPackageTallies is the definition of "this package is finished",
// written out case by case, because that definition is the whole reason
// package.done did not exist before: task.done fires per file, and the
// question nobody had answered was what a failed, held, switched-off or
// not-yet-started member does to the answer.
//
// Pure over the task map on purpose - see scriptPackageTallies' own doc
// comment. Every case below is a state a real queue reaches, and each one
// used to be a judgement call somebody would otherwise make differently at
// two call sites.
func TestScriptPackageTallies(t *testing.T) {
	future := time.Now().Add(time.Minute)
	cases := []struct {
		name     string
		tasks    []*core.Task
		want     script.PackageView
		complete bool
	}{
		{
			name:     "one file, finished",
			tasks:    []*core.Task{{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true, Loaded: 100}},
			want:     script.PackageView{Name: "P", Files: 1, Done: 1, Bytes: 100},
			complete: true,
		},
		{
			name: "a file still running holds the whole package back",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true, Loaded: 100},
				{ID: "2", Package: "P", Status: core.StatusRunning, Enabled: true, Loaded: 20},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1, Bytes: 120},
			complete: false,
		},
		{
			name: "a file that finally failed does not stop the package finishing",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true},
				{ID: "2", Package: "P", Status: core.StatusError, Enabled: true},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1, Failed: 1},
			complete: true,
		},
		{
			name: "a file failed WITH a retry armed is still pending",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true},
				{ID: "2", Package: "P", Status: core.StatusError, Enabled: true, NextTry: future},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1},
			complete: false,
		},
		{
			name: "a link the filter is holding is counted, never waited for",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true},
				{ID: "2", Package: "P", Status: core.StatusCollected, Enabled: true, Skipped: true},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1, Skipped: 1},
			complete: true,
		},
		{
			name: "a file the user switched off is counted, never waited for",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true},
				{ID: "2", Package: "P", Status: core.StatusQueued, Enabled: false},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1, Disabled: 1},
			complete: true,
		},
		{
			name: "a link still sitting in the collector holds the package back",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: true},
				{ID: "2", Package: "P", Status: core.StatusCollected, Enabled: true},
			},
			want:     script.PackageView{Name: "P", Files: 2, Done: 1},
			complete: false,
		},
		{
			name: "an archive still unpacking holds the package back",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusExtracting, Enabled: true},
			},
			want:     script.PackageView{Name: "P", Files: 1},
			complete: false,
		},
		{
			// The clause that is easy to leave out of complete(): with only
			// "nothing is pending" a package of nothing but filtered links
			// reports finished, and a script fires for a package that never
			// downloaded a byte and never will.
			name: "nothing but links the filter is holding is not a finished package",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusCollected, Enabled: true, Skipped: true},
				{ID: "2", Package: "P", Status: core.StatusCollected, Enabled: true, Skipped: true},
			},
			want:     script.PackageView{Name: "P", Files: 2, Skipped: 2},
			complete: false,
		},
		{
			// Same clause, the other way in: a package somebody staged and
			// then switched off entirely has nothing pending either.
			name: "nothing but switched-off links is not a finished package",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusCollected, Enabled: false},
			},
			want:     script.PackageView{Name: "P", Files: 1, Disabled: 1},
			complete: false,
		},
		{
			// Disabled deliberately overlaps the outcome counts - see
			// script.PackageView's doc comment on why the four do not
			// partition Files. Switching a file off AFTER it finished does
			// not un-finish it, so this package is done and reports both.
			name: "a finished file that was later switched off counts as both",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusDone, Enabled: false, Loaded: 7},
			},
			want:     script.PackageView{Name: "P", Files: 1, Done: 1, Disabled: 1, Bytes: 7},
			complete: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]*core.Task{}
			for _, task := range tc.tasks {
				m[task.ID] = task
			}
			got := scriptPackageTallies(m)
			p, ok := got["P"]
			if !ok {
				t.Fatalf("no tally for package P at all; got %v", got)
			}
			if p.view != tc.want {
				t.Errorf("view = %+v, want %+v", p.view, tc.want)
			}
			if p.complete() != tc.complete {
				t.Errorf("complete() = %v, want %v (pending=%d settled=%d)", p.complete(), tc.complete, p.pending, p.settled)
			}
		})
	}
}

// TestScriptPackageTalliesIgnoresUnpackagedTasks keeps the empty package name
// out of the map. Folding every unpackaged download into one bucket is the
// same mistake packageFilesLocked refuses to make for the info-file sweep,
// and here it would fire one package.done for the whole shared download
// folder every time any loose link finished.
func TestScriptPackageTalliesIgnoresUnpackagedTasks(t *testing.T) {
	got := scriptPackageTallies(map[string]*core.Task{
		"1": {ID: "1", Status: core.StatusDone, Enabled: true},
		"2": {ID: "2", Package: "   ", Status: core.StatusDone, Enabled: true},
		"3": {ID: "3", Package: "P", Status: core.StatusDone, Enabled: true},
	})
	if len(got) != 1 {
		t.Fatalf("tallies = %v, want exactly one entry (P)", got)
	}
	if _, ok := got["P"]; !ok {
		t.Fatalf("tallies = %v, want the entry to be P", got)
	}
}

// TestPackageDoneFiresWhenTheLastFileSettles is the whole feature end to
// end: the app publishes, the script host receives it off the bus, and a
// real script bound to package.done runs with the counts filled in.
//
// It also pins the boot seeding, which is the part with no other way to
// prove it: the "Boot" package below is already finished when the loop
// starts, and must never fire. Without that seeding, every package a person
// ever downloaded would announce itself two seconds after every restart.
func TestPackageDoneFiresWhenTheLastFileSettles(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	fc := &activityFakeConn{}
	a.Hub.Add(fc)

	if _, err := a.Scripts.SaveScript(script.Script{
		Name:    "announce package",
		Trigger: script.TriggerPackageDone,
		Enabled: true,
		Code:    `notify(pkg.name + ":" + pkg.done + "/" + pkg.files);`,
	}); err != nil {
		t.Fatal(err)
	}

	// "Boot" is complete before the sweep ever looks; "Release" is not.
	pending := &core.Task{ID: "2", Package: "Release", URL: "https://host.example/b.bin", Status: core.StatusRunning, Enabled: true}
	a.mu.Lock()
	a.tasks["1"] = &core.Task{ID: "1", Package: "Boot", URL: "https://host.example/a.bin", Status: core.StatusDone, Enabled: true}
	a.tasks["2"] = pending
	a.mu.Unlock()

	// Long enough to outlast one whole seeding pass. The first pass records
	// what is already complete and fires nothing, so flipping the task before
	// it happened would have "Release" seeded as finished and the event would
	// correctly never arrive - a green test that proved the opposite of what
	// it claims.
	time.Sleep(scriptPackagePoll + 500*time.Millisecond)

	a.mu.Lock()
	pending.Status = core.StatusDone
	a.mu.Unlock()

	waitFor(t, "a package.done script announced Release", func() bool {
		for _, raw := range fc.snapshot() {
			var env struct {
				Type string       `json:"type"`
				Data script.Event `json:"data"`
			}
			if json.Unmarshal(raw, &env) != nil || env.Type != "script" || env.Data.Kind != "notify" {
				continue
			}
			if env.Data.Message == "Boot:1/1" {
				t.Fatal("package.done fired for a package that was already finished when the app started; the first sweep must seed silently")
			}
			if env.Data.Message == "Release:1/1" {
				return true
			}
		}
		return false
	})
}

// TestLinkAddedFiresOnlyForLinksThatEnteredTheList is script.TriggerLinkAdded's
// own carve-out, against the real put(): a link the filter is holding is in
// the holding area rather than the collector, nothing will download it unless
// a person restores it, and greeting it as an arrival hands a script a task
// with no future.
func TestLinkAddedFiresOnlyForLinksThatEnteredTheList(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, err := a.Scripts.SaveScript(script.Script{
		Name:    "greet",
		Trigger: script.TriggerLinkAdded,
		Enabled: true,
		Code:    `task.setComment("greeted")`,
	}); err != nil {
		t.Fatal(err)
	}

	collected := &core.Task{ID: "1", URL: "https://host.example/a.bin", Status: core.StatusCollected, Enabled: true}
	held := &core.Task{ID: "2", URL: "https://host.example/b.bin", Status: core.StatusCollected, Enabled: true, Skipped: true}
	if _, ok := a.put(collected); !ok {
		t.Fatal("put refused the collected link")
	}
	if _, ok := a.put(held); !ok {
		t.Fatal("put refused the held link")
	}

	waitFor(t, "a link.added script commented the link that entered the list", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return collected.Comment == "greeted"
	})
	// The held link was published before the poll above even started, so a
	// firing for it would already be in the worker queue. The extra window
	// is what makes this a real negative rather than a race the script
	// happened to lose - the same shape TestScriptDoesNotFireOnTaskFailed-
	// WithRetryPending uses for its own absence check.
	time.Sleep(300 * time.Millisecond)
	a.mu.Lock()
	heldComment := held.Comment
	a.mu.Unlock()
	if heldComment != "" {
		t.Errorf("a link the filter is holding fired link.added (comment = %q)", heldComment)
	}
}

// TestAccountExpiredFiresOnTheCrossingNotTheState is the edge detection
// fireAccountExpiry exists for. The account-health sweep runs every fifteen
// minutes for the life of the process and a lapsed account is lapsed on
// every single pass, so firing on the state would be a notification script
// sending the same message four times an hour, for ever.
//
// Observed by subscribing to a.Events directly, which is also the point of
// the bus: a second consumer is a Subscribe call and nothing else.
func TestAccountExpiredFiresOnTheCrossingNotTheState(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var mu sync.Mutex
	var got []script.AccountView
	a.Events.Subscribe("test", func(f script.Firing) {
		if f.Trigger != script.TriggerAccountExpired || f.Account == nil {
			return
		}
		mu.Lock()
		got = append(got, *f.Account)
		mu.Unlock()
	})

	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	laterButStillPast := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	lapsed := AccountHealth{Tier: "premium", Expiry: past}
	// First reading of this account at all: the crossing.
	a.fireAccountExpiry("alldebrid", "", AccountHealth{}, false, lapsed)
	// The same lapse, read again fifteen minutes later. Must be silent.
	a.fireAccountExpiry("alldebrid", "", lapsed, true, lapsed)
	// A renewal the provider did not actually apply: the expiry moved and is
	// still in the past. That is a new fact, so it fires again.
	moved := AccountHealth{Tier: "premium", Expiry: laterButStillPast}
	a.fireAccountExpiry("alldebrid", "", lapsed, true, moved)
	// Healthy readings, and the two shapes of "nothing to say": a free tier
	// with no expiry at all, and an account nothing has read yet. Neither is
	// an expiry that passed.
	a.fireAccountExpiry("alldebrid", "", moved, true, AccountHealth{Tier: "premium", Expiry: future})
	a.fireAccountExpiry("torbox", "", AccountHealth{}, false, AccountHealth{Tier: "free"})
	a.fireAccountExpiry("torbox", "", AccountHealth{}, false, AccountHealth{Tier: tierUnknown})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("account.expired fired %d times (%+v), want exactly 2: the first lapse and the expiry that moved and is still past", len(got), got)
	}
	if got[0].Expiry != past || got[1].Expiry != laterButStillPast {
		t.Errorf("firings carried expiries %q and %q, want %q then %q", got[0].Expiry, got[1].Expiry, past, laterButStillPast)
	}
	if got[0].Service != "alldebrid" || got[0].Tier != "premium" {
		t.Errorf("first firing = %+v, want the alldebrid premium account", got[0])
	}
}
