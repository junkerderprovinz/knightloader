package settings

// The two fields that decide whether the database ever looks after itself
// without being asked. The fields themselves are documented on the Settings
// struct; what is here is the shipped values, the clamp, and why both of them
// are what they are.

// DefaultMaintenanceIntervalDays is zero, meaning only when somebody presses
// the button. An int field absent from an older settings.json unmarshals to
// exactly this, so the update that introduces database maintenance changes no
// instance's behaviour until it is asked to. The work behind this setting takes
// the database's one connection for the length of a full-file rewrite, which is
// not a thing to start doing on a timer because a version was installed.
const DefaultMaintenanceIntervalDays = 0

// maxMaintenanceIntervalDays is a year, a ceiling rather than a policy. The
// interface offers 30, 90 and 180, but the number can also arrive through the
// advanced table, where a typo of five digits would push the next run past any
// date the box will see and leave the setting claiming to be on while nothing
// happens.
const maxMaintenanceIntervalDays = 365

// sanitizeMaintenance folds a negative interval onto zero and clamps the top.
//
// A negative is read as off rather than refused, what every numeric setting in
// this package does (see clampDiskBytes, and KeepFinishedDays in
// sanitizeLifecycle): these values are typed into boxes, and one the app cannot
// use should be the absence of a value rather than an error to dismiss before
// the rest of the edits will save. Left alone, a negative interval would be
// worse than useless here, since the due check compares against armedAt plus
// the interval and a negative one puts that moment in the past, so the database
// would compact itself on every tick.
//
// MaintenanceCompactOnSchedule needs no cleaning: a bool has no invalid value,
// and it is not forced back to false when the interval is zero. Somebody who
// switches the schedule off for a fortnight and on again finds their answer to
// "and compact too" where they left it, which is why the interface dims that
// switch rather than hiding it.
func sanitizeMaintenance(n Settings) Settings {
	if n.MaintenanceIntervalDays < 0 {
		n.MaintenanceIntervalDays = 0
	}
	if n.MaintenanceIntervalDays > maxMaintenanceIntervalDays {
		n.MaintenanceIntervalDays = maxMaintenanceIntervalDays
	}
	return n
}
