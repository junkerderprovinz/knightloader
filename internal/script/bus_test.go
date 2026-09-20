package script

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// recorder is a Bus subscriber that keeps what it was handed. It has its own
// mutex because the publishing goroutine is not always the asserting one.
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
		t.Errorf("delivery order = %v, want [first second]; subscribers run in the order they subscribed", order)
	}
}

// TestBus_ContainsAPanickingSubscriber: a panic on the publishing download
// goroutine would kill the process, and the other subscribers still get the
// firing.
func TestBus_ContainsAPanickingSubscriber(t *testing.T) {
	b := NewBus()
	healthy := &recorder{name: "healthy"}
	b.Subscribe("thrower", func(Firing) { panic("subscriber blew up") })
	b.Subscribe("healthy", healthy.take)

	// A panic escaping Publish would take the test binary down.
	b.Publish(Firing{Trigger: TriggerLinkAdded, Task: &TaskView{ID: "t1"}})

	if got := healthy.snapshot(); len(got) != 1 {
		t.Fatalf("the subscriber after the panicking one received %d firings, want 1", len(got))
	}
}

// TestBus_StampsAtWhenTheFiringCarriesNone: otherwise trigger.firedAt would
// read 0001-01-01.
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

// TestHost_SubscribesToTheBusItWasGiven: internal/app publishes to its own Bus
// and never calls the Host directly.
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

// TestSandbox_PackageGlobalCannotBeCalledPackage: scripts compile in strict
// mode, where `package` is a reserved word no script can reference, which is
// why the global is "pkg".
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

// TestSandbox_PayloadGlobalsAreAbsentUnlessTheTriggerCarriesThem: an undefined
// global can be tested for, unlike one bound to a zero-valued object.
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

// TestSandbox_NewPayloadsReachTheirGlobals: each Firing payload is the object
// the script reads, under its documented name.
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

// TestSandbox_FiredAtIsTheEventsOwnInstant: runOne reports the Firing's At,
// not when a worker picked the job up.
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

// TestAllTriggersAreValidAndUnique keeps Valid, which saving checks, and
// AllTriggers, which the editor's picker shows, in agreement.
func TestAllTriggersAreValidAndUnique(t *testing.T) {
	seen := map[Trigger]bool{}
	for _, tr := range AllTriggers() {
		if !tr.Valid() {
			t.Errorf("AllTriggers offers %q, which Valid() rejects; a script bound to it could never be saved", tr)
		}
		if seen[tr] {
			t.Errorf("AllTriggers lists %q twice", tr)
		}
		seen[tr] = true
	}
	// Renaming one of these would unbind every script saved against it.
	for _, tr := range []Trigger{TriggerTaskDone, TriggerTaskFailed, TriggerQueueIdle, TriggerOnDemand} {
		if !seen[tr] {
			t.Errorf("%q is no longer offered; scripts already bound to it stop firing", tr)
		}
	}
}
