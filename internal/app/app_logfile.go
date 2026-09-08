package app

// Arming and disarming the optional log file, from the settings document.

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// LogDirEnv is the environment variable that moves the log somewhere other
// than <dataDir>/logs.
//
// It is an environment variable and NOT a settings field, and that is the whole
// of this feature's path story. routes_features.go reflects over the Settings
// struct to build the Advanced key table, so a string field there would appear
// as a free text box with no validation of its own: a path that does not exist,
// or that the account inside the container may not write, would stop the log
// with nothing on screen connecting the two. A variable is set once by somebody
// standing at the machine, is visible in the container's own configuration, and
// is read at boot exactly the way KL_DATA, KL_JD and KL_CNL already are.
const LogDirEnv = "KL_LOG_DIR"

// applyLogFile makes the file sink match the configuration - the same shape
// applyFeeds and applyWatchFolders use, and wired at the same TWO places they
// are.
//
// TWO CALL SITES, AND MISSING EITHER IS THE WHOLE BUG. Boot, so that an
// instance which already had the setting on writes its log from the moment it
// starts; and afterSettingsChange, so that switching it on takes effect now
// rather than at the next restart - which is the one thing a diagnostics
// setting must never need, because the person flipping it is already trying to
// catch something.
//
// It takes the block rather than the whole settings document so that this file
// depends on one small type instead of on the shape of everything.
//
// Off means the sink is detached and the handle closed, and that is deliberately
// not "leave the old file open until a restart": somebody switching this off is
// usually doing it because the volume is filling up.
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
		// Logged from HERE and never from inside the sink. This runs on the
		// boot or settings-save goroutine, holding no lock of the ring, so the
		// line goes through the tap once and lands in the ring, on stderr and
		// in the diagnostics bundle like any other. The sink it then reaches is
		// the one that just failed to open, which drops it without a word
		// rather than trying again - see internal/logring/file.go's rule 1 for
		// why a failure path down there may never log at all.
		log.Printf("the log file at %s is not being written (%v); the last %d lines stay in memory and still go into the diagnostics bundle", dir, err, logring.Capacity)
	}
}

// taskTag is the " (task <id>)" a log line carries so that the per-download log
// card can find it, or nothing at all when there is no download to name.
//
// A bare "(task )" would be worse than saying nothing. logring.TaskIDOf matches
// the literal word followed by an id and would find none there, so the empty
// parenthesis would sit in every one of those lines for ever, explaining
// nothing to anybody reading the log by eye either.
//
// It is a SUFFIX and not a prefix on purpose: the front of a line is what the
// source picker reads, and moving the id there would file a checksum failure
// under "task" rather than under "checksum", which is the bucket somebody
// chasing a bad hash would actually pick.
func taskTag(id string) string {
	if id == "" {
		return ""
	}
	return " (task " + id + ")"
}

// LogDir is where the log file goes: KL_LOG_DIR when it is set, else a "logs"
// folder beside the database.
//
// Exported because the diagnostics route reports it and because a person
// reading a bug report needs to be told where to look. Blank and whitespace-only
// values are treated as unset rather than as "write to the working directory",
// which is what an empty variable in a container template usually means.
func (a *App) LogDir() string {
	if dir := strings.TrimSpace(os.Getenv(LogDirEnv)); dir != "" {
		return dir
	}
	return filepath.Join(a.DataDir, "logs")
}
