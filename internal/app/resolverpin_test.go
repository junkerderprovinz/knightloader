package app

// Pinning one task to one backend. Two things are being pinned down here and
// they pull in opposite directions, which is why both need a test: the pin has
// to beat the dispatcher's own ranking, and it must NOT beat account health.
// A pin that could be overruled by the ranking is not a pin; a pin that gets
// past a benched account is a way of turning the health mechanism off one row
// at a time.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const pinHost = "pinned.example"

// pinResolver matches one host at a priority the test chooses. hostResolver
// (stallwatch_test.go) is fixed at 90, and this file needs two backends
// claiming the SAME link at different ranks - otherwise "the pin was honoured"
// and "the ranking happened to agree" look identical.
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

// pinBackend records WHICH task it was handed, which is the whole question in
// this file - capBackend records the connection count instead.
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
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	// The volume guard is switched fully off here, so nothing in this file
	// depends on how much room the machine running it happens to have.
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	// And no automatic retry, so a task this file settles as failed stays
	// settled instead of arming a timer that outlives the test.
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
	return a, bes
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

// wantNothingHandled is the half that makes every test in this file mean
// something: proving the OTHER backend was not quietly used instead.
//
// It waits before looking, and that wait is the assertion rather than
// politeness. dispatchLocked hands a task over as `go be.Download(...)`, so
// reading the channel the instant the pass returns tests only that a goroutine
// has not been scheduled yet - which is true of a diversion that is about to
// happen exactly as it is of one that never will.
func wantNothingHandled(t *testing.T, be *pinBackend, why string) {
	t.Helper()
	select {
	case got := <-be.got:
		t.Fatalf("%s: it was handed %q anyway", why, got)
	case <-time.After(250 * time.Millisecond):
	}
}

// TestAnUnpinnedTaskFollowsTheRanking is the control. Without it every
// assertion below could be explained by the ranking already agreeing with the
// pin, and the whole file would prove nothing.
func TestAnUnpinnedTaskFollowsTheRanking(t *testing.T) {
	a, bes := pinApp(t)
	queuePinned(a, "p1", "")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	wantHandled(t, bes["alldebrid"], "p1")
	wantNothingHandled(t, bes["torbox"], "the lower-ranked backend took an unpinned task")
}

// TestAPinnedTaskGoesToTheBackendItNames is the feature: today a link stuck on
// one backend can only be deleted, or fixed by switching the whole instance
// over and pasting it again.
func TestAPinnedTaskGoesToTheBackendItNames(t *testing.T) {
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

// TestAPinnedBackendWithABenchedAccountFailsWhereItCanBeSeen is the limit on
// the whole feature, and the reason it does not amount to an off switch for
// account health. The wrong outcome here is not "it failed" - it is the app
// quietly fetching the link through the healthy backend next to it, which
// would make the pin decorative.
func TestAPinnedBackendWithABenchedAccountFailsWhereItCanBeSeen(t *testing.T) {
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
		t.Errorf("status = %q, want %q - a pinned task with nowhere to go has to fail visibly, not wait in silence", status, core.StatusError)
	}
	if reason != core.ReasonAuth {
		t.Errorf("reason = %q, want %q", reason, core.ReasonAuth)
	}
	if !strings.Contains(msg, "alldebrid") {
		t.Errorf("error = %q, want it to name the backend the task was pinned to", msg)
	}
}

// TestAPinNamingABackendThatCannotTakeTheLinkSaysSo is the other failure, and
// it is a different sentence because it is a different fix: nothing about
// waiting mends a pin pointed at a backend that does not handle this kind of
// link.
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

// TestAPinnedTaskNeverWalksTheFallbackChain closes the other door. Honouring
// the pin at dispatch and forgetting it in the fallback would move the task to
// another backend the first time the pinned one said "not mine" - the silent
// diversion the pin exists to make impossible, arriving one event later.
func TestAPinnedTaskNeverWalksTheFallbackChain(t *testing.T) {
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

// TestAPinMayNameTheServiceAndReachOneOfItsAccounts pins the vocabulary. A
// person who writes "alldebrid" means their AllDebrid subscription, not one
// particular key of it - the same rule settings.ResolverOrder is matched by.
// Without it a pin would break the moment a second key was added and the slot
// ids stopped being bare service names.
func TestAPinMayNameTheServiceAndReachOneOfItsAccounts(t *testing.T) {
	a, bes := pinApp(t)
	// Only the NAMED account of the service is registered, so a pin that
	// insisted on an exact id match would find nothing at all.
	a.Registry.Unregister("alldebrid")
	a.Registry.Register(pinResolver{id: "alldebrid#work", host: pinHost, prio: 90})
	queuePinned(a, "p1", "alldebrid")

	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()

	wantHandled(t, bes["alldebrid#work"], "p1")
	wantNothingHandled(t, bes["torbox"], "a service-wide pin fell through to another service")
}

// TestPinResolverRefusesABackendThisInstanceDoesNotHave keeps a typo out of
// the queue. Written into a task unchecked, it becomes a row that fails on the
// next dispatch pass for a reason nothing on screen connects to the dropdown
// somebody just used.
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

	// And the accepted case still lands, so the check above is a filter rather
	// than a wall.
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
