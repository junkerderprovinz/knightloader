package script

// The event bus: one publish, many readers. A new consumer of app events
// subscribes instead of being added as a second call beside every site that
// fires, so two readers cannot disagree about what happened.
//
// The script Host, the event targets, the event programs and the media hooks
// subscribe. The Hub does not: every fact published here already reaches the
// browser as its own hub message, and a bridge would send each UI update twice.

import (
	"log"
	"sync"
	"time"
)

// Firing is one occurrence of a Trigger: which event, when, and what every
// subscriber sees. Optional payload pointers rather than a map let a
// subscriber switch on Trigger and reach for the field that event carries.
//
// Which payload belongs to which trigger:
//
//   - Task: task.done, task.failed, link.added, checksum.failed, and manual
//     when started from a task's row. Also set alongside Extract and Captcha
//     where the app knows the download, so those scripts get task.pause()
//     and the rest.
//   - Package: package.done.
//   - Extract: extract.done.
//   - Reconnect: reconnect.done.
//   - Account: account.expired.
//   - Captcha: captcha.pending.
//   - Queue: every trigger, since it is counters rather than a payload.
//
// A payload the trigger does not carry is nil, and newRuntime leaves the
// matching JS global undefined, so a script can test `typeof pkg`.
type Firing struct {
	Trigger Trigger `json:"trigger"`
	// At is when the event happened, not when a worker ran a script for it;
	// trigger.firedAt shows this one.
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

// subscription pairs a reader with the name its panic log line reports.
type subscription struct {
	name string
	fn   func(Firing)
}

// NewBus builds an empty bus. It owns no goroutines and needs no Close, since
// delivery happens on the publisher's goroutine.
func NewBus() *Bus { return &Bus{} }

// Subscribe registers fn to receive every Firing published from now on.
//
// Every subscriber must return promptly. Publish calls them in turn on the
// publisher's goroutine, which in internal/app is a download's update path or
// a shared poll loop. The Host's subscriber only drops the Firing into a
// bounded channel (see Host.fire).
//
// There is no Unsubscribe: every subscriber is wired once and lives as long as
// the process.
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
// Call it outside the lock the fact came from: delivery is synchronous, and
// the Host's subscriber reaches Broadcast and every connected browser.
//
// A subscriber that panics is contained here, because an unrecovered panic on
// the publishing download goroutine would kill the process. The remaining
// subscribers still get the event.
func (b *Bus) Publish(f Firing) {
	if f.At.IsZero() {
		f.At = time.Now()
	}
	// Subscribers are called outside the read lock; one that subscribes
	// another would otherwise deadlock, since RWMutex is not reentrant.
	b.mu.RLock()
	subs := b.subs
	b.mu.RUnlock()
	for _, s := range subs {
		deliver(s, f)
	}
}

// deliver calls one subscriber with its own recover, so a panic does not skip
// the subscribers after it.
func deliver(s subscription, f Firing) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("script: bus subscriber %q panicked on %s, dropping this firing for it: %v", s.name, f.Trigger, r)
		}
	}()
	s.fn(f)
}
