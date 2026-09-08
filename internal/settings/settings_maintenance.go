package settings

// The two fields that decide whether the database ever looks after itself
// without being asked. The fields themselves are documented on the Settings
// struct; what is here is the shipped values, the clamp, and why both of them
// are what they are.

// DefaultMaintenanceIntervalDays is zero, and that is the whole shipping
// decision in this file.
//
// Zero means "only when somebody presses the button". It is what every
// existing install already has, because an int field absent from an older
// settings.json unmarshals to exactly this - so the update that introduces
// database maintenance changes the behaviour of no instance anywhere until
// somebody goes and asks for it. That is not caution for its own sake: the
// work behind this setting takes the database's one connection for the length
// of a full-file rewrite, and a version that started doing that on a timer
// because it was installed would be spending somebody's night on a decision
// they never made.
const DefaultMaintenanceIntervalDays = 0

// maxMaintenanceIntervalDays is a year, and it is a ceiling rather than a
// policy. The interface offers 30, 90 and 180; the number can also arrive
// through the advanced table, where a typo of five digits would push the next
// run past any date the box will ever see and make the setting indistinguish-
// able from off while claiming to be on. A year is long enough that nobody
// sensible is being cut off by it and short enough that "on" still means
// something will happen.
const maxMaintenanceIntervalDays = 365

// sanitizeMaintenance folds a negative interval onto zero and clamps the top.
//
// A negative is read as "off" rather than refused, which is what every other
// numeric setting in this package does (see clampDiskBytes and
// sanitizeLifecycle's own treatment of KeepFinishedDays): these values are
// typed into boxes, and a value the app cannot use should be the absence of one
// rather than an error message the user has to dismiss before the rest of their
// edits will save. A negative interval left alone would be worse than useless
// here - the due check compares against armedAt plus the interval, and a
// negative one puts that moment in the past, so the database would compact
// itself on the next tick and every tick after it.
//
// MaintenanceCompactOnSchedule needs no cleaning of its own: a bool has no
// invalid value, and it is deliberately NOT forced back to false when the
// interval is zero. Somebody who switches the schedule off for a fortnight and
// on again should find their answer to "and compact too" where they left it,
// and the interface dims that switch rather than hiding it for the same reason.
func sanitizeMaintenance(n Settings) Settings {
	if n.MaintenanceIntervalDays < 0 {
		n.MaintenanceIntervalDays = 0
	}
	if n.MaintenanceIntervalDays > maxMaintenanceIntervalDays {
		n.MaintenanceIntervalDays = maxMaintenanceIntervalDays
	}
	return n
}
