package settings

import (
	"os"
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

// checkBackupDir refuses a backup folder that is not absolute, cannot be made
// or cannot be written, which it finds out by making the folder and writing a
// probe file into it. What the check made is removed again, since the settings
// page saves while a path is still being typed; the first backup makes the
// folder for good. Empty is the default folder and always passes.
func checkBackupDir(what, dir string) error {
	if dir == "" {
		return nil
	}
	if !filepath.IsAbs(dir) {
		return &PathProblem{What: what, Code: "notAbsolute", Dir: dir}
	}
	existing := nearestExisting(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &PathProblem{What: what, Code: "cannotCreate", Dir: dir, Err: err}
	}
	defer func() {
		for d := dir; d != existing; d = filepath.Dir(d) {
			os.Remove(d)
		}
	}()
	probe := filepath.Join(dir, WriteProbeName)
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return &PathProblem{What: what, Code: "cannotWrite", Dir: dir, Err: err}
	}
	return os.Remove(probe)
}
