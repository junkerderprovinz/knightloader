package app

// Pinning one task to one backend. The two rules pull in opposite directions:
// the pin beats the dispatcher's ranking, and it does not beat account health.
// A pin the ranking can overrule is not a pin, and a pin that gets past a
// benched account turns the health mechanism off one row at a time.

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const pinHost = "pinned.example"

// pinResolver matches one host at a priority the test chooses. hostResolver in
// stallwatch_test.go is fixed at 90, and this file needs two backends claiming
// the same link at different ranks, or a honoured pin and a ranking that
// happens to agree look identical.
type pinResolver struct {
	id   string
	host string
	prio int
}

func (r pinResolver) Info() resolver.Info { return resolver.Info{ID: r.id, Prio: r.prio} }
func (r pinResolver) Match(raw string) bool {
	return strings.HasPrefix(raw, "https://"+r.host+"/")
}
func (pinResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// pinBackend records which task it was handed, where capBackend records the
// connection count.
type pinBackend struct{ got chan string }

func (b *pinBackend) Download(taskID, _ string, _ map[string]string, _ int) { b.got <- taskID }
func (b *pinBackend) Pause(string)                                          {}
func (b *pinBackend) Resume(string)                                         {}
func (b *pinBackend) Remove(string, bool)                                   {}

// pinApp registers two backends that both claim every link on pinHost, at
// different ranks: alldebrid above torbox. An unpinned task therefore lands on
// alldebrid, and a task that ends up on torbox got there because of the pin
// and for no other reason.
func pinApp(t *testing.T) (*App, map[string]*pinBackend) {
	t.Helper()
	a := newQueueApp(t)
	return a, wirePinBackends(t, a)
}

// wirePinBackends is pinApp's setup, for an app a test built itself.
func wirePinBackends(t *testing.T, a *App) map[string]*pinBackend {
	t.Helper()
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	// The volume guard is off, so nothing here depends on how much room the
	// machine running it has.
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	// No automatic retry, so a task settled as failed stays settled instead of
	// arming a timer that outlives the test.
	s.MaxRetries = 0
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	bes := map[string]*pinBackend{}
	a.bmu.Lock()
	for _, id := range []string{"alldebrid", "alldebrid#work", "torbox"} {
		be := &pinBackend{got: make(chan string, 4)}
		bes[id] = be
		a.debrid[id] = be
	}
	a.bmu.Unlock()
	a.Registry.Register(pinResolver{id: "alldebrid", host: pinHost, prio: 90})
	a.Registry.Register(pinResolver{id: "torbox", host: pinHost, prio: 50})
	return bes
}

// queuePinned stages one queued task carrying a pin (or none, for the control
// cases).
func queuePinned(a *App, id, pin string) {
	a.mu.Lock()
	a.tasks[id] = &core.Task{
		ID: id, URL: "https://" + pinHost + "/" + id + ".bin", Name: id + ".bin",
		Status: core.StatusQueued, Enabled: true, ResolverPin: pin,
	}
	a.queue = append(a.queue, id)
	a.mu.Unlock()
}

// wantHandled waits for one backend to be handed the named task.
func wantHandled(t *testing.T, be *pinBackend, id string) {
	t.Helper()
	select {
	case got := <-be.got:
		if got != id {
			t.Fatalf("the backend was handed %q, want %q", got, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the backend was never handed %q", id)
	}
}

// wantNothingHandled proves the other backend was not used instead. The wait is
// the assertion: dispatchLocked hands a task over as `go be.Download(...)`, so
// reading the channel the instant the pass returns would only show that a
// goroutine has not been scheduled yet.
func wantNothingHandled(t *testing.T, be *pinBackend, why string) {
	t.Helper()
	select {
	case got := <-be.got:
		t.Fatalf("%s: it was handed %q anyway", why, got)
	case <-time.After(250 * time.Millisecond):
	}
}

// The control: without it every assertion below could be explained by the
// ranking already agreeing with the pin.
func TestAnUnpinnedTaskFollowsTheRanking(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	queuePinned(a, "p1", "")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	wantHandled(t, bes["alldebrid"], "p1")
	wantNothingHandled(t, bes["torbox"], "the lower-ranked backend took an unpinned task")
}

// A pinned task goes to the backend it names, so a link that only one service
// can fetch does not have to be pasted again with the instance switched over.
func TestAPinnedTaskGoesToTheBackendItNames(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	queuePinned(a, "p1", "torbox")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	wantHandled(t, bes["torbox"], "p1")
	wantNothingHandled(t, bes["alldebrid"], "the higher-ranked backend took a task pinned to another one")

	a.mu.Lock()
	got := a.tasks["p1"].Resolver
	a.mu.Unlock()
	if got != "torbox" {
		t.Errorf("Resolver = %q after dispatch, want %q", got, "torbox")
	}
}

// The limit that keeps the pin from being an off switch for account health. The
// wrong outcome is not the failure but the app fetching the link through the
// healthy backend beside it, which would make the pin decorative.
func TestAPinnedBackendWithABenchedAccountFailsWhereItCanBeSeen(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	a.acctHealthTracker().ReportFailure("alldebrid", "", accounts.HealthInvalid, "test", 0)
	queuePinned(a, "p1", "alldebrid")

	a.mu.Lock()
	a.dispatchLocked()
	status := a.tasks["p1"].Status
	reason := a.tasks["p1"].Reason
	msg := a.tasks["p1"].Error
	a.mu.Unlock()

	wantNothingHandled(t, bes["torbox"], "a task pinned to a benched backend was diverted to a healthy one")
	wantNothingHandled(t, bes["alldebrid"], "a task was handed to a backend whose account is not usable")
	if status != core.StatusError {
		t.Errorf("status = %q, want %q; a pinned task with nowhere to go fails visibly", status, core.StatusError)
	}
	if reason != core.ReasonAuth {
		t.Errorf("reason = %q, want %q", reason, core.ReasonAuth)
	}
	if !strings.Contains(msg, "alldebrid") {
		t.Errorf("error = %q, want it to name the backend the task was pinned to", msg)
	}
}

// The other failure gets its own sentence because it needs a different fix:
// waiting never mends a pin pointed at a backend that does not take this link.
func TestAPinNamingABackendThatCannotTakeTheLinkSaysSo(t *testing.T) {
	a, _ := pinApp(t)
	a.Registry.Register(pinResolver{id: "ytdlp", host: "elsewhere.example", prio: 70})
	queuePinned(a, "p1", "ytdlp")

	a.mu.Lock()
	a.dispatchLocked()
	status := a.tasks["p1"].Status
	reason := a.tasks["p1"].Reason
	a.mu.Unlock()

	if status != core.StatusError {
		t.Errorf("status = %q, want %q", status, core.StatusError)
	}
	if reason != core.ReasonUnsupported {
		t.Errorf("reason = %q, want %q", reason, core.ReasonUnsupported)
	}
}

// Honouring the pin at dispatch and forgetting it in the fallback would move
// the task to another backend the first time the pinned one said "not mine".
func TestAPinnedTaskNeverWalksTheFallbackChain(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	queuePinned(a, "p1", "alldebrid")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()
	wantHandled(t, bes["alldebrid"], "p1")

	a.onUpdate("p1", core.Update{Status: core.StatusError, Err: "not my business", Unsupported: true})

	wantNothingHandled(t, bes["torbox"], "a pinned task was handed on to the next backend in the chain")
	a.mu.Lock()
	got := a.tasks["p1"].Resolver
	reason := a.tasks["p1"].Reason
	a.mu.Unlock()
	if got != "alldebrid" {
		t.Errorf("Resolver = %q after the pinned backend refused the link, want it left on %q", got, "alldebrid")
	}
	if reason != core.ReasonUnsupported {
		t.Errorf("reason = %q, want %q", reason, core.ReasonUnsupported)
	}
}

// Somebody who writes "alldebrid" means their AllDebrid subscription, not one
// key of it, which is how settings.ResolverOrder is matched too. Otherwise a pin
// would break as soon as a second key turned the slot ids into service#account.
func TestAPinMayNameTheServiceAndReachOneOfItsAccounts(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	// Only the named account of the service is registered, so a pin that
	// insisted on an exact id match would find nothing.
	a.Registry.Unregister("alldebrid")
	a.Registry.Register(pinResolver{id: "alldebrid#work", host: pinHost, prio: 90})
	queuePinned(a, "p1", "alldebrid")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	wantHandled(t, bes["alldebrid#work"], "p1")
	wantNothingHandled(t, bes["torbox"], "a service-wide pin fell through to another service")
}

// The dropdown offers what can take the link, best first, and names a debrid
// service the way the accounts page does. The HTTP fallback is never offered:
// it would save a hoster's page.
func TestPinChoicesListWhatCanTakeTheLinkBestFirst(t *testing.T) {
	t.Parallel()
	a, _ := pinApp(t)
	queuePinned(a, "p1", "")

	got := a.PinChoices([]string{"p1"})
	want := []PinChoice{{ID: "alldebrid", Label: "AllDebrid"}, {ID: "torbox", Label: "TorBox"}, {ID: "direct"}}
	if !slices.Equal(got, want) {
		t.Fatalf("PinChoices = %v, want %v", got, want)
	}
}

// Over several rows only what every one of them can go to is offered, so a
// pin chosen for all of them fails none.
func TestPinChoicesOverSeveralRowsAreWhatTheyShare(t *testing.T) {
	t.Parallel()
	a, _ := pinApp(t)
	queuePinned(a, "p1", "")
	a.mu.Lock()
	a.tasks["e1"] = &core.Task{ID: "e1", URL: "https://elsewhere.example/e1.bin", Status: core.StatusPaused, Enabled: true}
	a.mu.Unlock()

	got := a.PinChoices([]string{"p1", "e1"})
	if want := []PinChoice{{ID: "direct"}}; !slices.Equal(got, want) {
		t.Fatalf("PinChoices = %v, want %v", got, want)
	}
}

// A pin is the person's decision, so it outlives a restart: the paused
// download still goes to the backend it was pinned to, not to the one the
// ranking prefers.
func TestAPinStillHoldsAfterARestart(t *testing.T) {
	dir := t.TempDir()
	before, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := before.Store.Save(&core.Task{
		ID: "p1", URL: "https://" + pinHost + "/p1.bin", Name: "p1.bin", CreatedAt: time.Now(),
		Status: core.StatusPaused, Enabled: true, ResolverPin: "torbox",
	}); err != nil {
		t.Fatal(err)
	}
	before.Close()

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	bes := wirePinBackends(t, a)

	a.Resume("p1")

	wantHandled(t, bes["torbox"], "p1")
	wantNothingHandled(t, bes["alldebrid"], "the restart dropped the pin and the ranking took the task")
}

// A typo written into a task unchecked becomes a row that fails on the next
// dispatch pass for a reason nothing on screen connects to the dropdown.
func TestPinResolverRefusesABackendThisInstanceDoesNotHave(t *testing.T) {
	a, _ := pinApp(t)
	queuePinned(a, "p1", "")

	if err := a.PinResolver([]string{"p1"}, "definitely-not-a-backend"); err == nil {
		t.Fatal("PinResolver accepted a backend this instance does not have")
	}
	a.mu.Lock()
	got := a.tasks["p1"].ResolverPin
	a.mu.Unlock()
	if got != "" {
		t.Errorf("ResolverPin = %q after a refused request, want it untouched", got)
	}

	// The accepted case still lands, so the check above is a filter and not a
	// wall.
	if err := a.PinResolver([]string{"p1"}, "torbox"); err != nil {
		t.Fatalf("PinResolver(torbox) = %v, want it accepted", err)
	}
	a.mu.Lock()
	got = a.tasks["p1"].ResolverPin
	a.mu.Unlock()
	if got != "torbox" {
		t.Errorf("ResolverPin = %q, want %q", got, "torbox")
	}
}
