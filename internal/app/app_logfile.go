package app

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// LogDirEnv moves the log file somewhere other than <dataDir>/logs. It is an
// environment variable rather than a setting because the settings page would
// offer it as an unvalidated text box, and an unwritable path would silently
// stop the log.
const LogDirEnv = "KL_LOG_DIR"

// applyLogFile makes the file sink match cfg. It runs at boot and from
// afterSettingsChange, so switching the log on takes effect without a restart,
// and switching it off closes the file right away to free the volume.
func (a *App) applyLogFile(cfg settings.LogFile) {
	if !cfg.Enabled {
		_ = logring.CloseFile()
		return
	}
	cfg = cfg.Sanitized()
	dir := a.LogDir()
	if err := logring.OpenFile(logring.FileOptions{
		Dir:      dir,
		MaxBytes: cfg.MaxBytes(),
		Keep:     cfg.Keep,
	}); err != nil {
		// Logged here rather than inside the sink, which must never log (see
		// internal/logring/file.go).
		log.Printf("the log file at %s is not being written (%v); the last %d lines stay in memory and still go into the diagnostics bundle", dir, err, logring.Capacity)
	}
}

// taskTag returns the " (task <id>)" suffix the per-download log card looks
// for, or nothing when there is no task. It is a suffix because the source
// picker reads the front of the line.
func taskTag(id string) string {
	if id == "" {
		return ""
	}
	return " (task " + id + ")"
}

// LogDir returns KL_LOG_DIR when it is set, else a "logs" folder beside the
// database. A blank value counts as unset, since that is what an empty field in
// a container template produces.
func (a *App) LogDir() string {
	if dir := strings.TrimSpace(os.Getenv(LogDirEnv)); dir != "" {
		return dir
	}
	return filepath.Join(a.DataDir, "logs")
}
