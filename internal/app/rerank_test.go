package app

// Which backend a task goes to when it is handed out again. A backend recorded
// at staging or in an earlier run says little about the moment of dispatch: a
// debrid account that was benched then, or not yet set up, may be usable, and
// JDownloader's free mode is the last thing that should get a hoster link
// while it is.

import (
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const rerankHost = "rerank.example"

// routeSpy records what the dispatcher asks of one backend on a channel all
// spies of a test share, so a test reads the calls in the order they came.
type routeSpy struct {
	name   string
	events chan string
}

func (s *routeSpy) Download(id, _ string, _ map[string]string, _ int) {
	s.events <- s.name + " download " + id
}
func (s *routeSpy) Pause(string)             {}
func (s *routeSpy) Resume(id string)         { s.events <- s.name + " resume " + id }
func (s *routeSpy) Remove(id string, _ bool) { s.events <- s.name + " remove " + id }

// slowSpy is a backend that takes until release is closed to let go of a task,
// like a JDownloader that is slow to answer.
type slowSpy struct {
	routeSpy
	release chan struct{}
}

func (s *slowSpy) Remove(id string, _ bool) {
	s.events <- s.name + " remove " + id
	<-s.release
}

// slowJD puts a slow JDownloader in place of the spy rerankApp set up.
func slowJD(a *App, events chan string) chan struct{} {
	release := make(chan struct{})
	a.bmu.Lock()
	a.jd = &slowSpy{routeSpy{name: "jd", events: events}, release}
	a.bmu.Unlock()
	return release
}

// settle collects the backend calls until none has come for a quarter second.
func settle(events chan string) map[string]bool {
	got := map[string]bool{}
	for {
		select {
		case e := <-events:
			got[e] = true
		case <-time.After(250 * time.Millisecond):
			return got
		}
	}
}

// rerankApp ranks TorBox above JDownloader for every link on rerankHost, the
// way a debrid service outranks JD's free mode for a file hoster.
func rerankApp(t *testing.T) (*App, chan string) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	s.MaxRetries = 0
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 16)
	a.bmu.Lock()
	a.debrid["torbox"] = &routeSpy{name: "torbox", events: events}
	a.jd = &routeSpy{name: "jd", events: events}
	a.bmu.Unlock()
	a.Registry.Register(pinResolver{id: "torbox", host: rerankHost, prio: 50})
	a.Registry.Register(pinResolver{id: "jd", host: rerankHost, prio: 10})
	return a, events
}

// rerankTask files one task on rerankHost. The URL has no file extension, so
// the direct download does not claim it.
func rerankTask(a *App, t core.Task) *core.Task {
	t.URL = "https://" + rerankHost + "/file/" + t.ID
	t.Enabled = true
	if t.Status == "" {
		t.Status = core.StatusQueued
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	task := &t
	a.tasks[t.ID] = task
	if t.Status == core.StatusQueued {
		a.queue = append(a.queue, t.ID)
	}
	return task
}

func nextEvent(t *testing.T, events chan string) string {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no backend was asked for anything")
		return ""
	}
}

func wantEvents(t *testing.T, events chan string, want ...string) {
	t.Helper()
	for _, w := range want {
		if got := nextEvent(t, events); got != w {
			t.Fatalf("backend call %q, want %q", got, w)
		}
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected backend call %q after %v", e, want)
	case <-time.After(250 * time.Millisecond):
	}
}

// A row that JD was picked for at staging, or in an earlier run, goes to the
// debrid service ranked above JD once that service can take it, and JD is told
// to drop the package it may still hold, so the file is not fetched twice.
func TestAFreshDispatchPrefersAUsableDebridOverARecordedJD(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	task := rerankTask(a, core.Task{ID: "r1", Resolver: "jd", Mode: core.ModeFree})

	dispatchNow(a)

	// Two backends, so the two calls may come in either order.
	calls := map[string]bool{nextEvent(t, events): true, nextEvent(t, events): true}
	if !calls["jd remove r1"] || !calls["torbox download r1"] {
		t.Fatalf("backend calls %v, want JD to drop the task and TorBox to take it", calls)
	}
	wantEvents(t, events)
	a.mu.Lock()
	got, mode := task.Resolver, task.Mode
	a.mu.Unlock()
	if got != "torbox" {
		t.Errorf("Resolver = %q, want torbox", got)
	}
	if mode != core.ModePremium {
		t.Errorf("Mode = %q, want %q for a debrid service", mode, core.ModePremium)
	}
}

// JD letting go of the package and the debrid service taking the link do not
// wait on each other, so a JDownloader that is slow to answer holds up neither
// the download nor a pause that comes meanwhile.
func TestTheNewBackendStartsWithoutWaitingForTheOldOne(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	release := slowJD(a, events)
	defer close(release)
	rerankTask(a, core.Task{ID: "r1", Resolver: "jd"})

	dispatchNow(a)

	got := map[string]bool{nextEvent(t, events): true, nextEvent(t, events): true}
	if !got["jd remove r1"] || !got["torbox download r1"] {
		t.Fatalf("backend calls %v, want TorBox to start while JD is still letting go", got)
	}
}

// A service that turned a link down is not asked again while the account of
// the next one is benched. The link goes on down the chain, never back up,
// where the same refusal would send it round again.
func TestALinkTurnedDownGoesOnDownTheChain(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	a.bmu.Lock()
	a.debrid["debridlink"] = &routeSpy{name: "debridlink", events: events}
	a.bmu.Unlock()
	a.Registry.Register(pinResolver{id: "debridlink", host: rerankHost, prio: 47})
	if _, started := a.acctHealthTracker().ReportFailure("debridlink", "", accounts.HealthTempDisabled, "seed", time.Hour); !started {
		t.Fatal("setup: the bench did not start")
	}
	rerankTask(a, core.Task{ID: "r1"})
	dispatchNow(a)
	wantEvents(t, events, "torbox download r1")

	a.onUpdate("r1", core.Update{Status: core.StatusError, Err: "torbox: not this link", Unsupported: true, Reason: core.ReasonUnsupported})

	got := settle(events)
	if got["torbox download r1"] {
		t.Fatalf("backend calls %v: the link went back to the service that had just turned it down", got)
	}
	if !got["jd download r1"] {
		t.Fatalf("backend calls %v, want JDownloader to take the link", got)
	}
}

// When everything below the service that turned the link down is benched, the
// link waits for those accounts rather than failing as if nothing could take
// it.
func TestALinkTurnedDownWaitsForTheBenchedAccountsBelow(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	a.bmu.Lock()
	a.debrid["debridlink"] = &routeSpy{name: "debridlink", events: events}
	a.bmu.Unlock()
	isolateResolvers(a,
		pinResolver{id: "torbox", host: rerankHost, prio: 50},
		pinResolver{id: "debridlink", host: rerankHost, prio: 47},
	)
	if _, started := a.acctHealthTracker().ReportFailure("debridlink", "", accounts.HealthTempDisabled, "seed", time.Hour); !started {
		t.Fatal("setup: the bench did not start")
	}
	task := rerankTask(a, core.Task{ID: "r1"})
	dispatchNow(a)
	wantEvents(t, events, "torbox download r1")

	a.onUpdate("r1", core.Update{Status: core.StatusError, Err: "torbox: not this link", Unsupported: true, Reason: core.ReasonUnsupported})

	if got := settle(events); got["torbox download r1"] {
		t.Fatalf("backend calls %v: the link went back to the service that had just turned it down", got)
	}
	a.mu.Lock()
	status, waiting, msg := task.Status, task.Waiting, task.Error
	a.mu.Unlock()
	if status != core.StatusQueued || waiting != core.WaitingAccount {
		t.Fatalf("status %q (%q), waiting %q; want the task queued and waiting for the account", status, msg, waiting)
	}
}

// Bytes already fetched belong to the backend that fetched them.
func TestARowWithProgressStaysOnItsBackend(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	rerankTask(a, core.Task{ID: "r1", Resolver: "jd", Loaded: 1 << 20, Size: 4 << 20})

	dispatchNow(a)

	wantEvents(t, events, "jd download r1")
}

// A backend that just handed the task down the chain has had its say. Asking
// it again on the next pass would bounce the task between the two for ever.
func TestATaskHandedDownTheChainStaysWhereItLanded(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	rerankTask(a, core.Task{ID: "r1"})

	dispatchNow(a)
	wantEvents(t, events, "torbox download r1")

	// The reason keeps the account out of it, as it is when a service turns
	// down a link.
	a.onUpdate("r1", core.Update{Status: core.StatusError, Err: "torbox: not this host", Unsupported: true, Reason: core.ReasonUnsupported})

	// Two backends, so the two calls may come in either order.
	got := map[string]bool{nextEvent(t, events): true, nextEvent(t, events): true}
	if !got["torbox remove r1"] || !got["jd download r1"] {
		t.Fatalf("backend calls %v, want TorBox to drop the task and JD to take it", got)
	}
	wantEvents(t, events)
}

// The video rows of a yt-dlp link and torrents carry their backend as part of
// what they are, so a debrid service ranked above never takes them over.
func TestRowsWhoseBackendIsPartOfThemKeepIt(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	rerankTask(a, core.Task{ID: "v1", Resolver: "jd", Variant: "video:best"})
	rerankTask(a, core.Task{ID: "h1", Resolver: "jd", InfoHash: "0123456789abcdef0123456789abcdef01234567"})
	rerankTask(a, core.Task{ID: "s1", Resolver: "jd", TorrentFiles: []core.TorrentFile{{Path: "a.bin", Selected: true}}})

	dispatchNow(a)

	got := map[string]bool{}
	for range 3 {
		got[nextEvent(t, events)] = true
	}
	for _, want := range []string{"jd download v1", "jd download h1", "jd download s1"} {
		if !got[want] {
			t.Errorf("backend calls %v, want %q among them", got, want)
		}
	}
}

// A pin set on a paused task decides where it resumes. Resuming on the backend
// it was paused on would ignore the choice the user just made.
func TestAPinOnAPausedTaskTakesEffectOnResume(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	task := rerankTask(a, core.Task{ID: "r1", Resolver: "jd", Status: core.StatusPaused, Loaded: 4096, Size: 1 << 20})
	a.mu.Lock()
	a.started["r1"] = true
	a.mu.Unlock()

	if err := a.PinResolver([]string{"r1"}, "torbox"); err != nil {
		t.Fatal(err)
	}
	a.Resume("r1")

	wantEvents(t, events, "jd remove r1", "torbox download r1")
	a.mu.Lock()
	loaded := task.Loaded
	a.mu.Unlock()
	if loaded != 0 {
		t.Errorf("Loaded = %d after moving to another backend, want 0: those bytes were JD's", loaded)
	}
}

// A task the hard stop put back in the queue is started again by the next pass,
// and that pass goes to the pinned backend too.
func TestAPinOnARequeuedTaskMovesItBeforeItStartsAgain(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	rerankTask(a, core.Task{ID: "r1", Resolver: "jd"})
	a.mu.Lock()
	a.started["r1"] = true
	// A halt by hand, as SetHalted makes it: a schedule pass recomputes halted
	// from manualHalt and would otherwise lift it while the pin has a.mu let go.
	a.halted, a.manualHalt = true, true
	a.mu.Unlock()

	if err := a.PinResolver([]string{"r1"}, "torbox"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.halted, a.manualHalt = false, false
	queued := slices.Contains(a.queue, "r1")
	a.mu.Unlock()
	if !queued {
		t.Fatal("the task left the queue while it moved to the pinned backend")
	}
	dispatchNow(a)

	wantEvents(t, events, "jd remove r1", "torbox download r1")
}

// A resume that comes while the pin is still taking the task off its old
// backend waits for that, so the pinned backend never starts beside the old
// one.
func TestResumingDuringAPinMoveWaitsForTheOldBackend(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	release := slowJD(a, events)
	rerankTask(a, core.Task{ID: "r1", Resolver: "jd", Status: core.StatusPaused, Loaded: 4096, Size: 1 << 20})
	a.mu.Lock()
	a.started["r1"] = true
	a.mu.Unlock()

	pinned := make(chan error, 1)
	go func() { pinned <- a.PinResolver([]string{"r1"}, "torbox") }()
	if got := nextEvent(t, events); got != "jd remove r1" {
		close(release)
		t.Fatalf("backend call %q, want JD to let go first", got)
	}
	a.Resume("r1")
	select {
	case e := <-events:
		close(release)
		t.Fatalf("backend call %q while JD still held the task", e)
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	if err := <-pinned; err != nil {
		t.Fatal(err)
	}

	wantEvents(t, events, "torbox download r1")
}

// A pin naming the backend the task already runs on changes nothing about how
// it resumes, and neither does taking a pin off.
func TestAPinThatAgreesWithTheBackendKeepsTheProgress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, before, after string }{
		{"pinned to its own backend", "", "jd"},
		{"unpinned", "torbox", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, events := rerankApp(t)
			rerankTask(a, core.Task{ID: "r1", Resolver: "jd", ResolverPin: tc.before, Status: core.StatusPaused, Loaded: 4096, Size: 1 << 20})
			a.mu.Lock()
			a.started["r1"] = true
			a.mu.Unlock()

			if err := a.PinResolver([]string{"r1"}, tc.after); err != nil {
				t.Fatal(err)
			}
			a.Resume("r1")

			wantEvents(t, events, "jd resume r1")
		})
	}
}

// A retry date belongs to a failure. Once the task runs again, a date from
// weeks ago on the row only says something untrue.
func TestARestartedTaskShowsNoRetryDate(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	weeksAgo := time.Now().Add(-21 * 24 * time.Hour)
	task := rerankTask(a, core.Task{ID: "r1", Status: core.StatusError, Retries: 3, NextTry: weeksAgo})

	a.RestartTasks([]string{"r1"})

	nextEvent(t, events)
	a.mu.Lock()
	next := task.NextTry
	a.mu.Unlock()
	if !next.IsZero() {
		t.Errorf("NextTry = %s after the restart, want none", next)
	}
}

func TestAStartedTaskShowsNoRetryDate(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	weeksAgo := time.Now().Add(-21 * 24 * time.Hour)
	task := rerankTask(a, core.Task{ID: "r1", Resolver: "torbox", Retries: 3, NextTry: weeksAgo})

	dispatchNow(a)

	wantEvents(t, events, "torbox download r1")
	a.mu.Lock()
	next := task.NextTry
	a.mu.Unlock()
	if !next.IsZero() {
		t.Errorf("NextTry = %s on a running task, want none", next)
	}
}

// A task that one backend hands down the chain leaves that backend before it
// starts on the next. Two debrid services both give their link to the engine
// under the task's id, and a Remove that came after the new start would take
// the new transfer with it.
func TestTheNextBackendStartsOnlyOnceTheLastHasLetGo(t *testing.T) {
	t.Parallel()
	a, events := rerankApp(t)
	release := make(chan struct{})
	a.bmu.Lock()
	a.debrid["torbox"] = &slowSpy{routeSpy{name: "torbox", events: events}, release}
	a.debrid["debridlink"] = &routeSpy{name: "debridlink", events: events}
	a.bmu.Unlock()
	a.Registry.Register(pinResolver{id: "debridlink", host: rerankHost, prio: 40})
	rerankTask(a, core.Task{ID: "r1", Resolver: "torbox", Status: core.StatusRunning})
	a.mu.Lock()
	a.active["r1"], a.started["r1"] = true, true
	a.mu.Unlock()

	handed := make(chan struct{})
	go func() {
		defer close(handed)
		a.onUpdate("r1", core.Update{Status: core.StatusError, Err: "torbox: not this link", Unsupported: true})
	}()

	if got := nextEvent(t, events); got != "torbox remove r1" {
		t.Fatalf("backend call %q, want TorBox to let go of the task first", got)
	}
	select {
	case e := <-events:
		t.Fatalf("%q while TorBox was still letting go of the task", e)
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	wantEvents(t, events, "debridlink download r1")
	<-handed
}
