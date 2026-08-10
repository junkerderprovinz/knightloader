// Package confirm decides what happens to a link at the moment a batch
// leaves the collector: what to do with one that duplicates a link already in
// the list (OnDupes) and with one a check has already found offline
// (OnOffline), settled together so the person confirming a batch reads one
// sentence instead of two prompts back to back.
//
// It knows nothing about a Task, a store or a queue. It is handed the two
// facts it needs about each candidate - already-seen, already-offline - and
// hands back what to do with each one and the sentence that explains it. The
// caller (internal/app) is the only place that knows what a Task is and what
// "start" or "remove" actually does to one - see internal/app/app_confirm.go.
package confirm

import "strings"

// Policy is what to do with a link a batch is about to confirm that either
// duplicates one already in the list (OnDupes) or has already been checked
// and found gone (OnOffline).
type Policy string

const (
	// Include starts the link exactly as if nothing had matched it. It is
	// the safest reading of a signal that might be wrong, and the only one
	// of the five that can never lose a link the user meant to fetch.
	Include Policy = "include"
	// Exclude leaves the link in the collector: it is not part of this
	// confirm, and nothing else about it changes. It is exactly as
	// confirmable a moment later as it was a moment before - confirming
	// again with a policy that would now include it reaches it the same as
	// any other collected link.
	Exclude Policy = "exclude"
	// ExcludeAndRemove takes the link out of the list entirely. It is the
	// only one of the five that deletes anything, which is why it may never
	// be a default - see DefaultPolicy.
	ExcludeAndRemove Policy = "exclude-and-remove"
	// Ask defers to a person, when one is watching to ask - see Resolve for
	// what happens when nobody is.
	Ask Policy = "ask"
	// UseGlobal is a per-batch value only: it defers to whatever the
	// instance's own default currently is. A global default cannot itself
	// use-global - Resolve treats that the same as an empty or malformed
	// value and falls back to DefaultPolicy rather than looping.
	UseGlobal Policy = "use-global"
)

// DefaultPolicy is what a fresh install, and every setting nothing has ever
// changed, applies to both OnDupes and OnOffline. It is Exclude, and it may
// never become ExcludeAndRemove: Exclude leaves a link exactly where it was,
// recoverable by confirming again, and nothing may delete a link on behalf of
// a user who never touched the setting - only a policy chosen on purpose is
// allowed to do that.
const DefaultPolicy = Exclude

// Policies lists every value once, in the order a menu should offer them:
// the plain outcomes first, from doing nothing through to the one that
// deletes, then the two values that are not outcomes in themselves. Built
// fresh on every call so a caller sorting or filtering it cannot reorder the
// menu for everyone else.
func Policies() []Policy {
	return []Policy{Include, Exclude, ExcludeAndRemove, Ask, UseGlobal}
}

// Valid reports whether p is a value this package implements.
func (p Policy) Valid() bool {
	switch p {
	case Include, Exclude, ExcludeAndRemove, Ask, UseGlobal:
		return true
	}
	return false
}

// Parse maps a stored value onto a policy valid for a GLOBAL default, which
// is every value except UseGlobal - a global default cannot defer to itself.
// Anything unrecognised, UseGlobal included, becomes DefaultPolicy rather
// than an error, for the same reason dedupe.ParsePolicy and
// collide.ParsePolicy fold rather than fail: a settings file written by
// another build, or a hand-edited typo, must never be able to turn a default
// into ExcludeAndRemove by accident.
func Parse(s string) Policy {
	p := Policy(strings.ToLower(strings.TrimSpace(s)))
	if p.Valid() && p != UseGlobal {
		return p
	}
	return DefaultPolicy
}

// Trigger is where a confirm was set off. It changes nothing about how
// OnDupes or OnOffline are read, except what Ask resolves to - see Resolve.
type Trigger string

const (
	// TriggerManual is a person at the collector, confirming by hand - the
	// one trigger with somebody there to answer a prompt.
	TriggerManual Trigger = "manual"
	// TriggerAutoConfirm is the delayed auto-confirm countdown reaching
	// zero on its own (Settings.AutoConfirm / AutoConfirmDelay).
	TriggerAutoConfirm Trigger = "auto-confirm"
	// TriggerWatch is a dropped watch-folder file.
	TriggerWatch Trigger = "watch"
	// TriggerCnL is a Click'n'Load submission from a browser.
	TriggerCnL Trigger = "cnl"
)

// Interactive reports whether a person is at the keyboard to answer a
// prompt. Only TriggerManual is: the other three all fire with nobody
// watching, which is exactly why Ask has to resolve to something else for
// them rather than leaving a batch waiting on an answer that is never
// coming.
func (t Trigger) Interactive() bool { return t == TriggerManual }

// Config is a resolved OnDupes/OnOffline pair - see Resolve and
// ResolveConfig. It doubles as the per-batch input to ResolveConfig, where
// the zero value (both fields "") means "this batch named neither", read
// exactly like UseGlobal because an unset Policy fails Valid the same way
// UseGlobal is folded - a caller building one by hand for "no override" does
// not have to spell out confirm.UseGlobal on every field to get it.
type Config struct {
	OnDupes   Policy
	OnOffline Policy
}

// Resolve turns one batch's policy into the concrete value Evaluate applies,
// given the instance's own default and whether this confirm has anyone
// watching to answer a prompt.
//
// UseGlobal always defers to global, whatever global turns out to be - Ask
// included. Ask defers the same way, but only when nobody is watching:
// interactive is false for auto-confirm, the watch folder and Click'n'Load
// alike, and a batch that asked anyway would simply never resolve. Whatever
// global itself turns out to be is run through the same two rules again, so
// a global default that is itself Ask (a person confirming by hand always
// gets asked, everything else falls through to whatever global names beyond
// that) does not leave a non-interactive caller stuck on Ask a second time -
// and a global value this package does not recognise at all (empty, a typo,
// UseGlobal, which a global default may not carry but a corrupt settings
// file could produce anyway) settles on DefaultPolicy rather than being
// asked a third time. ExcludeAndRemove is only ever returned when the batch
// or the global default named it outright - nothing here ever substitutes
// it in.
func Resolve(batch, global Policy, interactive bool) Policy {
	p := batch
	if p == UseGlobal || !p.Valid() {
		p = global
	}
	if p == Ask && !interactive {
		p = global
		if p == Ask || p == UseGlobal || !p.Valid() {
			p = DefaultPolicy
		}
	}
	if p == UseGlobal || !p.Valid() {
		p = DefaultPolicy
	}
	return p
}

// ResolveConfig resolves a whole batch's OnDupes/OnOffline pair against the
// instance's own defaults and the trigger it fired from, in one call.
func ResolveConfig(batch, global Config, trigger Trigger) Config {
	interactive := trigger.Interactive()
	return Config{
		OnDupes:   Resolve(batch.OnDupes, global.OnDupes, interactive),
		OnOffline: Resolve(batch.OnOffline, global.OnOffline, interactive),
	}
}
