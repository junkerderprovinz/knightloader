package app

// Tests for this package's ambient-activity tracker (app_activity.go) and
// for the four places it is wired in: a.crawl (app_links.go), RecheckTasks
// (app_tasks.go), ConfirmTasks (app_confirm.go) and pollCaptchasOnce
// (app_captcha.go).
//
// activityFakeConn plays the same role hub_test.go's own fakeConn does for
// internal/hub's tests, redeclared here rather than imported: hub.Conn is
// the only exported seam, hub_test.go's implementation is unexported, and a
// same-package test in a DIFFERENT package cannot reach it.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
)

// activityFakeConn records every frame Hub.Write hands it, in arrival order.
// It never blocks and never errors, so it is never dropped by the hub's own
// back-pressure handling (internal/hub/hub.go's enqueue) - the one thing
// these tests need is to see everything, not to model a slow client.
type activityFakeConn struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (f *activityFakeConn) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, append([]byte(nil), p...))
	return nil
}

func (f *activityFakeConn) CloseNow() error { return nil }

func (f *activityFakeConn) snapshot() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.msgs))
	copy(out, f.msgs)
	return out
}

type envelope struct {
	Type string   `json:"type"`
	Data Activity `json:"data"`
}

// activityMessages waits for at least `want` broadcasts of kind to have
// arrived and returns them in the order they were sent.
//
// Polled rather than read synchronously: Hub.Broadcast only enqueues onto a
// channel, and the fake connection's own Write runs on the hub's writer
// goroutine, on its own schedule - reading snapshot() immediately after the
// call under test would be racing that goroutine, not observing it.
func activityMessages(t *testing.T, f *activityFakeConn, kind ActivityKind, want int) []Activity {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []Activity
	for time.Now().Before(deadline) {
		got = nil
		for _, raw := range f.snapshot() {
			var env envelope
			if json.Unmarshal(raw, &env) == nil && env.Type == "activity" && env.Data.Kind == kind {
				got = append(got, env.Data)
			}
		}
		if len(got) >= want {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("got %d %q activity message(s), want at least %d", len(got), kind, want)
	return nil
}

// waitForType blocks until a message of typ has been observed, so a caller
// proving an ABSENCE afterward (see TestConfirmTasksSkipsActivityForManualTrigger
// and TestPollCaptchasOnceWithoutJDBroadcastsNoActivityGauge) is reading a
// queue known to be fully drained up to that point, rather than guessing at
// a sleep long enough to outrun the writer goroutine.
func waitForType(t *testing.T, f *activityFakeConn, typ string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, raw := range f.snapshot() {
			var env envelope
			if json.Unmarshal(raw, &env) == nil && env.Type == typ {
				return
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("a %q message never arrived", typ)
}

func newActivityTestApp(t *testing.T) (*App, *activityFakeConn) {
	t.Helper()
	a := newCaptchaTestApp(t)
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	return a, fc
}

// TestBeginActivityAccumulatesAndBroadcasts pins the basic shape: a burst
// starts at (n, n), and two callers of the same kind add into the same
// counters rather than each owning their own - the case beginActivity's own
// doc comment names (two browsers both pressing "recheck all").
func TestBeginActivityAccumulatesAndBroadcasts(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.beginActivity(ActivityCrawl, 2)
	a.beginActivity(ActivityCrawl, 3)

	got := activityMessages(t, fc, ActivityCrawl, 2)
	if got[0] != (Activity{Kind: ActivityCrawl, Active: 2, Total: 2}) {
		t.Errorf("first broadcast = %+v, want {crawl 2 2}", got[0])
	}
	if got[1] != (Activity{Kind: ActivityCrawl, Active: 5, Total: 5}) {
		t.Errorf("second broadcast = %+v, want {crawl 5 5}", got[1])
	}
}

// TestEndActivityResetsTotalOnceIdle is the whole reason Total is not a
// running lifetime counter: once a burst finishes, the next one starts from
// zero rather than inheriting a number that has nothing to do with it.
func TestEndActivityResetsTotalOnceIdle(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.beginActivity(ActivityLinkCheck, 3)
	a.endActivity(ActivityLinkCheck, 1)
	a.endActivity(ActivityLinkCheck, 2)
	a.beginActivity(ActivityLinkCheck, 1)

	got := activityMessages(t, fc, ActivityLinkCheck, 4)
	want := []Activity{
		{Kind: ActivityLinkCheck, Active: 3, Total: 3},
		{Kind: ActivityLinkCheck, Active: 2, Total: 3},
		{Kind: ActivityLinkCheck, Active: 0, Total: 3},
		// The new burst starts clean - {1,4} would mean the previous burst's
		// total leaked into this one.
		{Kind: ActivityLinkCheck, Active: 1, Total: 1},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("message %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestEndActivityDoesNotUnderflow guards the defensive floor: a caller that
// retires more than it started must read as "0", never as a count that goes
// negative and reads backwards on the strip.
func TestEndActivityDoesNotUnderflow(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.beginActivity(ActivityCaptcha, 1)
	a.endActivity(ActivityCaptcha, 1)
	a.endActivity(ActivityCaptcha, 1)

	got := activityMessages(t, fc, ActivityCaptcha, 3)
	last := got[2]
	if last.Active != 0 || last.Total != 0 {
		t.Errorf("over-retired activity = %+v, want {active:0 total:0}", last)
	}
}

// TestActivityKindsAreIndependent makes sure the four kinds do not share a
// counter - a crawl finishing must not zero out a linkcheck burst still in
// progress.
func TestActivityKindsAreIndependent(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.beginActivity(ActivityLinkCheck, 4)
	a.beginActivity(ActivityCrawl, 1)
	a.endActivity(ActivityCrawl, 1)

	linkcheck := activityMessages(t, fc, ActivityLinkCheck, 1)
	if linkcheck[0].Active != 4 || linkcheck[0].Total != 4 {
		t.Errorf("linkcheck = %+v, want untouched at {4,4} after an unrelated crawl finished", linkcheck[0])
	}
	crawl := activityMessages(t, fc, ActivityCrawl, 2)
	if crawl[1].Active != 0 {
		t.Errorf("crawl = %+v, want it to have settled to 0 on its own", crawl[1])
	}
}

// TestSetActivityGaugeOverwritesPreviousBurst pins the gauge shape captcha
// uses: Active and Total always equal, and a later call simply replaces the
// live count rather than accumulating against it the way begin/end do.
func TestSetActivityGaugeOverwritesPreviousBurst(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.setActivityGauge(ActivityCaptcha, 3)
	a.setActivityGauge(ActivityCaptcha, 1)
	a.setActivityGauge(ActivityCaptcha, 0)

	got := activityMessages(t, fc, ActivityCaptcha, 3)
	want := []Activity{
		{Kind: ActivityCaptcha, Active: 3, Total: 3},
		{Kind: ActivityCaptcha, Active: 1, Total: 1},
		{Kind: ActivityCaptcha, Active: 0, Total: 0},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("message %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestCrawlBroadcastsCrawlActivity is the real a.crawl (app_links.go), not
// the tracker in isolation: a page pasted into the collector shows up as
// "crawl" activity that starts and ends around the fake crawler's own call,
// the way pollCaptchasOnce (app_captcha.go) already shaped a periodic
// poll-then-broadcast loop for 7A.
func TestCrawlBroadcastsCrawlActivity(t *testing.T) {
	a := newCrawlApp(t, true)
	fc := &activityFakeConn{}
	a.Hub.Add(fc)
	t.Cleanup(func() { a.Hub.Remove(fc) })
	a.Crawler = &fakeCrawler{yield: []crawler.Result{
		{URL: "https://host.example/one.bin", Name: "one.bin"},
	}}

	created := a.AddLinks([]string{"https://host.example/gallery"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the 1 file the fake page pointed at", len(created))
	}

	got := activityMessages(t, fc, ActivityCrawl, 2)
	if got[0] != (Activity{Kind: ActivityCrawl, Active: 1, Total: 1}) {
		t.Errorf("first crawl activity = %+v, want {crawl 1 1}", got[0])
	}
	if got[1] != (Activity{Kind: ActivityCrawl, Active: 0, Total: 1}) {
		t.Errorf("last crawl activity = %+v, want {crawl 0 1}", got[1])
	}
}

// TestRecheckTasksBroadcastsLinkCheckActivity is the real availability pass
// (app_tasks.go), not the tracker in isolation: two collected links behind
// one batching backend settle in one Check() round trip, and the activity
// broadcasts have to end at zero once RecheckTasks returns.
func TestRecheckTasksBroadcastsLinkCheckActivity(t *testing.T) {
	a, fc := newActivityTestApp(t)
	svc := &batchResolver{verdicts: map[string]core.Availability{
		"https://batch.example/a.bin": core.AvailOnline,
		"https://batch.example/b.bin": core.AvailOnline,
	}}
	a.Registry.Register(svc)
	putTask(t, a, core.Task{URL: "https://batch.example/a.bin", Name: "a.bin", Status: core.StatusCollected, Enabled: true})
	putTask(t, a, core.Task{URL: "https://batch.example/b.bin", Name: "b.bin", Status: core.StatusCollected, Enabled: true})

	a.RecheckTasks(nil)

	got := activityMessages(t, fc, ActivityLinkCheck, 3)
	if got[0] != (Activity{Kind: ActivityLinkCheck, Active: 2, Total: 2}) {
		t.Errorf("first linkcheck activity = %+v, want {linkcheck 2 2}", got[0])
	}
	last := got[len(got)-1]
	if last != (Activity{Kind: ActivityLinkCheck, Active: 0, Total: 2}) {
		t.Errorf("last linkcheck activity = %+v, want {linkcheck 0 2}", last)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Active > got[i-1].Active {
			t.Fatalf("active count rose from %d to %d at message %d, want it monotonically settling to 0", got[i-1].Active, got[i].Active, i)
		}
	}
}

// TestRecheckTasksOfNothingBroadcastsNothing guards the early return: a
// recheck that finds no collected tasks at all must not flash a {0,0} onto
// the strip for a kind that was never doing anything.
func TestRecheckTasksOfNothingBroadcastsNothing(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.RecheckTasks(nil)
	a.Hub.Broadcast("test-sentinel", nil)
	waitForType(t, fc, "test-sentinel")

	for _, raw := range fc.snapshot() {
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Type == "activity" && env.Data.Kind == ActivityLinkCheck {
			t.Fatalf("an empty recheck broadcast linkcheck activity: %+v", env.Data)
		}
	}
}

// TestConfirmTasksBroadcastsAutoConfirmForNonInteractiveTrigger is Wave 8's
// confirm-policy path (app_confirm.go), wired into the same channel: an
// auto-confirm evaluating a batch is ambient activity because nobody is
// watching the collector for it - see confirm.Trigger.Interactive's own
// doc comment.
func TestConfirmTasksBroadcastsAutoConfirmForNonInteractiveTrigger(t *testing.T) {
	a, fc := newActivityTestApp(t)
	putTask(t, a, core.Task{URL: "https://host.example/a.bin", Name: "a.bin", Size: 10, Status: core.StatusCollected, Enabled: true})
	putTask(t, a, core.Task{URL: "https://host.example/b.bin", Name: "b.bin", Size: 20, Status: core.StatusCollected, Enabled: true})

	result := a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerAutoConfirm)
	if len(result.Start) != 2 {
		t.Fatalf("confirm started %d tasks, want both (neither is a duplicate or offline)", len(result.Start))
	}

	got := activityMessages(t, fc, ActivityAutoConfirm, 2)
	if got[0] != (Activity{Kind: ActivityAutoConfirm, Active: 2, Total: 2}) {
		t.Errorf("first autoconfirm activity = %+v, want {autoconfirm 2 2}", got[0])
	}
	last := got[len(got)-1]
	if last != (Activity{Kind: ActivityAutoConfirm, Active: 0, Total: 2}) {
		t.Errorf("last autoconfirm activity = %+v, want {autoconfirm 0 2}", last)
	}
}

// TestConfirmTasksSkipsActivityForManualTrigger is the other half: a person
// at the collector confirming by hand already has the page as its own
// feedback, so this must NOT show up on the ambient status strip too.
func TestConfirmTasksSkipsActivityForManualTrigger(t *testing.T) {
	a, fc := newActivityTestApp(t)
	putTask(t, a, core.Task{URL: "https://host.example/c.bin", Name: "c.bin", Size: 10, Status: core.StatusCollected, Enabled: true})

	a.ConfirmTasks(nil, confirm.Config{}, confirm.TriggerManual)
	a.Hub.Broadcast("test-sentinel", nil)
	waitForType(t, fc, "test-sentinel")

	for _, raw := range fc.snapshot() {
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Type == "activity" && env.Data.Kind == ActivityAutoConfirm {
			t.Fatalf("a manual (interactive) confirm broadcast ambient activity: %+v", env.Data)
		}
	}
}

// TestPollCaptchasOnceWithoutJDBroadcastsNoActivityGauge is pollCaptchasOnce's
// error path (app_captcha.go), exercised the same way captcha_test.go's own
// package comment describes: KL_JD is unset in this process, so
// captcha.JDSource.List answers captcha.ErrJDNotConfigured without a network
// call, and that path must stay silent on the "activity" channel exactly as
// it already stays silent on "captcha" - see pollCaptchasOnce's own doc
// comment on why a transient failure must not re-publish an unchanged count
// on every tick.
func TestPollCaptchasOnceWithoutJDBroadcastsNoActivityGauge(t *testing.T) {
	a, fc := newActivityTestApp(t)

	a.pollCaptchasOnce(a.captchaStateFor())
	a.Hub.Broadcast("test-sentinel", nil)
	waitForType(t, fc, "test-sentinel")

	for _, raw := range fc.snapshot() {
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Type == "activity" && env.Data.Kind == ActivityCaptcha {
			t.Fatalf("a failed poll (JD not configured) broadcast a captcha gauge: %+v", env.Data)
		}
	}
}
