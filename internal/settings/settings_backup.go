package settings

import (
	"path/filepath"
	"strings"
)

// DefaultBackupIntervalDays and DefaultBackupKeep are a week of daily
// backups, what switching automatic backups on starts with.
const (
	DefaultBackupIntervalDays = 1
	DefaultBackupKeep         = 7
)

// maxBackupIntervalDays is a year, as for the maintenance interval: a typo of
// five digits would leave the schedule claiming to be on while nothing is
// ever written.
const maxBackupIntervalDays = 365

// maxBackupKeep bounds the files kept. Each holds the whole database, so a
// thousand of them is already more than a volume should carry.
const maxBackupKeep = 1000

// sanitizeBackup clamps both numbers to at least one: fewer than one backup
// kept would delete the one just written. A relative folder would resolve
// against the working directory; a save refuses one (CheckFolders), so this
// only cleans a hand-edited settings file.
func sanitizeBackup(n Settings) Settings {
	n.BackupIntervalDays = min(max(n.BackupIntervalDays, 1), maxBackupIntervalDays)
	n.BackupKeep = min(max(n.BackupKeep, 1), maxBackupKeep)
	n.BackupDir = strings.TrimSpace(n.BackupDir)
	if n.BackupDir != "" && !filepath.IsAbs(n.BackupDir) {
		n.BackupDir = ""
	}
	return n
}
