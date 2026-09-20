package notify

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// fastBackoff shortens the retry waits for the length of one test, which is why
// the steps are a package var. Tests in this package do not call t.Parallel, so
// the swap is safe; one that did would need a different route.
func fastBackoff(t *testing.T) {
	t.Helper()
	was := backoffSteps
	backoffSteps = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { backoffSteps = was })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func healthOf(d *Dispatcher, id string) Health {
	for _, h := range d.Health() {
		if h.TargetID == id {
			return h
		}
	}
	return Health{}
}

func taskDone(name string) script.Firing {
	return script.Firing{Trigger: script.TriggerTaskDone, At: time.Now(), Task: &script.TaskView{ID: "t", Name: name}}
}

// script.Bus.Publish delivers synchronously on the publisher's goroutine, and
// the publishers are a download's update path and two poll loops, so a
// subscriber that sent the request itself would stall the download that
// published for as long as the far end takes to answer.
//
// The far end signals that the request arrived, so a filter that dropped
// everything fails the wait at the bottom rather than sailing through the
// timing assertion above it.
func TestOnDoesNotBlockThePublisher(t *testing.T) {
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	d := New(Options{InstanceName: func() string { return "box" }})
	defer d.Close()
	d.Set([]Target{{
		ID: "1", Name: "slow", Enabled: true, URL: srv.URL,
		Triggers: []script.Trigger{script.TriggerTaskDone}, TimeoutSeconds: 30,
	}})

	bus := script.NewBus()
	bus.Subscribe("eventtargets", d.On)

	started := time.Now()
	bus.Publish(taskDone("film.mkv"))
	took := time.Since(started)

	// The server holds the request open for as long as this test allows, so
	// anything near a second here is a Send that happened inline.
	if took > 250*time.Millisecond {
		t.Fatalf("Publish took %s, so the subscriber is sending on the publisher's goroutine", took)
	}
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the message never reached the far end, so the timing above proves nothing")
	}
}

// link.added fires once per link, so one paste of a two hundred link container
// is two hundred messages, and what the queue cannot hold is dropped and
// counted rather than buffered.
func TestAFullQueueDropsAndCounts(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{
		ID: "1", Enabled: true, URL: srv.URL,
		Triggers: []script.Trigger{script.TriggerLinkAdded}, TimeoutSeconds: 30,
	}})

	for i := 0; i < queueDepth+64; i++ {
		d.On(script.Firing{Trigger: script.TriggerLinkAdded, At: time.Now()})
	}
	if got := healthOf(d, "1").Dropped; got == 0 {
		t.Fatalf("%d messages went into a queue %d deep against a target that answers nothing "+
			"and none was dropped, so they are being buffered", queueDepth+64, queueDepth)
	}
}

func TestOnlyTheTickedEventsReachATarget(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{
		ID: "1", Enabled: true, URL: srv.URL,
		Triggers: []script.Trigger{script.TriggerPackageDone},
	}})

	d.On(taskDone("not this one"))
	d.On(script.Firing{Trigger: script.TriggerPackageDone, At: time.Now(), Package: &script.PackageView{Name: "p"}})

	waitFor(t, "the package event to arrive", func() bool { return hits.Load() >= 1 })
	// Long enough that a task.done leaking through would have arrived too.
	time.Sleep(50 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("the far end saw %d messages, want exactly the one event that was ticked", got)
	}
}

func TestATargetWithNothingTickedSendsNothing(t *testing.T) {
	// An empty trigger list means no events rather than all of them: a target
	// subscribed to everything the moment it was switched on would send
	// hundreds of messages the first time somebody pasted a container.
	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{ID: "1", Enabled: true, URL: "https://x.invalid/"}})
	d.mu.Lock()
	n := len(d.workers)
	d.mu.Unlock()
	if n != 0 {
		t.Fatalf("a target with no event ticked got %d worker(s)", n)
	}
}

func TestAServerErrorIsRetriedAndARefusalIsNot(t *testing.T) {
	fastBackoff(t)

	var serverErrHits, refusalHits atomic.Int64
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if serverErrHits.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer flaky.Close()
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refusalHits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refusing.Close()

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{
		{ID: "1", Enabled: true, URL: flaky.URL, Triggers: []script.Trigger{script.TriggerTaskDone}, Attempts: 3},
		{ID: "2", Enabled: true, URL: refusing.URL, Triggers: []script.Trigger{script.TriggerTaskDone}, Attempts: 3},
	})

	d.On(taskDone("film.mkv"))

	waitFor(t, "the flaky target to get its message through", func() bool { return healthOf(d, "1").Sent == 1 })
	if got := serverErrHits.Load(); got != 2 {
		t.Errorf("the flaky server saw %d requests, want the 500 and one retry", got)
	}

	waitFor(t, "the refusing target to answer once", func() bool { return healthOf(d, "2").Attempts >= 1 })
	// Long enough that a retry, at a one millisecond backoff, would already have
	// happened.
	time.Sleep(100 * time.Millisecond)
	if got := refusalHits.Load(); got != 1 {
		t.Errorf("a 401 was tried %d times, want once", got)
	}
	if h := healthOf(d, "2"); h.LastCode != ProblemAuth || h.LastStatus != http.StatusUnauthorized {
		t.Errorf("the health row says code %q status %d, want %q and 401", h.LastCode, h.LastStatus, ProblemAuth)
	}
}

func TestHealthSeparatesTheLastTryFromTheLastArrival(t *testing.T) {
	fastBackoff(t)
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{ID: "1", Enabled: true, URL: srv.URL, Triggers: []script.Trigger{script.TriggerTaskDone}, Attempts: 1}})

	d.On(taskDone("one"))
	waitFor(t, "the first message to arrive", func() bool { return healthOf(d, "1").Sent == 1 })
	arrived := healthOf(d, "1").LastOK

	fail.Store(true)
	d.On(taskDone("two"))
	waitFor(t, "the second message to fail", func() bool { return healthOf(d, "1").LastStatus == http.StatusNotFound })

	h := healthOf(d, "1")
	if !h.LastOK.Equal(arrived) {
		t.Errorf("the last arrival moved on a failed attempt")
	}
	if !h.LastAttempt.After(h.LastOK) {
		t.Errorf("the last attempt (%s) is not after the last arrival (%s)", h.LastAttempt, h.LastOK)
	}
	if h.LastCode != ProblemNotFound {
		t.Errorf("the code is %q, want %q", h.LastCode, ProblemNotFound)
	}
}

func TestSwitchingATargetOffStopsItAndForgetsItsRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	on := Target{ID: "1", Enabled: true, URL: srv.URL, Triggers: []script.Trigger{script.TriggerTaskDone}}
	d.Set([]Target{on})
	d.On(taskDone("one"))
	waitFor(t, "the message to arrive", func() bool { return healthOf(d, "1").Sent == 1 })

	off := on
	off.Enabled = false
	d.Set([]Target{off})

	d.mu.Lock()
	n := len(d.workers)
	d.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d worker(s) still running for a target that is switched off", n)
	}
	if h := healthOf(d, "1"); h.Sent != 0 {
		t.Errorf("the health row survived the switch: %+v", h)
	}
}

func TestSetKeepsTheRowAndTheQueueAcrossAnUnrelatedSave(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	row := Target{ID: "1", Enabled: true, URL: srv.URL, Triggers: []script.Trigger{script.TriggerTaskDone}}
	d.Set([]Target{row})
	d.On(taskDone("one"))
	waitFor(t, "the message to arrive", func() bool { return healthOf(d, "1").Sent == 1 })

	// What saving an unrelated setting looks like: the same row comes back.
	// Rebuilding here would blank the table the operator is reading.
	renamed := row
	renamed.Name = "phone"
	d.Set([]Target{renamed})
	if h := healthOf(d, "1"); h.Sent != 1 {
		t.Errorf("the health row was rebuilt by an unrelated save: %+v", h)
	}
}

func TestCloseWaitsForADeliveryAlreadyUnderWay(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	d := New(Options{})
	d.Set([]Target{{ID: "1", Enabled: true, URL: srv.URL, Triggers: []script.Trigger{script.TriggerTaskDone}, TimeoutSeconds: 60}})
	d.On(taskDone("one"))
	<-started

	done := make(chan struct{})
	go func() {
		_ = d.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return, so the request in flight is not cancelled with the dispatcher")
	}
	// Idempotent, the same promise script.Host.Close makes.
	if err := d.Close(); err != nil {
		t.Fatalf("a second Close answered %v", err)
	}
}

func TestSetAfterCloseStartsNothing(t *testing.T) {
	d := New(Options{})
	_ = d.Close()
	d.Set([]Target{{ID: "1", Enabled: true, URL: "https://x.invalid/", Triggers: []script.Trigger{script.TriggerTaskDone}}})
	d.mu.Lock()
	n := len(d.workers)
	d.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d worker(s) started after Close committed to shutting down", n)
	}
}
