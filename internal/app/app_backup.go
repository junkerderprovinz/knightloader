package app

// Backups: what goes into the archive, shared by the download route and the
// schedule, and the schedule itself. An automatic backup is the same archive
// Download backup sends, sealed the same way, written into the backups folder
// of the data directory. The schedule keeps no record of its own: when the
// next one is due is read off the newest file there, so a restart changes
// nothing about it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// BackupParts are what a backup archive is built from.
type BackupParts struct {
	Manifest backup.Manifest
	// Settings is settings.json with every secret in the clear, because a
	// backup that cannot put a router or proxy password back is not a backup.
	Settings []byte
	// DBPath is a consistent snapshot of the store, never the live file, with
	// the task passwords in the clear.
	DBPath string
	// Files are the instance's state files (backup.StateFiles) it has, the
	// login and the credential key in the clear.
	Files map[string][]byte
}

// PrepareBackup snapshots the store to dbPath, which must not exist yet, and
// collects the rest of an archive. Everything the app key seals is put in the
// clear, so the archive restores under another key or none; the caller seals
// the archive as a whole. The caller removes dbPath afterwards.
func (a *App) PrepareBackup(dbPath string) (BackupParts, error) {
	// VACUUM INTO through the connection every other write goes through, never
	// a raw copy of the live file, which hands back a torn database if a task
	// settles mid-copy.
	if err := a.Store.BackupTo(dbPath); err != nil {
		return BackupParts{}, fmt.Errorf("could not snapshot the database: %w", err)
	}
	if err := store.OpenPasswordsIn(dbPath, a.AppKey); err != nil {
		return BackupParts{}, fmt.Errorf("could not open the task passwords in the snapshot: %w", err)
	}
	settingsJSON, err := json.MarshalIndent(a.Settings.Get(), "", "  ")
	if err != nil {
		return BackupParts{}, fmt.Errorf("could not encode settings: %w", err)
	}
	files, err := a.stateFiles()
	if err != nil {
		return BackupParts{}, err
	}
	return BackupParts{
		Manifest: backup.Manifest{
			Version:    buildinfo.Version,
			Deployment: buildinfo.Deployment,
			CreatedAt:  time.Now().UTC(),
			Files:      backup.StateFiles,
			KeyID:      a.AppKey.ID(),
		},
		Settings: settingsJSON,
		DBPath:   dbPath,
		Files:    files,
	}, nil
}

// stateFiles reads the state files this instance has. The login and the
// credential key come from the stores holding them open, in the clear; the
// others are read off the disk as they are, since none is sealed.
func (a *App) stateFiles() (map[string][]byte, error) {
	files := map[string][]byte{}
	state, err := a.Auth.State()
	if err != nil {
		return nil, fmt.Errorf("could not read the login: %w", err)
	}
	files[auth.FileName] = state
	files[accounts.KeyringName] = a.Accounts.Keyring()
	for _, name := range backup.StateFiles {
		if _, ok := files[name]; ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.DataDir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("could not read %s: %w", name, err)
		}
		files[name] = b
	}
	return files, nil
}

// WriteBackup writes the archive for parts to w, sealed with the app key when
// the install has one. Sealing needs the whole archive, so a sealed one is
// built in memory and written in one piece.
func (a *App) WriteBackup(w io.Writer, parts BackupParts) error {
	if a.AppKey == nil {
		return backup.Build(w, parts.Manifest, parts.Settings, parts.DBPath, parts.Files)
	}
	var buf bytes.Buffer
	if err := backup.Build(&buf, parts.Manifest, parts.Settings, parts.DBPath, parts.Files); err != nil {
		return err
	}
	sealed, err := backup.Seal(a.AppKey, buf.Bytes())
	if err != nil {
		return fmt.Errorf("could not encrypt the backup: %w", err)
	}
	_, err = w.Write(sealed)
	return err
}

// autoBackupRetry is how long the schedule waits after a failed run. Trying
// on every tick would write a log line a minute about the same broken disk.
const autoBackupRetry = time.Hour

// AutoBackupSkippedLowSpace is why a run stood down: the data folder was
// under settings.DiskLowSpace. It is a code so the interface can translate
// it.
const AutoBackupSkippedLowSpace = "low-space"

// AutoBackupRun is the last scheduled run, whether it wrote a file, failed or
// stood down.
type AutoBackupRun struct {
	At    time.Time `json:"at"`
	File  string    `json:"file,omitempty"`
	Error string    `json:"error,omitempty"`
	// Skipped is AutoBackupSkippedLowSpace or empty, with the figures that
	// decided it.
	Skipped    string `json:"skipped,omitempty"`
	FreeBytes  int64  `json:"freeBytes,omitempty"`
	FloorBytes int64  `json:"floorBytes,omitempty"`
}

// AutoBackupState is the answer to GET /api/system/backups. Dir is a path on
// this machine, so the route stays off the group's forwarding list.
type AutoBackupState struct {
	Dir string `json:"dir"`
	// DefaultDir is where they go while settings.BackupDir is empty.
	DefaultDir string `json:"defaultDir"`
	// Files is newest first and never nil.
	Files   []backup.AutoFile `json:"files"`
	Running bool              `json:"running"`
	// NextRunAt is nil while the schedule is off.
	NextRunAt *time.Time     `json:"nextRunAt"`
	Last      *AutoBackupRun `json:"last"`
}

// autoBackupRunner is the schedule's state in this process. Losing it in a
// restart costs only the record of a failure, and the next run makes a new
// one.
type autoBackupRunner struct {
	mu      sync.Mutex
	running bool
	last    *AutoBackupRun
	retryAt time.Time
}

// AutoBackupDir is the folder automatic backups are written to:
// settings.BackupDir, or the backups folder in the data directory.
func (a *App) AutoBackupDir() string {
	if dir := a.Settings.Get().BackupDir; dir != "" {
		return dir
	}
	return filepath.Join(a.DataDir, backup.AutoDirName)
}

// AutoBackupState lists the automatic backups and says when the next one is
// due and how the last run went.
func (a *App) AutoBackupState() (AutoBackupState, error) {
	cfg := a.Settings.Get()
	dir := a.AutoBackupDir()
	files, err := backup.ListAuto(dir)
	if err != nil {
		return AutoBackupState{}, err
	}
	out := AutoBackupState{Dir: dir, DefaultDir: filepath.Join(a.DataDir, backup.AutoDirName), Files: files}
	if out.Files == nil {
		out.Files = []backup.AutoFile{}
	}
	r := &a.autoBackup
	r.mu.Lock()
	defer r.mu.Unlock()
	out.Running = r.running
	if r.last != nil {
		last := *r.last
		out.Last = &last
	}
	if cfg.BackupAuto {
		next := backup.NextAuto(files, cfg.BackupIntervalDays)
		if r.retryAt.After(next) {
			next = r.retryAt
		}
		if now := time.Now(); next.Before(now) {
			next = now
		}
		out.NextRunAt = &next
	}
	return out, nil
}

// runAutoBackupIfDue is the schedule, called once a minute from sweep(). The
// run itself goes on its own goroutine, because Close waits for upkeep.
func (a *App) runAutoBackupIfDue() {
	cfg := a.Settings.Get()
	if !cfg.BackupAuto {
		return
	}
	now := time.Now()
	r := &a.autoBackup
	r.mu.Lock()
	waiting := r.running || now.Before(r.retryAt)
	r.mu.Unlock()
	if waiting {
		return
	}

	dir := a.AutoBackupDir()
	files, err := backup.ListAuto(dir)
	if err != nil {
		a.finishAutoBackup(AutoBackupRun{At: now, Error: err.Error()})
		return
	}
	if now.Before(backup.NextAuto(files, cfg.BackupIntervalDays)) {
		return
	}
	// An unmeasurable disk is not a full one, so the run goes ahead then.
	if free, ok := diskspace.Free(a.DataDir); ok && cfg.DiskLowSpace > 0 && free < uint64(cfg.DiskLowSpace) {
		a.skipAutoBackup(now, int64(free), cfg.DiskLowSpace)
		return
	}

	r.mu.Lock()
	r.running = true
	r.mu.Unlock()
	a.spawn(func() { a.finishAutoBackup(a.makeAutoBackup(dir, cfg.BackupKeep)) })
}

// makeAutoBackup writes one automatic backup and removes the oldest beyond
// keep. A failed removal is logged and does not fail the run, since the new
// backup is there.
func (a *App) makeAutoBackup(dir string, keep int) AutoBackupRun {
	run := AutoBackupRun{At: time.Now()}
	file, err := backup.WriteAuto(dir, run.At, a.AppKey != nil, func(w io.Writer, scratch string) error {
		parts, err := a.PrepareBackup(scratch)
		if err != nil {
			return err
		}
		return a.WriteBackup(w, parts)
	})
	if err != nil {
		run.Error = err.Error()
		return run
	}
	run.File = file.Name
	removed, err := backup.PruneAuto(dir, keep)
	if err != nil {
		log.Printf("backup: wrote %s, but older automatic backups could not be removed: %v", file.Name, err)
	}
	log.Printf("backup: wrote the automatic backup %s (%d bytes) and removed %d older ones", file.Name, file.Bytes, len(removed))
	return run
}

// finishAutoBackup records a run and releases the claim. A failure holds the
// schedule back for autoBackupRetry.
func (a *App) finishAutoBackup(run AutoBackupRun) {
	r := &a.autoBackup
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
	r.last = &run
	r.retryAt = time.Time{}
	if run.Error != "" {
		r.retryAt = run.At.Add(autoBackupRetry)
		log.Printf("backup: the automatic backup failed and is tried again in an hour: %s", run.Error)
	}
}

// skipAutoBackup records a run that stood down for lack of room. It logs once
// per stretch of low space and is checked again on every tick, since asking
// the disk costs nothing.
func (a *App) skipAutoBackup(at time.Time, free, floor int64) {
	r := &a.autoBackup
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil || r.last.Skipped != AutoBackupSkippedLowSpace {
		log.Printf("backup: the automatic backup waits: %s has %d bytes free, under the floor of %d", a.DataDir, free, floor)
	}
	r.last = &AutoBackupRun{At: at, Skipped: AutoBackupSkippedLowSpace, FreeBytes: free, FloorBytes: floor}
}

// backupSubsystem is the health row of the schedule: failed after a run that
// wrote nothing, degraded while it waits for room.
func (a *App) backupSubsystem(cfg settings.Settings) Subsystem {
	row := Subsystem{ID: SubsystemBackup, State: StateOK}
	if !cfg.BackupAuto {
		row.State = StateUnused
		return row
	}
	r := &a.autoBackup
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.last == nil:
	case r.last.Error != "":
		row.State, row.Detail, row.Remedy = StateFailed, r.last.Error, remedyBackupFailed
	case r.last.Skipped != "":
		row.State, row.Remedy = StateDegraded, remedyBackupLowSpace
	}
	return row
}
