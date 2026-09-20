package app

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// TestScriptPackageTallies spells out, case by case, when a package counts as
// finished: what failed, held, disabled and unstarted members do to the answer.
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
			name: "a file failed with a retry armed is still pending",
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
			// With only "nothing is pending", a package of filtered links would
			// report finished without ever downloading a byte.
			name: "nothing but links the filter is holding is not a finished package",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusCollected, Enabled: true, Skipped: true},
				{ID: "2", Package: "P", Status: core.StatusCollected, Enabled: true, Skipped: true},
			},
			want:     script.PackageView{Name: "P", Files: 2, Skipped: 2},
			complete: false,
		},
		{
			name: "nothing but switched-off links is not a finished package",
			tasks: []*core.Task{
				{ID: "1", Package: "P", Status: core.StatusCollected, Enabled: false},
			},
			want:     script.PackageView{Name: "P", Files: 1, Disabled: 1},
			complete: false,
		},
		{
			// Disabled overlaps the outcome counts (see script.PackageView), and
			// switching a file off after it finished does not un-finish it.
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

// TestScriptPackageTalliesIgnoresUnpackagedTasks: one bucket for all loose
// links would fire package.done whenever any of them finished.
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

// TestPackageDoneFiresWhenTheLastFileSettles runs a real package.done script
// end to end. It also checks the boot seeding: "Boot" is finished before the
// loop starts and must never fire, or every past package would announce itself
// after each restart.
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

	pending := &core.Task{ID: "2", Package: "Release", URL: "https://host.example/b.bin", Status: core.StatusRunning, Enabled: true}
	a.mu.Lock()
	a.tasks["1"] = &core.Task{ID: "1", Package: "Boot", URL: "https://host.example/a.bin", Status: core.StatusDone, Enabled: true}
	a.tasks["2"] = pending
	a.mu.Unlock()

	// Let the seeding pass run first; finishing "Release" before it would seed
	// it as done and the event would never arrive.
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

// TestLinkAddedFiresOnlyForLinksThatEnteredTheList: a link the filter holds is
// not in the collector and will not download unless restored.
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
	// Extra time so a firing for the held link would have landed before the
	// check.
	time.Sleep(300 * time.Millisecond)
	a.mu.Lock()
	heldComment := held.Comment
	a.mu.Unlock()
	if heldComment != "" {
		t.Errorf("a link the filter is holding fired link.added (comment = %q)", heldComment)
	}
}

// TestAccountExpiredFiresOnTheCrossingNotTheState: the health sweep runs every
// fifteen minutes, and a lapsed account stays lapsed, so firing on the state
// would repeat the message for ever.
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
	// First reading of the account: the crossing.
	a.fireAccountExpiry("alldebrid", "", AccountHealth{}, false, lapsed)
	// The same lapse read again: silent.
	a.fireAccountExpiry("alldebrid", "", lapsed, true, lapsed)
	// The expiry moved but is still past: a new fact, so it fires.
	moved := AccountHealth{Tier: "premium", Expiry: laterButStillPast}
	a.fireAccountExpiry("alldebrid", "", lapsed, true, moved)
	// A healthy reading, a free tier without expiry and an unread account.
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
