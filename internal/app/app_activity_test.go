package app

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

// activityFakeConn records every frame the hub writes, in order. It never
// blocks or fails, so the hub never drops it.
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

// activityMessages waits until at least want broadcasts of kind have arrived
// and returns them in order. Hub.Broadcast only enqueues, so the fake's Write
// runs later on the hub's writer goroutine.
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

// waitForType blocks until a message of typ has arrived, so a test checking
// for the absence of a message knows everything sent before it was delivered.
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
		{Kind: ActivityLinkCheck, Active: 1, Total: 1},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("message %d = %+v, want %+v", i, got[i], w)
		}
	}
}

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
	// Cancellable while the crawl runs, so the strip shows a stop button for
	// exactly that long.
	if got[0] != (Activity{Kind: ActivityCrawl, Active: 1, Total: 1, Cancellable: 1}) {
		t.Errorf("first crawl activity = %+v, want {crawl 1 1 cancellable 1}", got[0])
	}
	if got[1] != (Activity{Kind: ActivityCrawl, Active: 0, Total: 1}) {
		t.Errorf("last crawl activity = %+v, want {crawl 0 1 cancellable 0}", got[1])
	}
}

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

// TestConfirmTasksSkipsActivityForManualTrigger: someone confirming by hand
// already has the collector page as feedback.
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

// TestPollCaptchasOnceWithoutJDBroadcastsNoActivityGauge: with KL_JD unset the
// poll fails without a network call, and a failed poll must not republish the
// count.
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
