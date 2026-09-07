package script

// The event bus: one publish, many readers.
//
// Until this file internal/app called Host.Fire straight from the two places
// that had something to report, and that shape does not survive a second
// reader. The next thing that wants to know a package finished - a message
// out to Matrix or ntfy, a media library told to rescan, a single "events"
// feed on the WebSocket - would have to be added as a SECOND call beside
// every firing site, and the day somebody adds a ninth event and remembers
// only one of the two lists, the two readers disagree about what the app
// actually did. Publishing once and letting readers register themselves
// makes "add a consumer" a subscription rather than an edit to every site
// that fires.
//
// This build wires exactly ONE subscriber, the script Host (see NewHost).
// The Hub is deliberately not a second one: every fact published here
// already reaches the browser as its own hub message ("task", "extract",
// "captcha", "queue"), so a bus-to-hub bridge added now would send every UI
// update twice. The bridge belongs to whoever builds a feature that wants
// one stream instead of six, and it is a Subscribe call, not a change to
// this file.

import (
	"log"
	"sync"
	"time"
)

// Firing is one occurrence of a Trigger: which event, when it happened, and
// the context every subscriber sees. It is one struct with optional payload
// pointers rather than an interface or a map[string]any, for the reason
// Trigger's own doc comment gives for being a closed list: a subscriber can
// switch on Trigger and reach for exactly the field that event carries, and
// the compiler is what tells the next person adding an event that they have
// to say what it carries.
//
// Which payload belongs to which trigger is fixed and small:
//
//   - Task: task.done, task.failed, link.added, checksum.failed, and
//     manual when the run was started from a task's own row. Also set
//     alongside Extract and Captcha where the app could name the download
//     those are about, so a script bound to either still gets task.pause()
//     and friends.
//   - Package: package.done, and nothing else.
//   - Extract: extract.done.
//   - Reconnect: reconnect.done.
//   - Account: account.expired.
//   - Captcha: captcha.pending.
//   - Queue: every trigger. It is counters, not a payload, and a script
//     bound to any event may legitimately want to know what the queue looked
//     like at the moment it fired.
//
// A payload the trigger does not carry is nil, and newRuntime (sandbox.go)
// leaves the matching JS global undefined rather than binding an empty
// object - the same rule "task" has followed since the first version: a
// script tests `typeof pkg` and gets an honest answer, instead of reading
// zeroes off a shape that was never filled in.
type Firing struct {
	Trigger Trigger `json:"trigger"`
	// At is when the event happened, not when a worker got round to running
	// a script for it. The two differ by however long the fire queue was,
	// and the first is the one a script's own trigger.firedAt should show.
	At        time.Time      `json:"at"`
	Task      *TaskView      `json:"task,omitempty"`
	Queue     QueueView      `json:"queue"`
	Package   *PackageView   `json:"package,omitempty"`
	Extract   *ExtractView   `json:"extract,omitempty"`
	Reconnect *ReconnectView `json:"reconnect,omitempty"`
	Account   *AccountView   `json:"account,omitempty"`
	Captcha   *CaptchaView   `json:"captcha,omitempty"`
}

// Bus fans one Firing out to every registered subscriber. The zero value is
// not usable; call NewBus.
type Bus struct {
	mu   sync.RWMutex
	subs []subscription
}

// subscription pairs a reader with the name used in the one log line this
// file can produce. The name is required rather than optional because that
// log line ("subscriber %q panicked") is the only evidence a wrongly written
// consumer ever leaves, and "a subscriber panicked" names nobody.
type subscription struct {
	name string
	fn   func(Firing)
}

// NewBus builds an empty bus. It owns no goroutines and needs no Close:
// delivery happens on the publisher's own goroutine (see Publish), so there
// is nothing here that can outlive the app the way a worker pool can.
func NewBus() *Bus { return &Bus{} }

// Subscribe registers fn to receive every Firing published from now on.
//
// EVERY SUBSCRIBER MUST RETURN PROMPTLY. Publish calls them one after
// another on the caller's own goroutine, and every caller in internal/app is
// either a download's update path or a poll loop shared with other work - a
// subscriber that makes a network call or waits on a channel does not slow
// down "the events", it slows down the download that published. This is the
// same contract Actions (sandbox.go) states for the same reason, and the
// Host's own subscriber honours it the same way: it drops the Firing into a
// bounded channel and returns (see Host.fire).
//
// There is no Unsubscribe. Every subscriber this design anticipates is wired
// once while the app is being built and lives exactly as long as the process
// does; an Unsubscribe nobody calls is a method that quietly rots until the
// first caller discovers it was never right. Whoever needs one should add it
// together with the caller that proves it works.
func (b *Bus) Subscribe(name string, fn func(Firing)) {
	if fn == nil {
		return
	}
	b.mu.Lock()
	b.subs = append(b.subs, subscription{name: name, fn: fn})
	b.mu.Unlock()
}

// Publish delivers f to every subscriber, in the order they subscribed.
//
// Call it OFF whatever lock the fact came from. Delivery is synchronous, so
// publishing while holding internal/app's a.mu would hand that lock's
// protection to code this package cannot see - and the Host's own subscriber
// reaches Broadcast, which reaches every connected browser.
//
// A subscriber that panics is contained here rather than allowed to travel
// back up into its publisher. The publisher is a download goroutine, and an
// unrecovered panic on any goroutine kills the whole process, taking every
// other download with it - the identical argument execute's own recover()
// makes in sandbox.go, one layer down. The remaining subscribers still get
// the event: one broken consumer must not silently unsubscribe the others.
func (b *Bus) Publish(f Firing) {
	if f.At.IsZero() {
		f.At = time.Now()
	}
	// Snapshotted under the read lock and called outside it. Calling
	// subscribers while still holding it would deadlock the moment one of
	// them subscribes another (sync.RWMutex is not reentrant, and a Subscribe
	// waiting for the write lock blocks every later reader), which is exactly
	// what a consumer that registers a sub-consumer on its first event would
	// do.
	b.mu.RLock()
	subs := b.subs
	b.mu.RUnlock()
	for _, s := range subs {
		deliver(s, f)
	}
}

// deliver is one subscriber's call, wrapped so its panic ends here. Split
// out of the loop above rather than written as an inline closure so the
// recover() is scoped to a single subscriber: written inline, the deferred
// recover would only run when Publish itself returns, by which point the
// panic has already skipped every subscriber queued behind the one that
// threw.
func deliver(s subscription, f Firing) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("script: bus subscriber %q panicked on %s, dropping this firing for it: %v", s.name, f.Trigger, r)
		}
	}()
	s.fn(f)
}
