// Package confirm decides what happens to a link when a batch leaves the
// collector: what to do with one that duplicates a link already in the list
// (OnDupes) and with one a check found offline (OnOffline). Both are settled
// together, so the person confirming reads one sentence rather than two
// prompts.
//
// The caller passes in the two facts about each candidate and gets back what
// to do with it; internal/app knows what starting or removing a Task means.
package confirm

import "strings"

// Policy is what to do with a duplicate (OnDupes) or offline (OnOffline) link
// in a batch being confirmed.
type Policy string

const (
	// Include starts the link as if nothing had matched. It can never lose a
	// link the user meant to fetch.
	Include Policy = "include"
	// Exclude leaves the link in the collector, where a later confirm can
	// still start it.
	Exclude Policy = "exclude"
	// ExcludeAndRemove takes the link out of the list entirely. It is the
	// only policy that deletes anything, so it is never a default.
	ExcludeAndRemove Policy = "exclude-and-remove"
	// Ask defers to a person when one is watching; see Resolve.
	Ask Policy = "ask"
	// UseGlobal is a per-batch value that defers to the instance default. A
	// global default of UseGlobal falls back to DefaultPolicy.
	UseGlobal Policy = "use-global"
)

// DefaultPolicy applies to both OnDupes and OnOffline until someone changes
// them. It must not become ExcludeAndRemove: nothing may delete a link for a
// user who never chose that.
const DefaultPolicy = Exclude

// Policies lists every value once, in menu order. It returns a fresh slice on
// every call.
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

// Parse maps a stored value onto a policy valid as a global default, which
// excludes UseGlobal. Anything unrecognised becomes DefaultPolicy rather than
// an error, so a typo in a settings file can never turn a default into
// ExcludeAndRemove.
func Parse(s string) Policy {
	p := Policy(strings.ToLower(strings.TrimSpace(s)))
	if p.Valid() && p != UseGlobal {
		return p
	}
	return DefaultPolicy
}

// Trigger is where a confirm was set off. It only changes what Ask resolves
// to.
type Trigger string

const (
	// TriggerManual is a person confirming at the collector.
	TriggerManual Trigger = "manual"
	// TriggerAutoConfirm is the auto-confirm countdown reaching zero.
	TriggerAutoConfirm Trigger = "auto-confirm"
	// TriggerWatch is a dropped watch-folder file.
	TriggerWatch Trigger = "watch"
	// TriggerCnL is a Click'n'Load submission from a browser.
	TriggerCnL Trigger = "cnl"
)

// Interactive reports whether a person is there to answer a prompt. Only
// TriggerManual is; the others fire with nobody watching.
func (t Trigger) Interactive() bool { return t == TriggerManual }

// Config is an OnDupes/OnOffline pair. As the per-batch input to
// ResolveConfig, an empty field means the batch named nothing and reads like
// UseGlobal.
type Config struct {
	OnDupes   Policy
	OnOffline Policy
}

// Resolve turns one batch's policy into the concrete value Evaluate applies,
// given the instance default and whether anyone is watching.
//
// UseGlobal, or an invalid batch value, defers to global. Ask defers to
// global too when nobody is watching, since the answer would never come. A
// global that is itself Ask in that case, or is empty or corrupt, settles on
// DefaultPolicy. ExcludeAndRemove comes back only when batch or global named
// it.
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

// ResolveConfig resolves both policies of a batch against the instance
// defaults and the trigger.
func ResolveConfig(batch, global Config, trigger Trigger) Config {
	interactive := trigger.Interactive()
	return Config{
		OnDupes:   Resolve(batch.OnDupes, global.OnDupes, interactive),
		OnOffline: Resolve(batch.OnOffline, global.OnOffline, interactive),
	}
}
