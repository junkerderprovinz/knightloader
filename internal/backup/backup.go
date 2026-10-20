// Package backup builds the downloadable snapshot an install can be restored
// from, and validates and stages an uploaded one.
//
// The snapshot is the SQLite store, settings.json, which also holds the rule
// sets and the schedule, and the state files that make up the rest of the
// instance (StateFiles). Every secret in it is in the clear, so it restores
// under another app key or none; the archive as a whole is sealed instead. A
// restore never replaces the live files, since the running process holds them
// open (Windows refuses the replacement, and the single-connection store
// cannot be told to reopen). Stage validates an upload against a throwaway
// copy and leaves the result for ApplyPending to put in place at the next
// start-up, before anything in the data directory is opened.
package backup

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
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
	// readyDirName is where ApplyPending seals the staged files with the
	// running instance's key before any of them replaces a live one.
	readyDirName = "restore-ready"
	// previousDirName holds the live files a restore replaces until every new
	// one is in place, so a failed swap can put them back.
	previousDirName = "restore-previous"
)

// Manifest identifies one bundle: what build made it and when.
type Manifest struct {
	Version    string    `json:"version"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"createdAt"`
	// Files are the state files the bundle speaks for. It carries those the
	// instance had, and a restore removes the live copy of the others.
	Files []string `json:"files,omitempty"`
	// KeyID is the ID of the app key the login password was set under
	// (appkey.Key.ID), empty without a key.
	KeyID string `json:"keyId,omitempty"`
}

// Build writes one backup archive to w.
//
// settingsJSON and files carry every secret in the clear, because a restore
// that cannot put them back is not a restore. The caller's route is
// responsible for requiring a session. dbPath must be a consistent snapshot
// from store.Store.BackupTo, not the live file. files maps names out of
// StateFiles to their content.
func Build(w io.Writer, manifest Manifest, settingsJSON []byte, dbPath string, files map[string][]byte) (err error) {
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
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := writeEntry(zw, name, files[name]); err != nil {
			return err
		}
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
	u, err := validate(zipBytes, runningVersion)
	if err != nil {
		return Manifest{}, err
	}
	if err := stageFiles(dataDir, u.settings, u.db, u.manifest, u.files); err != nil {
		return Manifest{}, err
	}
	return u.manifest, nil
}

// Check validates an uploaded bundle as Stage does and writes nothing, so a
// restore can be confirmed knowing what it brings. paired reports whether the
// instance it was made on was in a pairing group, whose identity the restore
// takes over.
func Check(zipBytes []byte, runningVersion string) (manifest Manifest, paired bool, err error) {
	u, err := validate(zipBytes, runningVersion)
	if err != nil {
		return Manifest{}, false, err
	}
	group, ok := u.files["group.json"]
	return u.manifest, ok && federation.GroupJoined(group), nil
}

// upload is a bundle that passed every check.
type upload struct {
	manifest     Manifest
	settings, db []byte
	files        map[string][]byte
}

func validate(zipBytes []byte, runningVersion string) (upload, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return upload{}, fmt.Errorf("this is not a valid backup archive: %w", err)
	}

	manifestRaw, err := readEntry(zr, manifestEntry)
	if err != nil {
		return upload{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return upload{}, fmt.Errorf("the backup's manifest.json is not readable: %w", err)
	}

	settingsRaw, err := readEntry(zr, settingsEntry)
	if err != nil {
		return upload{}, err
	}
	// Decoding into the real struct catches a type mismatch from a truncated
	// or hand-edited file here, where the upload can still be named.
	var probe settings.Settings
	if err := json.Unmarshal(settingsRaw, &probe); err != nil {
		return upload{}, fmt.Errorf("the backup's settings.json does not match this build's settings shape: %w", err)
	}

	dbRaw, err := readEntry(zr, dbEntry)
	if err != nil {
		return upload{}, err
	}
	dbRaw, err = cleanDatabase(dbRaw)
	if err != nil {
		return upload{}, err
	}

	if semver.IsValid(manifest.Version) && semver.IsValid(runningVersion) &&
		semver.Compare(manifest.Version, runningVersion) > 0 {
		return upload{}, fmt.Errorf(
			"this backup was made by %s, which is newer than the %s this server is running; "+
				"upgrade the server first, then restore", manifest.Version, runningVersion)
	}

	files, err := readStateFiles(zr, manifest.Files)
	if err != nil {
		return upload{}, err
	}
	return upload{manifest: manifest, settings: settingsRaw, db: dbRaw, files: files}, nil
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

// cleanDatabase opens raw as a standalone SQLite file, checks that it passes
// SQLite's integrity check and has a tasks table, and returns it stripped (see
// stripDatabase).
func cleanDatabase(raw []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "kl-restore-validate-")
	if err != nil {
		return nil, fmt.Errorf("backup: could not stage a copy to validate: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, dbEntry)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("backup: could not stage a copy to validate: %w", err)
	}
	if err := checkDatabase(path); err != nil {
		return nil, err
	}
	if err := stripDatabase(path); err != nil {
		return nil, fmt.Errorf("the backup's database could not be cleaned: %w", err)
	}
	return os.ReadFile(path)
}

func checkDatabase(path string) error {
	db, err := sql.Open("sqlite", path)
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

	// Staged, it would keep every later start from opening the store.
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("the backup's database could not be read: %w", err)
	}
	if version > store.SchemaVersion() {
		return fmt.Errorf("%w: the backup's database is at schema version %d and this build knows %d; "+
			"update this server first, then restore", store.ErrNewerSchema, version, store.SchemaVersion())
	}
	return nil
}

// stripDatabase removes every trigger and view from the SQLite file at path,
// and anything else in its schema that is not a table or an index, before it
// empties its passkeys table. They are code the file brings along, and a
// trigger on passkeys would write a removed key straight back. KnightLoader's
// own schema has none of them.
func stripDatabase(path string) error {
	if err := dropCode(path); err != nil {
		return err
	}
	// A fresh connection, which reads the schema without what dropCode deleted.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	// What the tables still carry, a default or a check, may call no function
	// with side effects.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA trusted_schema = OFF`); err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'passkeys'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		if _, err := db.Exec(`DELETE FROM passkeys`); err != nil {
			return err
		}
	}
	// Rebuilt, so neither the definitions nor the keys stay behind on free
	// pages.
	if _, err := db.Exec(`VACUUM`); err != nil {
		return err
	}
	var result string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity check after cleaning: %s", result)
	}
	return nil
}

// dropCode deletes every schema row that is not a plain table or index. The
// type column is not what SQLite goes by when it loads the schema, so a row is
// judged by the statement it holds as well, and both have to agree. The rows
// are deleted by rowid, so nothing read from the file becomes part of a
// statement.
func dropCode(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	// writable_schema is a setting of one connection.
	db.SetMaxOpenConns(1)
	rows, err := db.Query(`SELECT rowid, type, sql FROM sqlite_master`)
	if err != nil {
		return err
	}
	var code []int64
	for rows.Next() {
		var rowid int64
		var kind string
		var stmt sql.NullString
		if err := rows.Scan(&rowid, &kind, &stmt); err != nil {
			rows.Close()
			return err
		}
		if !plainSchema(kind, stmt) {
			code = append(code, rowid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(code) == 0 {
		return nil
	}
	if _, err := db.Exec(`PRAGMA writable_schema = ON`); err != nil {
		return err
	}
	for _, rowid := range code {
		if _, err := db.Exec(`DELETE FROM sqlite_master WHERE rowid = ?`, rowid); err != nil {
			return err
		}
	}
	_, err = db.Exec(`PRAGMA writable_schema = OFF`)
	return err
}

// plainSchema reports whether a schema row of the given type and statement is
// a table or an index and nothing else. An index SQLite made for a constraint
// has no statement.
func plainSchema(kind string, stmt sql.NullString) bool {
	kind = strings.ToLower(kind)
	if !stmt.Valid {
		return kind == "index"
	}
	words := leadingWords(stmt.String, 3)
	if len(words) < 2 || words[0] != "create" {
		return false
	}
	switch {
	case words[1] == "table":
		return kind == "table"
	case words[1] == "index", words[1] == "unique" && len(words) == 3 && words[2] == "index":
		return kind == "index"
	}
	return false
}

// leadingWords returns up to n words at the start of a statement in lower
// case, skipping white space and comments, and stops at anything else.
func leadingWords(stmt string, n int) []string {
	var words []string
	for i := 0; i < len(stmt) && len(words) < n; {
		switch c := stmt[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			i++
		case strings.HasPrefix(stmt[i:], "--"):
			end := strings.IndexByte(stmt[i:], '\n')
			if end < 0 {
				return words
			}
			i += end + 1
		case strings.HasPrefix(stmt[i:], "/*"):
			end := strings.Index(stmt[i+2:], "*/")
			if end < 0 {
				return words
			}
			i += end + 4
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			start := i
			for i < len(stmt) && (stmt[i] >= 'a' && stmt[i] <= 'z' || stmt[i] >= 'A' && stmt[i] <= 'Z') {
				i++
			}
			words = append(words, strings.ToLower(stmt[start:i]))
		default:
			return words
		}
	}
	return words
}

// stageFiles writes the restore into a fresh staging directory and commits it
// with one rename. A restore staged earlier and not yet applied is replaced.
func stageFiles(dataDir string, settingsRaw, dbRaw []byte, manifest Manifest, files map[string][]byte) error {
	staging := filepath.Join(dataDir, stagingDirName)
	final := filepath.Join(dataDir, pendingDirName)

	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("backup: could not clear a previous staging attempt: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
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
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(staging, name), b, 0o600); err != nil {
			return fmt.Errorf("backup: could not stage %s: %w", name, err)
		}
	}

	if err := os.RemoveAll(final); err != nil {
		return fmt.Errorf("backup: could not replace a previously staged restore: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("backup: could not commit the staged restore: %w", err)
	}
	return nil
}

// ApplyPending puts a restore left by Stage in place of the live files. Call
// it at start-up, before anything in dataDir is opened, with the key the
// instance runs on: every secret is sealed with it before a live file is
// touched.
//
// It is all or nothing. The staged files are prepared in a folder of their
// own first, so one that cannot be prepared changes nothing. The swap moves
// each live file aside before its replacement goes in, and a failure puts
// them back. The staged copy is removed only once every file is in place, so
// a crash in between leaves everything needed to finish on the next start-up.
// applied reports whether there was anything to apply.
func ApplyPending(dataDir string, key *appkey.Key) (applied bool, manifest Manifest, err error) {
	pending := filepath.Join(dataDir, pendingDirName)
	if _, statErr := os.Stat(pending); errors.Is(statErr, os.ErrNotExist) {
		return false, Manifest{}, nil
	} else if statErr != nil {
		return false, Manifest{}, fmt.Errorf("restore: could not check for a staged restore: %w", statErr)
	}
	if err := undoSwap(dataDir); err != nil {
		return false, Manifest{}, fmt.Errorf("restore: could not put back the files of an interrupted restore: %w", err)
	}

	if mf, readErr := os.ReadFile(filepath.Join(pending, manifestEntry)); readErr == nil {
		_ = json.Unmarshal(mf, &manifest) // a broken manifest must not block the restore
	}
	if err := checkStateNames(manifest.Files); err != nil {
		return false, manifest, fmt.Errorf("restore: %w", err)
	}

	ready := filepath.Join(dataDir, readyDirName)
	defer os.RemoveAll(ready)
	install, remove, err := prepare(pending, ready, dataDir, key, manifest)
	if err != nil {
		return false, manifest, fmt.Errorf("restore: could not prepare the staged files: %w", err)
	}
	if err := swap(dataDir, ready, install, remove); err != nil {
		return false, manifest, fmt.Errorf("restore: could not put the restored files in place, and the old ones are back: %w", err)
	}

	if err := os.RemoveAll(pending); err != nil {
		return true, manifest, fmt.Errorf(
			"restore: applied, but could not clear the staged copy (%w); "+
				"it is harmless and will be re-applied, identically, next start-up", err)
	}
	return true, manifest, nil
}

// prepare copies the staged files into ready and seals them there with key,
// each through its own package's open, and returns the names to put in place
// and the live ones to remove.
//
// The database is stripped again, for a restore staged by a build that did
// not strip it, brought to this build's schema, and given the live database's
// passkeys in place of its own: the passkeys of the running server stay. So
// does its login when the backup's password was set under another app key,
// which this instance could never check.
func prepare(pending, ready, dataDir string, key *appkey.Key, manifest Manifest) (install, remove []string, err error) {
	if err := os.RemoveAll(ready); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(ready, 0o700); err != nil {
		return nil, nil, err
	}
	for _, name := range append([]string{dbEntry, settingsEntry}, manifest.Files...) {
		err := copyTo(filepath.Join(pending, name), filepath.Join(ready, name), 0o600)
		switch {
		case err == nil:
			install = append(install, name)
		case errors.Is(err, fs.ErrNotExist) && name != dbEntry && name != settingsEntry:
			remove = append(remove, name)
		default:
			return nil, nil, err
		}
	}

	if err := prepareDatabase(filepath.Join(ready, dbEntry), filepath.Join(dataDir, dbEntry), key); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", dbEntry, err)
	}
	if _, err := settings.LoadWithKey(ready, key); err != nil {
		return nil, nil, err
	}
	if state, err := os.ReadFile(filepath.Join(ready, auth.FileName)); err == nil {
		needsKey, err := auth.HashNeedsItsKey(state)
		if err != nil {
			return nil, nil, err
		}
		if needsKey && manifest.KeyID != key.ID() {
			log.Print("restore: the login password in the backup was set under another app key and cannot be checked here, so this instance keeps its own login")
			install = slices.DeleteFunc(install, func(n string) bool { return n == auth.FileName })
		} else if _, err := auth.OpenWithKey(ready, key); err != nil {
			return nil, nil, err
		}
	}
	if _, err := os.Stat(filepath.Join(ready, accounts.KeyringName)); err == nil {
		if _, err := accounts.OpenWithKey(ready, key); err != nil {
			return nil, nil, err
		}
	}
	return install, remove, nil
}

func prepareDatabase(ready, live string, key *appkey.Key) error {
	if err := stripDatabase(ready); err != nil {
		return err
	}
	s, err := store.OpenWithKey(ready, key)
	if err != nil {
		return err
	}
	if err := s.ReplacePasskeysFrom(live); err != nil {
		s.Close()
		return err
	}
	return s.Close()
}

// renameFile is os.Rename, replaced by a test that makes one fail.
var renameFile = os.Rename

// swapJournal lists, in previousDirName, the restored files that had no live
// copy before, so they can be taken out again when a swap is undone. It is
// written once every live file is aside and before any new one goes in.
const swapJournal = "swap.json"

// swap moves every live file named in install or remove into previousDirName,
// the journals of the database with them, and then the prepared files from
// ready into their place. A failure puts every live file back.
func swap(dataDir, ready string, install, remove []string) error {
	prev := filepath.Join(dataDir, previousDirName)
	if err := os.RemoveAll(prev); err != nil {
		return err
	}
	if err := os.MkdirAll(prev, 0o700); err != nil {
		return err
	}
	aside := append(slices.Clone(install), remove...)
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		aside = append(aside, dbEntry+suffix)
	}
	var fresh []string
	for _, name := range aside {
		err := renameFile(filepath.Join(dataDir, name), filepath.Join(prev, name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if slices.Contains(install, name) {
				fresh = append(fresh, name)
			}
		case err != nil:
			return errors.Join(err, undoSwap(dataDir))
		}
	}
	journal, err := json.Marshal(fresh)
	if err != nil {
		return errors.Join(err, undoSwap(dataDir))
	}
	if err := os.WriteFile(filepath.Join(prev, swapJournal), journal, 0o600); err != nil {
		return errors.Join(err, undoSwap(dataDir))
	}
	for _, name := range install {
		if err := renameFile(filepath.Join(ready, name), filepath.Join(dataDir, name)); err != nil {
			return errors.Join(err, undoSwap(dataDir))
		}
	}
	return os.RemoveAll(prev)
}

// undoSwap puts back what a failed or interrupted swap moved aside, and takes
// out the restored files that had no live copy before.
func undoSwap(dataDir string) error {
	prev := filepath.Join(dataDir, previousDirName)
	entries, err := os.ReadDir(prev)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if journal, err := os.ReadFile(filepath.Join(prev, swapJournal)); err == nil {
		var fresh []string
		if err := json.Unmarshal(journal, &fresh); err != nil {
			return err
		}
		for _, name := range fresh {
			if err := os.Remove(filepath.Join(dataDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	for _, e := range entries {
		if e.Name() == swapJournal {
			continue
		}
		if err := renameFile(filepath.Join(prev, e.Name()), filepath.Join(dataDir, e.Name())); err != nil {
			return err
		}
	}
	return os.RemoveAll(prev)
}

func copyTo(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}
