// Package backup builds the downloadable snapshot of everything a restore
// needs to bring an install back, and validates and stages an uploaded one.
//
// The snapshot is the SQLite store (tasks, history, ui state — one file,
// see store.Store.BackupTo) plus settings.json, which is also where the
// rule sets (Packagizer, LinkFilter) and the timetable (Schedule) already
// live — see settings.Settings's own doc comment. There is no separate
// rules.json or schedule.json to also bundle; grep the tree before assuming
// otherwise, because an earlier design sketch did split them out and the
// wiring that landed did not.
//
// A restore never touches the live store or settings file directly. Both
// are files a running process already has open, and neither this process
// nor SQLite is happy about that file changing out from under the open
// handle — on Windows a file already open cannot even be replaced by that
// route, and on every platform a store built for exactly one connection
// (store.Open's SetMaxOpenConns(1)) has no way to be told "the bytes under
// you just changed, reopen". So the promise this package keeps instead is:
// validate an upload to exhaustion against a throwaway copy, and only once
// every check has passed, stage the result where the NEXT process
// start-up — before it opens anything in the data directory at all — will
// find and apply it. See ApplyPending.
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

// The zip entry names, which double as the on-disk file names both inside
// the staging directory and in the data directory itself — one constant
// each rather than two spellings that could drift apart.
const (
	dbEntry       = "knightloader.db"
	settingsEntry = "settings.json"
	manifestEntry = "manifest.json"
)

// MaxUploadBytes bounds an uploaded bundle. An install with years of task
// history is a few megabytes; this leaves two orders of magnitude of
// headroom before a restore is refused as unreasonable, rather than the
// server buffering however much a request claims to be sending.
const MaxUploadBytes = 512 << 20

const (
	// pendingDirName is where a fully validated restore waits for the next
	// start-up to apply it.
	pendingDirName = "restore-pending"
	// stagingDirName is where Stage writes while it is still assembling
	// that restore. It is renamed to pendingDirName only once every file is
	// down, so ApplyPending — which only ever looks at pendingDirName — can
	// never see a half-written attempt.
	stagingDirName = "restore-pending.staging"
)

// Manifest identifies one bundle: what build made it and when. It travels
// inside the archive as manifest.json and is what a rejected or accepted
// restore can name in its answer, instead of "invalid file".
type Manifest struct {
	Version    string    `json:"version"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Build writes one backup archive to w.
//
// settingsJSON is expected to be the running instance's settings encoded
// exactly as it holds them — secrets included. That is deliberate and safe
// here in a way it would not be on an ordinary settings read: a backup that
// cannot put a router or proxy password back is not a backup, and this
// package has no HTTP concerns of its own, so the caller (the /api/system/
// backup route) is what is responsible for requiring the session that every
// other route needs. See that route's own comment.
//
// dbPath is expected to already be a consistent, standalone snapshot —
// store.Store.BackupTo, which uses SQLite's VACUUM INTO — not a raw copy of
// the live file, which could race whatever else is writing to it at the
// same moment.
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

// Stage validates an uploaded bundle to exhaustion and, only once every
// check has passed, writes it into dataDir where ApplyPending will find it
// at the next process start-up. Nothing under dataDir is touched before
// that last, all-or-nothing step — every check up to it runs against the
// upload itself and a throwaway copy of its database, never against
// anything live.
//
// runningVersion is compared against the manifest so a backup made by a
// newer build than the one asked to restore it is refused with a reason,
// rather than handed to a schema and a settings shape this build may not
// fully understand. Either side being unparseable as a released version
// (buildinfo.Version is "dev" on every untagged build) skips the
// comparison rather than guessing.
//
// On success it returns the manifest that was staged, so the caller can
// report what is about to be applied.
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
	// Decoded into the real struct, not merely checked as JSON: a type
	// mismatch here (a string where a number belongs) is exactly what a
	// truncated or hand-edited upload produces, and json.Unmarshal already
	// refuses that for free — a generic "is this JSON" check would let it
	// through and hand the mismatch to Settings.Set on the next boot
	// instead, far from where the upload that caused it can be named.
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

// readEntry reads one zip entry fully, bounded the same way an upload's
// total size already is — a zip entry's declared size is attacker-supplied
// the moment this is fed an upload rather than a backup this instance wrote
// itself, and decompression bombs are exactly a small file claiming to
// unpack into an enormous one.
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

// validateDatabase opens raw as a standalone SQLite file — never the live
// one — and runs the checks that separate "a KnightLoader backup" from
// "some other file with this name": SQLite's own integrity check, and the
// presence of the tasks table every migration since the first has assumed
// exists.
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

// stageFiles writes settingsRaw, dbRaw and manifest into a fresh staging
// directory, then commits it with one rename — so ApplyPending, which only
// ever looks at pendingDirName, never observes a partly written attempt,
// and a process killed mid-write leaves nothing for it to find at all.
//
// A restore staged before this one, if there was one and nobody has
// restarted to apply it yet, is replaced: the most recently validated
// upload wins, the same way saving a settings page twice replaces the
// first save rather than merging with it.
func stageFiles(dataDir string, settingsRaw, dbRaw []byte, manifest Manifest) error {
	staging := filepath.Join(dataDir, stagingDirName)
	final := filepath.Join(dataDir, pendingDirName)

	// A leftover from an earlier, abandoned attempt — the process was
	// killed mid-write, or this validation simply never got as far as the
	// commit rename below. Either way, starting clean beats layering a new
	// attempt on top of a half-written one.
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

// ApplyPending looks for a restore Stage left behind and, if there is one,
// puts it in place of the live store and settings file. Call it at
// start-up, before store.Open or settings.Load touch either — see the
// package doc comment for why that ordering is what makes this safe on
// every platform this ships for.
//
// It copies rather than moves the staged files into the data directory, and
// removes the staging directory only once both copies have succeeded — so a
// crash between the two leaves the pending directory exactly as able to
// finish the job on the NEXT start-up as it was on this one, instead of
// missing the file the first copy already consumed by moving it away.
//
// applied reports whether there was anything to apply, so the caller can
// log accordingly; a missing pending directory is the ordinary case on
// every boot that is not completing a restore; and is not an error.
func ApplyPending(dataDir string) (applied bool, manifest Manifest, err error) {
	pending := filepath.Join(dataDir, pendingDirName)
	if _, statErr := os.Stat(pending); errors.Is(statErr, os.ErrNotExist) {
		return false, Manifest{}, nil
	} else if statErr != nil {
		return false, Manifest{}, fmt.Errorf("restore: could not check for a staged restore: %w", statErr)
	}

	if mf, readErr := os.ReadFile(filepath.Join(pending, manifestEntry)); readErr == nil {
		_ = json.Unmarshal(mf, &manifest) // best effort — a missing or corrupt manifest must not block the restore it describes
	}

	if err := copyFile(filepath.Join(pending, settingsEntry), filepath.Join(dataDir, settingsEntry)); err != nil {
		return false, manifest, fmt.Errorf("restore: could not put %s in place: %w", settingsEntry, err)
	}
	if err := copyFile(filepath.Join(pending, dbEntry), filepath.Join(dataDir, dbEntry)); err != nil {
		return false, manifest, fmt.Errorf("restore: could not put %s in place: %w", dbEntry, err)
	}
	// A journal left behind by whichever database sat here before must not
	// survive onto the one that just replaced it — SQLite would try to
	// reconcile it against content it has never seen. Best effort: a
	// missing file (the ordinary case — the previous process shut down
	// cleanly) is not an error, and nothing here is fatal to the restore
	// that already succeeded above.
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

// copyFile copies src to dst through a temporary file beside dst, renamed
// into place only once fully written — so a process killed mid-copy leaves
// the OLD dst intact rather than a truncated new one masquerading as it.
// src is never modified, which is what makes ApplyPending safe to retry.
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
