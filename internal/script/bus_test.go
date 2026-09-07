package script

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// recorder is a Bus subscriber that keeps what it was handed. Its own mutex
// because Publish delivers on the publisher's goroutine, which in these
// tests is not always the one asserting.
type recorder struct {
	mu   sync.Mutex
	got  []Firing
	name string
	log  *[]string
}

func (r *recorder) take(f Firing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, f)
	if r.log != nil {
		*r.log = append(*r.log, r.name)
	}
}

func (r *recorder) snapshot() []Firing {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Firing(nil), r.got...)
}

// TestBus_DeliversToEverySubscriberInSubscriptionOrder is the property the
// whole file exists for: one publish reaches every reader. If it only
// reached the first, "add a consumer" would silently mean "replace the
// existing one", and the script host would stop firing the day a
// notification channel subscribed.
func TestBus_DeliversToEverySubscriberInSubscriptionOrder(t *testing.T) {
	b := NewBus()
	var order []string
	first := &recorder{name: "first", log: &order}
	second := &recorder{name: "second", log: &order}
	b.Subscribe("first", first.take)
	b.Subscribe("second", second.take)

	b.Publish(Firing{Trigger: TriggerPackageDone, Package: &PackageView{Name: "Release", Done: 3}})

	for _, r := range []*recorder{first, second} {
		got := r.snapshot()
		if len(got) != 1 {
			t.Fatalf("%s received %d firings, want exactly 1", r.name, len(got))
		}
		if got[0].Trigger != TriggerPackageDone || got[0].Package == nil || got[0].Package.Name != "Release" {
			t.Errorf("%s received %+v, want the package.done firing for Release", r.name, got[0])
		}
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("delivery order = %v, want [first second] - subscribers run in the order they subscribed", order)
	}
}

// TestBus_ContainsAPanickingSubscriber pins the recover() in deliver. The
// publisher is a download's own goroutine: an unrecovered panic there does
// not fail an event, it kills the process and every other download in it.
// The second subscriber still getting its firing is the other half - one
// broken consumer must not silently unsubscribe the rest.
func TestBus_ContainsAPanickingSubscriber(t *testing.T) {
	b := NewBus()
	healthy := &recorder{name: "healthy"}
	b.Subscribe("thrower", func(Firing) { panic("subscriber blew up") })
	b.Subscribe("healthy", healthy.take)

	// Panicking out of Publish would take this goroutine, and the test
	// binary, down with it - so reaching the line after it at all is half of
	// what is being asserted here.
	b.Publish(Firing{Trigger: TriggerLinkAdded, Task: &TaskView{ID: "t1"}})

	if got := healthy.snapshot(); len(got) != 1 {
		t.Fatalf("the subscriber after the panicking one received %d firings, want 1", len(got))
	}
}

// TestBus_StampsAtWhenTheFiringCarriesNone keeps trigger.firedAt honest for
// a publisher that did not set At itself. Zero would reach a script as
// 0001-01-01, which reads as a bug in the app rather than a field nobody
// filled in.
func TestBus_StampsAtWhenTheFiringCarriesNone(t *testing.T) {
	b := NewBus()
	r := &recorder{name: "r"}
	b.Subscribe("r", r.take)

	before := time.Now()
	b.Publish(Firing{Trigger: TriggerQueueIdle})
	after := time.Now()

	got := r.snapshot()
	if len(got) != 1 {
		t.Fatalf("received %d firings, want 1", len(got))
	}
	if got[0].At.Before(before) || got[0].At.After(after) {
		t.Errorf("At = %v, want a stamp taken inside Publish (between %v and %v)", got[0].At, before, after)
	}
}

// TestHost_SubscribesToTheBusItWasGiven is the wiring internal/app depends
// on: it publishes to a Bus it owns and never touches the Host, so a Host
// that did not subscribe itself is a Host no event in the app ever reaches.
func TestHost_SubscribesToTheBusItWasGiven(t *testing.T) {
	bus := NewBus()
	hub := newFakeHub()
	h, err := NewHost(Options{DataDir: t.TempDir(), Actions: newFakeActions(), Hub: hub, Bus: bus})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if h.Bus() != bus {
		t.Fatal("Host.Bus() is not the bus it was given")
	}

	if _, err := h.SaveScript(Script{
		Name: "on-package", Trigger: TriggerPackageDone, Enabled: true,
		Code: `notify("pkg:" + pkg.name + ":" + pkg.done + "/" + pkg.files + " failed=" + pkg.failed);`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	bus.Publish(Firing{Trigger: TriggerPackageDone, Package: &PackageView{
		Name: "Release", Files: 4, Done: 3, Failed: 1,
	}})

	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "pkg:Release:3/4 failed=1" {
		t.Fatalf("notify message = %q, want %q", ev.Message, "pkg:Release:3/4 failed=1")
	}
}

// TestSandbox_PackageGlobalCannotBeCalledPackage is why the global is "pkg".
// Every script compiles in strict mode (rebuildIndex and RunNow both pass
// strict=true), where `package` is a FutureReservedWord that a script cannot
// even MENTION - so a global by that name would have been one no script
// bound to package.done could read, and every such script would have failed
// to compile with the error pointing at the user's own first line.
//
// Pinned as a test rather than left as a claim in a comment: the day
// somebody "tidies" the name back to `package` because it reads better, this
// is what tells them why it was not.
func TestSandbox_PackageGlobalCannotBeCalledPackage(t *testing.T) {
	if _, err := goja.Compile("t", "notify(package.name);", true); err == nil {
		t.Fatal("`package` compiled in strict mode; the reason the global is named pkg no longer holds and the naming can be revisited")
	} else if !strings.Contains(err.Error(), "reserved word") {
		t.Fatalf("`package` failed to compile for an unexpected reason: %v", err)
	}
	if _, err := goja.Compile("t", "notify(pkg.name);", true); err != nil {
		t.Fatalf("`pkg` does not compile in strict mode either: %v", err)
	}
}

// TestSandbox_PayloadGlobalsAreAbsentUnlessTheTriggerCarriesThem is the same
// rule "task" has always followed, extended to the five new payloads: a
// global left undefined can be tested for, where one bound to a zero-valued
// object has a script acting on a package of no files or an account with no
// name and no way to tell.
func TestSandbox_PayloadGlobalsAreAbsentUnlessTheTriggerCarriesThem(t *testing.T) {
	hub := newFakeHub()
	h := newTestHost(t, newFakeActions(), hub)
	if _, err := h.SaveScript(Script{
		Name: "shapes", Trigger: TriggerTaskDone, Enabled: true,
		Code: `notify([typeof pkg, typeof extraction, typeof reconnect, typeof account, typeof captcha, typeof task].join(","));`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	h.Bus().Publish(Firing{Trigger: TriggerTaskDone, Task: &TaskView{ID: "t1"}})

	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	const want = "undefined,undefined,undefined,undefined,undefined,object"
	if ev.Message != want {
		t.Fatalf("globals on a task.done firing = %q, want %q", ev.Message, want)
	}
}

// TestSandbox_NewPayloadsReachTheirGlobals walks the four remaining payloads
// in one pass. One case per event rather than one test per event, because
// what is being checked is identical each time: the Firing field the app
// fills in is the object the script reads, under the name the docs promise.
func TestSandbox_NewPayloadsReachTheirGlobals(t *testing.T) {
	cases := []struct {
		name   string
		firing Firing
		code   string
		want   string
	}{
		{
			name: "extract.done",
			firing: Firing{Trigger: TriggerExtractDone, Extract: &ExtractView{
				JobID: "j1", Name: "film.rar", OK: false, Error: "wrong password", Password: true,
			}},
			code: `notify(extraction.id + ":" + extraction.name + ":" + extraction.ok + ":" + extraction.password);`,
			want: "j1:film.rar:false:true",
		},
		{
			name: "reconnect.done",
			firing: Firing{Trigger: TriggerReconnectDone, Reconnect: &ReconnectView{
				OK: true, Changed: true, From: "10.0.0.1", To: "10.0.0.2", Checks: 3,
			}},
			code: `notify(reconnect.ok + ":" + reconnect.changed + ":" + reconnect.from + ">" + reconnect.to);`,
			want: "true:true:10.0.0.1>10.0.0.2",
		},
		{
			name: "account.expired",
			firing: Firing{Trigger: TriggerAccountExpired, Account: &AccountView{
				Service: "alldebrid", Account: "second", Label: "spare key", Expiry: "2020-01-01T00:00:00Z",
			}},
			code: `notify(account.service + "#" + account.account + ":" + account.label + ":" + account.expiry);`,
			want: "alldebrid#second:spare key:2020-01-01T00:00:00Z",
		},
		{
			name: "captcha.pending",
			firing: Firing{Trigger: TriggerCaptchaPending, Captcha: &CaptchaView{
				ID: "c1", Host: "host.example", Kind: "image", TaskID: "t9",
			}},
			code: `notify(captcha.id + ":" + captcha.host + ":" + captcha.kind + ":" + captcha.taskId);`,
			want: "c1:host.example:image:t9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub := newFakeHub()
			h := newTestHost(t, newFakeActions(), hub)
			if _, err := h.SaveScript(Script{
				Name: tc.name, Trigger: tc.firing.Trigger, Enabled: true, Code: tc.code,
			}); err != nil {
				t.Fatalf("SaveScript: %v", err)
			}
			h.Bus().Publish(tc.firing)
			ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
			if ev.Message != tc.want {
				t.Fatalf("notify message = %q, want %q", ev.Message, tc.want)
			}
		})
	}
}

// TestSandbox_FiredAtIsTheEventsOwnInstant proves runOne reads the Firing's
// At rather than calling time.Now() when a worker finally picks the job up.
// The two differ by however long the fire queue was, and a script asked to
// report "the download finished at" must not report when a worker got round
// to it.
func TestSandbox_FiredAtIsTheEventsOwnInstant(t *testing.T) {
	hub := newFakeHub()
	h := newTestHost(t, newFakeActions(), hub)
	if _, err := h.SaveScript(Script{
		Name: "when", Trigger: TriggerLinkAdded, Enabled: true,
		Code: `notify(trigger.firedAt);`,
	}); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	at := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	h.Bus().Publish(Firing{Trigger: TriggerLinkAdded, At: at, Task: &TaskView{ID: "t1"}})

	ev := waitEvent(t, hub, func(e Event) bool { return e.Kind == "notify" })
	if ev.Message != "2021-03-04T05:06:07Z" {
		t.Fatalf("trigger.firedAt = %q, want the Firing's own At (2021-03-04T05:06:07Z), not the worker's clock", ev.Message)
	}
}

// TestAllTriggersAreValidAndUnique closes the loop between the two lists a
// Trigger has to be in. Valid() is what Store validation refuses a save on
// and what rebuildIndex trusts; AllTriggers is what the editor's picker is
// built from. A trigger in one and not the other is either a picker entry
// that cannot be saved or a saveable trigger nobody can pick, and both fail
// on the user's screen rather than here.
func TestAllTriggersAreValidAndUnique(t *testing.T) {
	seen := map[Trigger]bool{}
	for _, tr := range AllTriggers() {
		if !tr.Valid() {
			t.Errorf("AllTriggers offers %q, which Valid() rejects - a script bound to it could never be saved", tr)
		}
		if seen[tr] {
			t.Errorf("AllTriggers lists %q twice", tr)
		}
		seen[tr] = true
	}
	// The four the first build shipped, spelled out: a rename here silently
	// unbinds every script already saved against the old string.
	for _, tr := range []Trigger{TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand} {
		if !seen[tr] {
			t.Errorf("%q is no longer offered; scripts already bound to it stop firing", tr)
		}
	}
}
