package app

import (
	"io"
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
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	// The collector's HEAD, answered here instead of on the network. Turning
	// crawling off below was supposed to mean "no test needs a network", and this
	// was the hole in that promise: every staged link fired a real DNS lookup at
	// a host.example address, and the failure landed whenever it landed. That is
	// what made TestALateProbeDoesNotEraseTheReason fail on CI and pass here - a
	// second writer nobody had ordered, racing the one the test was about.
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

// TestFilteredLinkIsVisibleWithItsReason is the promise the filter is built on.
// JDownloader eats filtered links in silence: something is gone, nothing says
// what or why, and the user reports it as a bug in the paste box. A link this
// filter turns down has to be somewhere the user can find it, carrying the rule
// that stopped it and the reason that rule gave.
//
// Somewhere, not in the collector. It is held: kept and persisted, but out of the
// list, out of the queue and out of the counters — because a filter that is
// working would otherwise fill the collector with the junk it just caught.
func TestFilteredLinkIsVisibleWithItsReason(t *testing.T) {
	cases := []struct {
		name   string
		set    rules.Set
		wantIn []string
	}{
		{
			name: "the rule's own words, with the rule named alongside them",
			set:  rejectRule("sample files are not wanted here"),
			// Both halves have to be there: the reason is what the user reads, the
			// rule name is what they edit.
			wantIn: []string{"sample files are not wanted here", "no samples"},
		},
		{
			name:   "a rule that gave no reason still names itself",
			set:    rejectRule(""),
			wantIn: []string{"no samples"},
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
			if len(got.MatchedRules) != 1 || got.MatchedRules[0] != "no samples" {
				t.Errorf("matched rules = %v, want the one rule that caught it, as data and not only inside the sentence", got.MatchedRules)
			}
			if got.Resolver != "" {
				t.Errorf("resolver = %q; a refused link must not be resolved at all", got.Resolver)
			}
			// The holding area is what the interface lists, so it has to be the
			// same link and not merely a flag somewhere.
			held := a.FilteredLinks()
			if len(held) != 1 || held[0].ID != got.ID {
				t.Errorf("the holding area holds %d links, want the one that was refused", len(held))
			}
		})
	}
}

// TestAHeldLinkCannotBeStarted is the other half of holding it. "Start
// everything" reaches every collected task, so a link parked with a reason and
// nothing else stopping it is a filter one button undoes.
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

// TestRestoreLetsALinkPastTheRuleThatCaughtIt is the point of the holding area.
// The commonest reason to open it is that the rule turned out to be too broad,
// and the queue asks the filter one final time before any bytes move — so a
// Restore that only un-parked the link would hand it straight back to the rule
// that caught it, with the same sentence, and read as a button that does nothing.
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
		t.Error("the reason was dropped; it is the record that the user overruled the filter, and the queue reads it")
	}
	if len(a.FilteredLinks()) != 0 {
		t.Error("the link is still in the holding area after being restored")
	}

	// dispatchLocked settles what it refuses inside StartTasks, so if the rule
	// were still in the way the refusal would already be on the task here. No
	// waiting, and therefore nothing for a slow host to make flaky.
	a.StartTasks([]string{id})
	a.mu.Lock()
	live := *a.tasks[id]
	a.mu.Unlock()
	if live.Status == core.StatusError && strings.Contains(live.Error, "sample files are not wanted here") {
		t.Fatal("the queue refused the restored link with the very reason the user had already overruled")
	}
}

// TestClearFilteredEmptiesOnlyTheHoldingArea guards the button next to Restore.
// Clear is offered on a list of links somebody has decided they do not want, and
// a Clear that reached past that list into the collector would delete work the
// user is in the middle of.
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

// TestARefusedPageIsNeverFetched is the entrance the collector's own funnel
// does not cover. Every link reaches the list through stage, and stage asks the
// filter first — but a pasted page is handed to the crawler before that, so a
// rule written to keep this box away from a host would fetch from it once per
// paste and only then refuse what came back.
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

// TestWhatTheQueueRefusesReachesTheUser is the half of the dispatch-time
// refusal the check above cannot see. dispatchLocked settles a task under the
// lock and takes no copy of it, so nothing was ever written to the store or sent
// to a browser — and StartTasks, which snapshots before it dispatches, then
// writes its own "queued, no error" copy over the top. The user is left with a
// task that says queued forever, comes back paused after a restart, and carries
// no reason anywhere: the same silent disappearance the staging record exists to
// prevent, moved one button along.
func TestWhatTheQueueRefusesReachesTheUser(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s *settings.Settings, base string)
		prepare func(t *testing.T, base string)
		// arm runs after the link is staged and before it is started. It is how
		// the filter case reaches the queue at all: a rule that already existed
		// would have held the link at the paste box, so the only way a filtered
		// link is ever in the collector is that the rule was written after it.
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

			// The store is what the list is rebuilt from, so it is the closest thing
			// to what the user is looking at that a test can read.
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

// TestALateProbeDoesNotEraseTheReason is the third writer of the error field
// and the one that arrives last. The collector fires a HEAD at a plain file link
// while it waits, and that answer routinely lands after the user has pressed
// start and the dispatcher has already refused the task. Left free to write, the
// probe replaces the refusal with "offline: ..." — or, on a link that turned out
// to be perfectly fine, with the empty string, leaving a failed download that
// says nothing at all about why.
func TestALateProbeDoesNotEraseTheReason(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.CollisionPolicy = string(collide.Skip)
	})
	// The probe is held until this test lets it answer, which is the whole
	// scenario: the HEAD is in flight while the user presses start, and it comes
	// back after the dispatcher has already refused the task. Held rather than
	// simulated by calling setAvailability directly - the point is that the real
	// path cannot erase the reason, and a test that stands in for that path
	// proves nothing about it.
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
		t.Errorf("availability = %q; what the probe learned about the link is still worth keeping", live.Online)
	}
}

// TestPackagizerNamesThePackageAndThePlaceItLands is the other half of the
// order that matters: the Packagizer has to run before the task is staged, so
// the folder it picks is the folder dirFor answers with. Run afterwards, its
// folder action names a directory nothing ever writes to.
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

	// Pasted without a package name, which is exactly when derivePackage would
	// otherwise step in and overwrite the rule's answer.
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
		t.Errorf("matched rules = %v, want the audit trail for why it landed here", got.MatchedRules)
	}
	// The task's own folder is taken verbatim and never joined with the package
	// subfolder, so a rule and the global setting cannot nest duplicates.
	if a.dirFor(got) != got.Dir {
		t.Error("the rule's folder was combined with something else")
	}
}

// TestDerivedPackageLeavesRuleNamedTasksAlone pins the conflict between the two
// things that name a package. The rule is the more specific answer and it ran
// first; a guess made from the batch afterwards would silently overwrite it, and
// the user would see a rule that works look like one that does nothing.
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

// TestRenameActionIsNotAppliedToTheTask documents a deliberate omission rather
// than an oversight. No backend accepts a destination file name — the engine is
// handed a directory and names the file itself — so writing a rule's name onto
// the task would leave the list showing one name while the disk holds another,
// and extraction and checksum verification both build their path from that name.
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
	// The same rule's other action still lands, so this is the one field left
	// out and not the rule being ignored.
	if created[0].Package != "Renamed" {
		t.Errorf("package = %q, want the rest of the rule applied", created[0].Package)
	}
}

// TestDuplicateLinkIsFoldedAwayWithATrace covers the second way a link can fail
// to become a task. Folding it is the point of the mirror set, but folding it in
// silence is the behaviour this project refuses, so the reason is kept where the
// interface can ask for it.
func TestDuplicateLinkIsFoldedAwayWithATrace(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	const link = "https://host.example/one.bin"
	if created := a.AddLinks([]string{link}, ""); len(created) != 1 {
		t.Fatalf("first paste staged %d tasks", len(created))
	}
	// Pasted again in a different but equivalent spelling, which is what the
	// mirror set normalises and a raw string comparison would miss.
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

// TestRemovedTaskStopsBlockingItsOwnLink is the failure the mirror set brings
// with it: a set that outlives the batch it was built for has to follow a
// deletion, or a deleted download refuses its own re-add for the life of the
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

// TestScheduleNeverClearsTheUsersOwnStop is the interaction that would otherwise
// bite quietly. SetHalted(false) also disarms the stop mark, on the reasoning
// that a user resuming the queue has finished with it — but a window ending at
// 06:00 is not the user, and throwing away their "finish this, then stop" for a
// reason nobody could connect to anything they did is the worst kind of bug.
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

// TestTheStopMarkSurvivesTheNextBoundary is the third halt in the app and the
// one that is easiest to forget. It is set from a finished download rather than
// from a click, so unless it is recorded as the user's own stop it is invisible
// to the state the runner falls back to — and the next boundary that changes
// anything, a nightly limit ending at 06:00, hands the queue a "not paused" it
// never asked for and starts everything the mark was there to stop.
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

// TestManualHaltSurvivesTheEndOfAWindow is the same conflict from the other
// side. The timetable is evaluated against what the user set by hand, so a stop
// made at 03:00 is already in force when a window ends and the window has
// nothing to release.
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

// TestSpeedLimitTakesEffectThroughTheSchedule proves the runner is really wired
// rather than merely constructed: the limiter is written by the timetable, so a
// saved settings page reaches it only by going through the runner.
func TestSpeedLimitTakesEffectThroughTheSchedule(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.SpeedLimit = 1 << 20 })
	// Set wakes the runner, which applies on its own goroutine, so the limit
	// arrives shortly after the save rather than inside it.
	waitFor(t, "the saved speed limit reaching the limiter", func() bool { return a.Throttle.Limit() == 1<<20 })
}

// TestSavingSettingsDoesNotBuildASecondRunner is the shape a timetable leak
// would take. The runner owns a goroutine and is the only writer of the speed
// limit, so a save that built a new one would leave the old one alive, applying
// the timetable the user just replaced, and the limiter would be handed two
// answers by two goroutines for the rest of the process.
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
	// makes the identity check above mean anything.
	waitFor(t, "the last saved limit reaching the limiter", func() bool { return a.Throttle.Limit() == 3<<20 })
}

// TestPauseWindowHaltsTheQueue runs the real Runner against a window that covers
// this very minute, which is the only way to show that New starts it and that
// ApplySettings hands it the new timetable.
func TestPauseWindowHaltsTheQueue(t *testing.T) {
	now := time.Now()
	window := schedule.Entry{
		Name:   "right now",
		Days:   []time.Weekday{now.Weekday()},
		Start:  "00:00",
		End:    "23:59",
		Action: schedule.ActionPause,
	}
	// A window ending at 23:59 does not cover the last minute of the day, and a
	// test that fails once a day at midnight is worse than no test.
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

// TestExtractionRuleOutranksTheGlobalSwitch is why the task's flag is a pointer.
// A rule that deliberately switches unpacking off has to survive a global that
// is on, and with a plain bool "the rule said no" and "no rule had an opinion"
// are the same value.
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

// TestSkipPolicyDoesNotDownloadOverAnExistingFile is the one collision policy
// this app can honour today, and it is honoured at the last moment before bytes
// move rather than at staging time: the file it is about may well have appeared
// in the folder while the link sat in the collector.
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
// The deadline is a HANG DETECTOR, not a speed assertion. It was three seconds
// and that turned out to be an assertion about how fast the machine is: a full
// `go test ./...` runs 46 packages at once, so three seconds of wall clock can
// be a fraction of a second of CPU for the one goroutine being waited on, and
// TestExpandYtdlpVariantsFamilyStillRenamesThePackageOnceNamed failed at 3.51s
// on a loaded run while passing on every unloaded one. Waiting longer costs
// nothing when the condition is met - the loop returns on the next 10ms tick -
// and only spends the extra time on a failure that was going to fail anyway.
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
