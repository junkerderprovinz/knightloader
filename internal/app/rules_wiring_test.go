package app

import (
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newRuleApp builds an app with the given rule wiring already saved, so every
// test below exercises the same path a settings save takes.
func newRuleApp(t *testing.T, mutate func(s *settings.Settings, base string)) (*App, string) {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	// The collector's HEAD, answered here instead of on the network. Without it
	// every staged link fires a real DNS lookup at a host.example address and
	// writes its failure onto the task whenever that lands, which races whatever
	// the test is about.
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		return probeAnswer(req, http.StatusOK), nil
	})
	base := t.TempDir()
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 2, 1
	s.DownloadDir = base
	// Off, so a pasted page URL is staged as itself and no test needs a network.
	s.Crawl = false
	mutate(&s, base)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a, base
}

// probeFunc turns a function into the collector's HEAD client.
type probeFunc func(*http.Request) (*http.Response, error)

func (f probeFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

// probeAnswer is what a HEAD comes back as. ContentLength is -1 ("not stated")
// rather than 0, because analyze reads a positive length as the file's size and
// a zero-byte answer would stamp every staged link as empty.
func probeAnswer(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Body:          io.NopCloser(strings.NewReader("")),
		Header:        http.Header{},
		Request:       req,
		ContentLength: -1,
	}
}

// rejectRule is a filter that turns down anything whose URL says "sample".
func rejectRule(reason string) rules.Set {
	return rules.Set{
		StopAfterMatch: true,
		Rules: []rules.Rule{{
			Name: "no samples",
			Conditions: []rules.Condition{
				{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"},
			},
			Action: rules.Action{Reject: true, Reason: reason},
		}},
	}
}

// A link the filter turns down is somewhere the user can find it, carrying the
// rule that stopped it and the reason that rule gave, rather than disappearing
// the way JDownloader's filtered links do. It is held rather than collected:
// kept and persisted, but out of the list, the queue and the counters, or a
// working filter would fill the collector with what it just caught.
func TestFilteredLinkIsVisibleWithItsReason(t *testing.T) {
	cases := []struct {
		name   string
		set    rules.Set
		wantIn []string
		// The sentence again as a code with its values, which the interface
		// words in the reader's language.
		wantCode   string
		wantParams map[string]string
	}{
		{
			name: "the rule's own words, with the rule named alongside them",
			set:  rejectRule("sample files are not wanted here"),
			// The reason is what the user reads, the rule name is what they edit.
			wantIn:     []string{"sample files are not wanted here", "no samples"},
			wantCode:   skipFilterRuleReason,
			wantParams: map[string]string{"reason": "sample files are not wanted here", "rule": "no samples"},
		},
		{
			name:       "a rule that gave no reason still names itself",
			set:        rejectRule(""),
			wantIn:     []string{"no samples"},
			wantCode:   rules.CodeFilterRule,
			wantParams: map[string]string{"rule": "no samples"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.LinkFilter = tc.set })

			created := a.AddLinks([]string{"https://host.example/sample.mkv"}, "Batch")
			if len(created) != 1 {
				t.Fatalf("staged %d tasks; a refused link is recorded, never dropped", len(created))
			}
			got := created[0]
			if !got.Skipped {
				t.Error("the link is not held; it would sit in the collector among the links that will download")
			}
			if got.Status != core.StatusCollected {
				t.Errorf("status = %q, want it parked rather than failed", got.Status)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(got.SkipReason, want) {
					t.Errorf("the reason reads %q, want it to mention %q", got.SkipReason, want)
				}
			}
			if got.SkipCode != tc.wantCode || !maps.Equal(got.SkipParams, tc.wantParams) {
				t.Errorf("the reason's code is %q %v, want %q %v", got.SkipCode, got.SkipParams, tc.wantCode, tc.wantParams)
			}
			if len(got.MatchedRules) != 1 || got.MatchedRules[0] != "no samples" {
				t.Errorf("matched rules = %v, want the rule that caught it as data rather than only inside the sentence", got.MatchedRules)
			}
			if got.Resolver != "" {
				t.Errorf("resolver = %q; a refused link is not resolved", got.Resolver)
			}
			// The holding area is what the interface lists, so it holds the same
			// link and not merely a flag.
			held := a.FilteredLinks()
			if len(held) != 1 || held[0].ID != got.ID {
				t.Errorf("the holding area holds %d links, want the one that was refused", len(held))
			}
		})
	}
}

// "Start everything" reaches every collected task, so a link parked with a
// reason and nothing else stopping it would be a filter one button undoes.
func TestAHeldLinkCannotBeStarted(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.LinkFilter = rejectRule("sample files are not wanted here")
	})
	created := a.AddLinks([]string{"https://host.example/sample.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}

	a.StartTasks(nil) // the "start everything" button

	a.mu.Lock()
	live := *a.tasks[created[0].ID]
	active := len(a.active)
	queued := len(a.queue)
	a.mu.Unlock()

	if active != 0 || queued != 0 {
		t.Errorf("%d dispatched and %d queued; a held link must not be reachable from start", active, queued)
	}
	if live.Status != core.StatusCollected || !live.Skipped {
		t.Errorf("status = %q, held = %v; want it still parked and still explained", live.Status, live.Skipped)
	}
	if !strings.Contains(live.SkipReason, "sample files are not wanted here") {
		t.Errorf("the reason reads %q, want the filter's own words to survive the start", live.SkipReason)
	}
}

// The holding area is usually opened because a rule turned out to be too broad,
// and the queue asks the filter once more before any bytes move, so a Restore
// that only un-parked the link would hand it back to the rule that caught it.
func TestRestoreLetsALinkPastTheRuleThatCaughtIt(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.LinkFilter = rejectRule("sample files are not wanted here")
	})
	created := a.AddLinks([]string{"https://host.example/sample.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	id := created[0].ID

	restored := a.RestoreFiltered(nil) // the "restore everything" button
	if len(restored) != 1 || restored[0].ID != id {
		t.Fatalf("restored %d links, want the one that was held", len(restored))
	}
	if restored[0].Skipped {
		t.Error("the restored link is still held")
	}
	if restored[0].SkipReason == "" {
		t.Error("the reason was dropped; it records that the user overruled the filter, and the queue reads it")
	}
	if len(a.FilteredLinks()) != 0 {
		t.Error("the link is still in the holding area after being restored")
	}

	// dispatchLocked settles what it refuses inside StartTasks, so a rule still
	// in the way would have written its refusal onto the task by this line.
	a.StartTasks([]string{id})
	a.mu.Lock()
	live := *a.tasks[id]
	a.mu.Unlock()
	if live.Status == core.StatusError && strings.Contains(live.Error, "sample files are not wanted here") {
		t.Fatal("the queue refused the restored link with the very reason the user had already overruled")
	}
}

// Clear is offered on a list of links somebody has decided they do not want, so
// reaching past that list into the collector would delete work in progress.
func TestClearFilteredEmptiesOnlyTheHoldingArea(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.LinkFilter = rejectRule("sample files are not wanted here")
	})
	created := a.AddLinks([]string{
		"https://host.example/sample.mkv",
		"https://host.example/keep.bin",
	}, "")
	if len(created) != 2 {
		t.Fatalf("staged %d tasks, want the refused one and the kept one", len(created))
	}

	if removed := a.ClearFiltered(nil); len(removed) != 1 {
		t.Fatalf("cleared %d links, want only the one being held", len(removed))
	}
	if n := len(a.FilteredLinks()); n != 0 {
		t.Errorf("%d links still held after clearing", n)
	}
	a.mu.Lock()
	left := len(a.tasks)
	a.mu.Unlock()
	if left != 1 {
		t.Errorf("%d tasks left, want the one link the filter never touched", left)
	}
}

// Every link reaches the list through stage, which asks the filter first, but a
// pasted page goes to the crawler before that. A rule written to keep this box
// away from a host would otherwise fetch from it once per paste.
func TestARefusedPageIsNeverFetched(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Crawl = true
		s.LinkFilter = rejectRule("nothing from there")
	})
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin", Name: "one.bin"}}}
	a.Crawler = fc

	created := a.AddLinks([]string{"https://pages.example/sample-gallery"}, "")

	if len(fc.seen) != 0 {
		t.Errorf("the crawler was sent to %v, which is the host the filter names", fc.seen)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks; the refusal is recorded, never dropped", len(created))
	}
	if !created[0].Skipped {
		t.Error("the refused page was staged rather than held")
	}
	if !strings.Contains(created[0].SkipReason, "nothing from there") {
		t.Errorf("the refused page reads %q, want the rule's reason", created[0].SkipReason)
	}
}

// A dispatch-time refusal has to reach the store and the browser. dispatchLocked
// settles a task under the lock and takes no copy, and StartTasks snapshots
// before it dispatches, so a refusal can be overwritten by a "queued, no error"
// copy and leave a task that says queued forever and carries no reason.
func TestWhatTheQueueRefusesReachesTheUser(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s *settings.Settings, base string)
		prepare func(t *testing.T, base string)
		// arm runs after the link is staged and before it is started. A rule
		// that already existed would have held the link at the paste box, so a
		// filtered link only reaches the collector when the rule came later.
		arm    func(t *testing.T, a *App, base string)
		link   string
		wantIn string
	}{
		{
			name:   "a link filter rule written after the link was staged",
			mutate: func(*settings.Settings, string) {},
			arm: func(t *testing.T, a *App, base string) {
				s := settings.Defaults()
				s.MaxConcurrent, s.MaxPerHost = 2, 1
				s.DownloadDir = base
				s.Crawl = false
				s.LinkFilter = rejectRule("sample files are not wanted here")
				if _, err := a.ApplySettings(s); err != nil {
					t.Fatal(err)
				}
			},
			link:   "https://host.example/sample.mkv",
			wantIn: "sample files are not wanted here",
		},
		{
			name:   "a destination that was already taken",
			mutate: func(s *settings.Settings, _ string) { s.CollisionPolicy = string(collide.Skip) },
			prepare: func(t *testing.T, base string) {
				if err := os.WriteFile(filepath.Join(base, "already.mkv"), []byte("mine"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			link:   "https://host.example/already.mkv",
			wantIn: "already exists",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, base := newRuleApp(t, tc.mutate)
			if tc.prepare != nil {
				tc.prepare(t, base)
			}
			created := a.AddLinks([]string{tc.link}, "")
			if len(created) != 1 {
				t.Fatalf("staged %d tasks", len(created))
			}
			id := created[0].ID
			if tc.arm != nil {
				tc.arm(t, a, base)
			}

			a.StartTasks(nil) // the "start everything" button

			// The list is rebuilt from the store, so it is the closest a test
			// can read to what the user is looking at.
			waitFor(t, "the refusal reaching the stored task", func() bool {
				stored, err := a.Store.All()
				if err != nil {
					return false
				}
				for _, s := range stored {
					if s.ID == id {
						return s.Status == core.StatusError && strings.Contains(s.Error, tc.wantIn)
					}
				}
				return false
			})
		})
	}
}

// The collector fires a HEAD at a plain file link while it waits, and the answer
// often lands after the user pressed start and the dispatcher refused the task.
// Free to write, the probe would replace the refusal with "offline: ...", or on
// a link that is fine with the empty string, leaving a failure with no reason.
func TestALateProbeDoesNotEraseTheReason(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.CollisionPolicy = string(collide.Skip)
	})
	// The probe is held until this test lets it answer, so the HEAD is in
	// flight while the user presses start and comes back after the dispatcher
	// has refused the task. Held rather than simulated through setAvailability,
	// because the claim is about the real path.
	answer := make(chan struct{})
	a.Probe = probeFunc(func(req *http.Request) (*http.Response, error) {
		<-answer
		return probeAnswer(req, http.StatusOK), nil
	})

	if err := os.WriteFile(filepath.Join(base, "already.mkv"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinks([]string{"https://host.example/already.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	a.StartTasks(nil)
	waitFor(t, "the task to be refused", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		t := a.tasks[created[0].ID]
		return t != nil && t.Status == core.StatusError
	})

	// Now the HEAD answers, last.
	close(answer)
	waitFor(t, "the probe's answer to land", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		t := a.tasks[created[0].ID]
		return t != nil && t.Online == core.AvailOnline
	})

	a.mu.Lock()
	live := *a.tasks[created[0].ID]
	a.mu.Unlock()
	if live.Status != core.StatusError {
		t.Errorf("status = %q, want the refusal to stand", live.Status)
	}
	if !strings.Contains(live.Error, "already exists") {
		t.Errorf("the task reads %q, want the reason it was refused for", live.Error)
	}
	if live.Online != core.AvailOnline {
		t.Errorf("availability = %q; what the probe learned about the link is worth keeping", live.Online)
	}
}

// The Packagizer runs before the task is staged, so the folder it picks is the
// one dirFor answers with. Run afterwards, its folder action would name a
// directory nothing writes to.
func TestPackagizerNamesThePackageAndThePlaceItLands(t *testing.T) {
	var target string
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		target = filepath.Join(base, "Films")
		yes := true
		prio := 2
		chunks := 6
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name: "films go together",
			Conditions: []rules.Condition{
				{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "films.example"},
			},
			Action: rules.Action{
				PackageName: "Films",
				DownloadDir: target,
				Comment:     "collected by the films rule",
				Priority:    &prio,
				Chunks:      &chunks,
				AutoExtract: &yes,
			},
		}}}
	})

	// Pasted without a package name, which is when derivePackage would step in
	// and overwrite the rule's answer.
	created := a.AddLinks([]string{"https://films.example/one.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]

	if got.Package != "Films" {
		t.Errorf("package = %q, want the rule's name kept rather than a guess", got.Package)
	}
	if got.Dir != target {
		t.Errorf("task folder = %q, want %q", got.Dir, target)
	}
	if dir := a.dirFor(got); dir != target {
		t.Errorf("dirFor answered %q; the rule ran too late to decide where the file goes", dir)
	}
	if got.Comment != "collected by the films rule" {
		t.Errorf("comment = %q", got.Comment)
	}
	if got.Priority != 2 {
		t.Errorf("priority = %d, want 2", got.Priority)
	}
	if got.Chunks != 6 {
		t.Errorf("chunks = %d, want 6", got.Chunks)
	}
	if got.AutoExtract == nil || !*got.AutoExtract {
		t.Errorf("auto-extract = %v, want the rule's own answer", got.AutoExtract)
	}
	if len(got.MatchedRules) != 1 || got.MatchedRules[0] != "films go together" {
		t.Errorf("matched rules = %v, want the record of why it landed here", got.MatchedRules)
	}
	// The task's own folder is taken verbatim and never joined with the package
	// subfolder, so a rule and the global setting cannot nest folders.
	if a.dirFor(got) != got.Dir {
		t.Error("the rule's folder was combined with something else")
	}
}

// Two things name a package. The rule is the more specific answer and it ran
// first, so a guess made from the batch afterwards would overwrite it and make a
// working rule look like one that does nothing.
func TestDerivedPackageLeavesRuleNamedTasksAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name: "one hoster, one package",
			Conditions: []rules.Condition{
				{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "films.example"},
			},
			Action: rules.Action{PackageName: "Films"},
		}}}
	})

	created := a.AddLinks([]string{
		"https://films.example/Great.Film.2026.part01.rar",
		"https://other.example/Great.Film.2026.part02.rar",
	}, "")
	if len(created) != 2 {
		t.Fatalf("staged %d tasks", len(created))
	}
	for _, task := range created {
		if hostOf(task.URL) == "films.example" && task.Package != "Films" {
			t.Errorf("the rule-named task ended up in %q, want Films", task.Package)
		}
		if hostOf(task.URL) == "other.example" && task.Package == "" {
			t.Error("the unnamed task got no package at all; the batch guess must still run for it")
		}
	}
}

// No backend accepts a destination file name: the engine is handed a directory
// and names the file itself. Writing a rule's name onto the task would leave the
// list showing one name while the disk holds another, and extraction and
// checksum verification both build their path from it.
func TestRenameActionIsNotAppliedToTheTask(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name:   "rename everything",
			Action: rules.Action{Filename: "renamed.bin", PackageName: "Renamed"},
		}}}
	})

	created := a.AddLinks([]string{"https://host.example/original.bin"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	if created[0].Name == "renamed.bin" {
		t.Error("a rule renamed the task; the file on disk keeps the backend's name, so the two would disagree")
	}
	// The same rule's other action lands, so this is one field left out rather
	// than the rule being ignored.
	if created[0].Package != "Renamed" {
		t.Errorf("package = %q, want the rest of the rule applied", created[0].Package)
	}
}

// The second way a link can fail to become a task. Folding it away is what the
// mirror set is for, and the reason is kept where the interface can ask for it.
func TestDuplicateLinkIsFoldedAwayWithATrace(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	const link = "https://host.example/one.bin"
	if created := a.AddLinks([]string{link}, ""); len(created) != 1 {
		t.Fatalf("first paste staged %d tasks", len(created))
	}
	// A different but equivalent spelling, which the mirror set normalises and a
	// raw string comparison would miss.
	if created := a.AddLinks([]string{"https://Host.Example:443/one.bin"}, ""); len(created) != 0 {
		t.Fatalf("second paste staged %d tasks, want the link folded away", len(created))
	}
	if n := len(a.Tasks()); n != 1 {
		t.Fatalf("%d tasks in the list, want one", n)
	}

	skipped := a.SkippedLinks()
	if len(skipped) != 1 {
		t.Fatalf("%d skipped links recorded, want the folded one traced", len(skipped))
	}
	if skipped[0].Reason == "" || skipped[0].Kind != "duplicate" {
		t.Errorf("skipped entry = %+v, want a kind and a reason", skipped[0])
	}
	if skipped[0].OfID == "" {
		t.Error("the trace does not say which task the link folded into")
	}

	a.ClearSkipped()
	if len(a.SkippedLinks()) != 0 {
		t.Error("the trace survived being cleared")
	}
}

// The mirror set outlives the batch it was built for, so it follows a deletion:
// otherwise a deleted download refuses its own re-add for the life of the
// process and the paste box appears to ignore the user.
func TestRemovedTaskStopsBlockingItsOwnLink(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	const link = "https://host.example/two.bin"
	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	a.Remove(created[0].ID, false)

	again := a.AddLinks([]string{link}, "")
	if len(again) != 1 {
		t.Fatalf("re-adding a removed link staged %d tasks, want 1", len(again))
	}
}

// SetHalted(false) disarms the stop mark, because a user resuming the queue has
// finished with it. A window ending at 06:00 is not the user, so it leaves their
// "finish this, then stop" alone.
func TestScheduleNeverClearsTheUsersOwnStop(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	a.mu.Lock()
	a.tasks["marked"] = &core.Task{ID: "marked", URL: "https://host.example/x.bin"}
	a.mu.Unlock()
	a.SetStopMark("marked")

	a.applySchedule(schedule.State{Paused: true})
	if !a.Queue().Halted {
		t.Fatal("a pause window did not halt the queue")
	}
	a.applySchedule(schedule.State{Paused: false})

	q := a.Queue()
	if q.Halted {
		t.Error("the queue stayed halted after the window ended")
	}
	if q.StopMark != "marked" {
		t.Errorf("stop mark = %q, want the user's own mark untouched by the schedule", q.StopMark)
	}
}

// The third halt in the app is set from a finished download rather than a click,
// so it has to be recorded as the user's own stop. Otherwise the next boundary,
// a nightly limit ending at 06:00, hands the queue a "not paused" and starts
// everything the mark was there to stop.
func TestTheStopMarkSurvivesTheNextBoundary(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.VerifyChecksums, s.Extract = false, false
	})
	a.mu.Lock()
	a.tasks["marked"] = &core.Task{ID: "marked", URL: "https://host.example/last.bin", Name: "last.bin"}
	a.mu.Unlock()
	a.SetStopMark("marked")

	a.onUpdate("marked", core.Update{Status: core.StatusDone})
	if !a.Queue().Halted {
		t.Fatal("the stop mark did not halt the queue")
	}

	// What the runner does at the next boundary where no window applies.
	a.applySchedule(a.scheduleBase())

	if !a.Queue().Halted {
		t.Error("a schedule boundary lifted the halt the stop mark had just set")
	}
}

// The same conflict from the other side: the timetable is evaluated against
// what the user set by hand, so a stop made at 03:00 is in force when a window
// ends and the window has nothing to release.
func TestManualHaltSurvivesTheEndOfAWindow(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.SpeedLimit = 4096 })

	a.SetHalted(true)
	base := a.scheduleBase()
	if !base.Paused {
		t.Error("the schedule's base does not report the halt the user set by hand")
	}
	if base.Limit != 4096 {
		t.Errorf("the schedule's base limit = %d, want the configured speed limit", base.Limit)
	}

	// What the runner does when a limit window closes over a hand-halted queue.
	a.applySchedule(a.scheduleBase())
	if !a.Queue().Halted {
		t.Error("the end of a window lifted a stop the user made by hand")
	}
}

// The timetable is the only writer of the limiter, so a saved settings page
// reaches it through the runner or not at all.
func TestSpeedLimitTakesEffectThroughTheSchedule(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.SpeedLimit = 1 << 20 })
	// Set wakes the runner, which applies on its own goroutine, so the limit
	// arrives shortly after the save rather than inside it.
	waitFor(t, "the saved speed limit reaching the limiter", func() bool { return a.Throttle.Limit() == 1<<20 })
}

// The runner owns a goroutine and is the only writer of the speed limit, so a
// save that built a new one would leave the old one applying the timetable the
// user just replaced, with two goroutines answering the limiter.
func TestSavingSettingsDoesNotBuildASecondRunner(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	first := a.sched
	for i, limit := range []int64{1 << 20, 0, 3 << 20} {
		s := a.Settings.Get()
		s.SpeedLimit = limit
		if _, err := a.ApplySettings(s); err != nil {
			t.Fatalf("save %d: %v", i+1, err)
		}
	}
	if a.sched != first {
		t.Error("a settings save replaced the schedule runner; the one it replaced is still running")
	}
	// The surviving runner is still the one wired to the limiter, which is what
	// the identity check above rests on.
	waitFor(t, "the last saved limit reaching the limiter", func() bool { return a.Throttle.Limit() == 3<<20 })
}

// The real Runner against a window covering this minute, which is what shows
// that New starts it and ApplySettings hands it the new timetable.
func TestPauseWindowHaltsTheQueue(t *testing.T) {
	now := time.Now()
	window := schedule.Entry{
		Name:   "right now",
		Days:   []time.Weekday{now.Weekday()},
		Start:  "00:00",
		End:    "23:59",
		Action: schedule.ActionPause,
	}
	// A window ending at 23:59 does not cover the last minute of the day.
	if now.Hour() == 23 && now.Minute() >= 59 {
		t.Skip("the covering window cannot be expressed in the last minute of the day")
	}
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.Schedule = []schedule.Entry{window} })

	waitFor(t, "the pause window halting the queue", func() bool { return a.Queue().Halted })

	st := a.ScheduleState()
	if !st.State.Paused {
		t.Error("the reported schedule state does not say the queue is paused")
	}
	if len(st.Entries) != 1 {
		t.Errorf("the reported timetable has %d rows", len(st.Entries))
	}
	if st.Next == nil {
		t.Error("no next boundary reported; the interface cannot say how long this lasts")
	}
}

// Why the task's flag is a pointer: a rule that switches unpacking off has to
// survive a global that is on, and with a plain bool "the rule said no" and "no
// rule had an opinion" are the same value.
func TestExtractionRuleOutranksTheGlobalSwitch(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name   string
		onTask *bool
		global bool
		want   bool
	}{
		{name: "no rule had an opinion, so the global decides", onTask: nil, global: true, want: true},
		{name: "and decides the other way too", onTask: nil, global: false, want: false},
		{name: "a rule that says no beats a global that says yes", onTask: &no, global: true, want: false},
		{name: "a rule that says yes beats a global that says no", onTask: &yes, global: false, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &core.Task{AutoExtract: tc.onTask}
			if got := extractWanted(task, settings.Settings{Extract: tc.global}); got != tc.want {
				t.Errorf("extractWanted = %v, want %v", got, tc.want)
			}
		})
	}
}

// The collision policy is honoured at the last moment before bytes move rather
// than at staging time, since the file it is about may appear in the folder
// while the link sits in the collector.
func TestSkipPolicyDoesNotDownloadOverAnExistingFile(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.CollisionPolicy = string(collide.Skip)
	})
	if err := os.WriteFile(filepath.Join(base, "already.mkv"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	created := a.AddLinks([]string{"https://host.example/already.mkv"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	a.StartTasks(nil)

	a.mu.Lock()
	live := a.tasks[created[0].ID]
	status, msg := live.Status, live.Error
	active := len(a.active)
	a.mu.Unlock()

	if active != 0 {
		t.Error("the download was started over a file that was already there")
	}
	if status != core.StatusError {
		t.Errorf("status = %q, want the task settled rather than left queued", status)
	}
	if !strings.Contains(msg, "already exists") {
		t.Errorf("the task reads %q, want it to name the file that was in the way", msg)
	}
	if body, err := os.ReadFile(filepath.Join(base, "already.mkv")); err != nil || string(body) != "mine" {
		t.Errorf("the existing file was touched: %q, %v", body, err)
	}
}

// waitFor polls a condition another goroutine satisfies: the schedule runner
// applying a window, or the dispatcher publishing what it settled.
//
// The deadline detects a hang rather than asserting a speed. A full
// `go test ./...` runs dozens of packages at once, so a few seconds of wall
// clock can be a fraction of a second of CPU for the goroutine being waited on.
// A generous deadline costs nothing when the condition is met, since the loop
// returns on the next tick.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never happened; nothing is driving it", what)
}
