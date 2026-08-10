package settings

// settings_confirm.go: the confirm-time policy this instance applies when a
// batch leaves the collector - OnDupes, OnOffline, AddAtTop and the
// AutoConfirm/AutoConfirmDelay/AutoStart split (see their own doc comments
// on the Settings struct in settings.go) - plus the one migration that split
// demands from every existing install.

import (
	"encoding/json"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
)

// sanitizeConfirm folds OnDupes and OnOffline onto a policy this instance can
// actually apply as a GLOBAL default: any of the four real outcomes, never
// UseGlobal (a default cannot defer to itself) and never anything this build
// does not recognise - confirm.Parse already refuses both by falling back to
// confirm.DefaultPolicy, the same "an unreadable settings file can never stop
// links from being added" rule sanitizeIntake applies to MirrorPolicy and
// CollisionPolicy just above.
//
// AutoConfirmDelay is clamped the same way MaxRetries is: a negative number
// makes no sense as a wait, and a delay above a day is almost certainly a
// stray digit rather than a real preference - the countdown this bounds is
// meant to be read by a person watching it count down, not set once and
// forgotten for a week.
func sanitizeConfirm(n Settings) Settings {
	n.OnDupes = string(confirm.Parse(n.OnDupes))
	n.OnOffline = string(confirm.Parse(n.OnOffline))
	if n.AutoConfirmDelay < 0 {
		n.AutoConfirmDelay = 0
	}
	const maxAutoConfirmDelay = 24 * 60 * 60 // a day, in seconds
	if n.AutoConfirmDelay > maxAutoConfirmDelay {
		n.AutoConfirmDelay = maxAutoConfirmDelay
	}
	return n
}

// migrateAutoStart maps the single autoStart flag an older build wrote onto
// the three fields that replaced it - AutoConfirm (skip the collector
// without a click), AutoConfirmDelay (how long to wait first, which the old
// flag never had at all) and AutoStart (once something reaches the queue -
// by hand or on its own - start it immediately). It runs on the raw bytes,
// once, at load, for the reason migrateArchiveDisposal does: AutoStart keeps
// its JSON key across the split but changes what it means, so letting a
// legacy document's "autoStart" reach the new AutoStart field through the
// ordinary json.Unmarshal in Load (which it does, same key, same type) would
// carry the OLD meaning into the field under its NEW one.
//
// Detected by the absence of autoConfirm, a key no build before this one
// ever wrote. Its presence - even false - means this document already
// carries the split, and nothing here may touch it: a save from a client
// that deliberately turned autoConfirm off would otherwise be undone on
// every later load, exactly the trap TestTheNewKeyWinsOverTheOldBoolean
// already pins for the archive-disposal migration.
//
// The old flag conflated confirm and start, and there is nothing on a
// legacy document to tell the two apart, so both new fields inherit its one
// value - true maps to both true, or every install that had it on wakes up
// with every future batch parked in the collector, confirmed by nothing.
// False maps AutoConfirm to false, matching exactly what an unset flag
// always did (nothing was ever auto-confirmed), and AutoStart to true
// regardless of which way the old flag pointed: false only ever governed
// whether a batch skipped the collector on its own, and the one route that
// was ALWAYS there before this split - a person clicking "start" on a
// collected batch - has always started it immediately once clicked. That
// behaviour has nothing to do with the old flag and nothing here may change
// it retroactively, which is why AutoStart's own default (Defaults, above)
// already agrees with this unconditionally, for the installs that carry no
// legacy flag to read at all.
func migrateAutoStart(raw []byte, n Settings) Settings {
	var old struct {
		AutoStart   *bool `json:"autoStart"`
		AutoConfirm *bool `json:"autoConfirm"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return n
	}
	if old.AutoStart == nil || old.AutoConfirm != nil {
		return n
	}
	n.AutoConfirm = *old.AutoStart
	n.AutoStart = true
	return n
}
