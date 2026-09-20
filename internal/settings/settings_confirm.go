package settings

// The confirm-time policy this instance applies when a batch leaves the
// collector: OnDupes, OnOffline, AddAtTop and the AutoConfirm,
// AutoConfirmDelay and AutoStart split, whose doc comments are on the Settings
// struct, plus the migration that split demands from every existing install.

import (
	"encoding/json"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
)

// sanitizeConfirm folds OnDupes and OnOffline onto a policy this instance can
// apply as a global default: any of the four real outcomes, never UseGlobal,
// since a default cannot defer to itself, and never a value this build does not
// recognise. confirm.Parse refuses both by falling back to
// confirm.DefaultPolicy, the rule sanitizeIntake applies to MirrorPolicy and
// CollisionPolicy so that an unreadable settings file can never stop links from
// being added.
//
// AutoConfirmDelay is clamped the way MaxRetries is: a negative number is not a
// wait, and a delay above a day is a stray digit. The countdown it bounds is
// meant to be watched.
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

// migrateAutoStart maps the single autoStart flag an older build wrote onto the
// three fields that replaced it: AutoConfirm, AutoConfirmDelay and AutoStart.
// It runs on the raw bytes, once, at load, for the reason
// migrateArchiveDisposal does: AutoStart keeps its JSON key across the split
// but changes what it means, so a legacy document's "autoStart" reaching the
// new field through the ordinary json.Unmarshal in Load would carry the old
// meaning into the field under its new one.
//
// The absence of autoConfirm, a key no earlier build wrote, is what marks a
// legacy document. Its presence, even false, means the document already carries
// the split and nothing here may touch it, or a save from a client that turned
// autoConfirm off would be undone on every later load.
//
// The old flag conflated confirm and start, and a legacy document has nothing
// to tell the two apart. True maps to AutoConfirm true, or every install that
// had it on wakes up with every future batch parked in the collector. False
// maps AutoConfirm to false, which is what an unset flag always did, and
// AutoStart to true either way: clicking "start" on a collected batch has
// always started it, and the old flag only governed whether a batch skipped the
// collector on its own.
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
