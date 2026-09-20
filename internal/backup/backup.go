// Package backup builds the downloadable snapshot an install can be restored
// from, and validates and stages an uploaded one.
//
// The snapshot is the SQLite store plus settings.json, which also holds the
// rule sets and the schedule. A restore never replaces the live files, since
// the running process holds them open (Windows refuses the replacement, and
// the single-connection store cannot be told to reopen). Stage validates an
// upload against a throwaway copy and leaves the result for ApplyPending to
// put in place at the next start-up, before anything in the data directory
// is opened.
package backup

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/settings"
	"golang.org/x/mod/semver"
	_ "modernc.org/sqlite"
)

// The zip entry names, which are also the file names in the staging and data
// directories.
const (
	dbEntry       = "knightloader.db"
	settingsEntry = "settings.json"
	manifestEntry = "manifest.json"
)

// MaxUploadBytes bounds an uploaded bundle, and each entry read from one. An
// install with years of history is a few megabytes.
const MaxUploadBytes = 512 << 20

const (
	// pendingDirName is where a validated restore waits for the next
	// start-up.
	pendingDirName = "restore-pending"
	// stagingDirName is where Stage assembles a restore. It is renamed to
	// pendingDirName only once complete, so ApplyPending never sees a
	// half-written attempt.
	stagingDirName = "restore-pending.staging"
)

// Manifest identifies one bundle: what build made it and when.
type Manifest struct {
	Version    string    `json:"version"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Build writes one backup archive to w.
//
// settingsJSON includes every secret (router and proxy passwords, event
// target headers such as push tokens), because a restore that cannot put them
// back is not a restore. The caller's route is responsible for requiring a
// session. dbPath must be a consistent snapshot from store.Store.BackupTo,
// not the live file.
func Build(w io.Writer, manifest Manifest, settingsJSON []byte, dbPath string) (err error) {
	zw := zip.NewWriter(w)
	defer func() {
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
	}()

	mf, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	if err := writeEntry(zw, manifestEntry, mf); err != nil {
		return err
	}
	if err := writeEntry(zw, settingsEntry, settingsJSON); err != nil {
		return err
	}

	dbf, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("backup: open the database snapshot: %w", err)
	}
	defer dbf.Close()
	dw, err := zw.Create(dbEntry)
	if err != nil {
		return fmt.Errorf("backup: add %s: %w", dbEntry, err)
	}
	if _, err := io.Copy(dw, dbf); err != nil {
		return fmt.Errorf("backup: write %s: %w", dbEntry, err)
	}
	return nil
}

func writeEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("backup: add %s: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

// Stage validates an uploaded bundle and, once every check has passed, writes
// it into dataDir for ApplyPending. Nothing under dataDir is touched before
// that final step.
//
// A bundle made by a newer version than runningVersion is refused. The
// comparison is skipped when either side is not a release version, such as
// "dev".
func Stage(dataDir string, zipBytes []byte, runningVersion string) (Manifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return Manifest{}, fmt.Errorf("this is not a valid backup archive: %w", err)
	}

	manifestRaw, err := readEntry(zr, manifestEntry)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("the backup's manifest.json is not readable: %w", err)
	}

	settingsRaw, err := readEntry(zr, settingsEntry)
	if err != nil {
		return Manifest{}, err
	}
	// Decoding into the real struct catches a type mismatch from a truncated
	// or hand-edited file here, where the upload can still be named.
	var probe settings.Settings
	if err := json.Unmarshal(settingsRaw, &probe); err != nil {
		return Manifest{}, fmt.Errorf("the backup's settings.json does not match this build's settings shape: %w", err)
	}

	dbRaw, err := readEntry(zr, dbEntry)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateDatabase(dbRaw); err != nil {
		return Manifest{}, err
	}

	if semver.IsValid(manifest.Version) && semver.IsValid(runningVersion) &&
		semver.Compare(manifest.Version, runningVersion) > 0 {
		return Manifest{}, fmt.Errorf(
			"this backup was made by %s, which is newer than the %s this server is running; "+
				"upgrade the server first, then restore", manifest.Version, runningVersion)
	}

	if err := stageFiles(dataDir, settingsRaw, dbRaw, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// readEntry reads one zip entry, bounded by MaxUploadBytes because the
// declared size of an uploaded entry cannot be trusted (a decompression
// bomb).
func readEntry(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, fmt.Errorf("the backup is missing %s; it is not one this build can restore from", name)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes))
	if err != nil {
		return nil, fmt.Errorf("could not read %s from the backup: %w", name, err)
	}
	return b, nil
}

// validateDatabase opens raw as a standalone SQLite file and checks that it
// passes SQLite's integrity check and has a tasks table.
func validateDatabase(raw []byte) error {
	tmp, err := os.CreateTemp("", "kl-restore-validate-*.db")
	if err != nil {
		return fmt.Errorf("backup: could not stage a copy to validate: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("backup: could not stage a copy to validate: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backup: could not stage a copy to validate: %w", err)
	}

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return fmt.Errorf("the backup's database could not be opened: %w", err)
	}
	defer db.Close()

	var result string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("the backup's database could not be read: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("the backup's database failed its integrity check: %s", result)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&n); err != nil {
		return fmt.Errorf("the backup's database could not be read: %w", err)
	}
	if n == 0 {
		return errors.New("the backup's database has no tasks table; it is not a KnightLoader backup")
	}
	return nil
}

// stageFiles writes the restore into a fresh staging directory and commits it
// with one rename. A restore staged earlier and not yet applied is replaced.
func stageFiles(dataDir string, settingsRaw, dbRaw []byte, manifest Manifest) error {
	staging := filepath.Join(dataDir, stagingDirName)
	final := filepath.Join(dataDir, pendingDirName)

	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("backup: could not clear a previous staging attempt: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("backup: could not prepare to stage the restore: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, settingsEntry), settingsRaw, 0o600); err != nil {
		return fmt.Errorf("backup: could not stage %s: %w", settingsEntry, err)
	}
	if err := os.WriteFile(filepath.Join(staging, dbEntry), dbRaw, 0o644); err != nil {
		return fmt.Errorf("backup: could not stage %s: %w", dbEntry, err)
	}
	mf, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: could not stage the manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, manifestEntry), mf, 0o644); err != nil {
		return fmt.Errorf("backup: could not stage %s: %w", manifestEntry, err)
	}

	if err := os.RemoveAll(final); err != nil {
		return fmt.Errorf("backup: could not replace a previously staged restore: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("backup: could not commit the staged restore: %w", err)
	}
	return nil
}

// ApplyPending puts a restore left by Stage in place of the live store and
// settings file. Call it at start-up, before store.Open or settings.Load.
//
// It copies rather than moves the staged files and removes the pending
// directory only after both copies succeed, so a crash in between leaves
// everything needed to finish on the next start-up. applied reports whether
// there was anything to apply.
func ApplyPending(dataDir string) (applied bool, manifest Manifest, err error) {
	pending := filepath.Join(dataDir, pendingDirName)
	if _, statErr := os.Stat(pending); errors.Is(statErr, os.ErrNotExist) {
		return false, Manifest{}, nil
	} else if statErr != nil {
		return false, Manifest{}, fmt.Errorf("restore: could not check for a staged restore: %w", statErr)
	}

	if mf, readErr := os.ReadFile(filepath.Join(pending, manifestEntry)); readErr == nil {
		_ = json.Unmarshal(mf, &manifest) // a broken manifest must not block the restore
	}

	if err := copyFile(filepath.Join(pending, settingsEntry), filepath.Join(dataDir, settingsEntry)); err != nil {
		return false, manifest, fmt.Errorf("restore: could not put %s in place: %w", settingsEntry, err)
	}
	if err := copyFile(filepath.Join(pending, dbEntry), filepath.Join(dataDir, dbEntry)); err != nil {
		return false, manifest, fmt.Errorf("restore: could not put %s in place: %w", dbEntry, err)
	}
	// A journal left by the replaced database would be reconciled against
	// content it has never seen.
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		_ = os.Remove(filepath.Join(dataDir, dbEntry+suffix))
	}

	if err := os.RemoveAll(pending); err != nil {
		return true, manifest, fmt.Errorf(
			"restore: applied, but could not clear the staged copy (%w); "+
				"it is harmless and will be re-applied, identically, next start-up", err)
	}
	return true, manifest, nil
}

// copyFile copies src to dst through a temporary file renamed into place, so
// a crash mid-copy leaves the old dst intact. src is never modified, which
// keeps ApplyPending retryable.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".restoring"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
