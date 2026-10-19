package settings

// DefaultBackupKeep is how many automatic backups stay: a week of them at one
// a day. An int absent from an older settings.json keeps this value, since
// Load decodes over Defaults().
const DefaultBackupKeep = 7

// maxBackupIntervalDays is a year, as for the maintenance interval: a typo of
// five digits would leave the schedule claiming to be on while nothing is
// ever written.
const maxBackupIntervalDays = 365

// maxBackupKeep bounds the files kept. Each holds the whole database, so a
// thousand of them is already more than a data volume should carry.
const maxBackupKeep = 1000

// sanitizeBackup reads a negative interval as off, like every number typed
// into a box here. Fewer than one backup to keep would delete the one just
// written, so the floor is one.
func sanitizeBackup(n Settings) Settings {
	n.BackupIntervalDays = min(max(n.BackupIntervalDays, 0), maxBackupIntervalDays)
	n.BackupKeep = min(max(n.BackupKeep, 1), maxBackupKeep)
	return n
}
