package appkey

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CopiesPrefix starts the name of the folder KeepCopies makes.
const CopiesPrefix = "before-app-key-"

// CopiesKept is how long the copies stay once nothing has failed to seal or
// open. A key that works for a week of ordinary use is the one the owner
// meant to set.
const CopiesKept = 7 * 24 * time.Hour

// copiesStateName is the file in the copies folder that says how long it
// stays. It survives restarts because it lives next to what it describes.
const copiesStateName = "state.json"

type copiesState struct {
	Made time.Time `json:"made"`
	// CleanSince is when the last failure was noted, or Made when there was
	// none. The copies go CopiesKept after it.
	CleanSince time.Time `json:"cleanSince"`
}

// copiesMu orders the read-modify-write of the state files, which failures on
// several goroutines can reach at once.
var copiesMu sync.Mutex

// KeepCopies copies those of names that exist in dataDir into a new folder
// beside them and returns the folder, or "" when none of them exists. It runs
// before the first start with a key rewrites the files sealed, so a sealing
// that goes wrong can be undone by hand. SweepCopies removes the folder once
// the key has worked for CopiesKept.
func KeepCopies(dataDir string, names []string, now time.Time) (string, error) {
	dir := filepath.Join(dataDir, CopiesPrefix+now.Format("20060102-150405"))
	made := false
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dataDir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
		}
		if !made {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
			}
			made = true
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			return "", fmt.Errorf("keeping a copy of %s: %w", name, err)
		}
	}
	if !made {
		return "", nil
	}
	if err := writeCopiesState(dir, copiesState{Made: now, CleanSince: now}); err != nil {
		return "", fmt.Errorf("keeping the copies: %w", err)
	}
	return dir, nil
}

// NoteFailure starts the wait over for every copies folder in dataDir, so the
// copies outlive a key that does not open everything it should.
func NoteFailure(dataDir string, now time.Time) error {
	copiesMu.Lock()
	defer copiesMu.Unlock()
	dirs, err := copiesDirs(dataDir)
	for _, dir := range dirs {
		st := readCopiesState(dir, now)
		st.CleanSince = now
		err = errors.Join(err, writeCopiesState(dir, st))
	}
	return err
}

// CopiesUntil reports when the copies in dataDir go, the latest date when
// there is more than one folder, and false when there are none.
func CopiesUntil(dataDir string, now time.Time) (time.Time, bool) {
	copiesMu.Lock()
	defer copiesMu.Unlock()
	dirs, _ := copiesDirs(dataDir)
	var until time.Time
	for _, dir := range dirs {
		if u := readCopiesState(dir, now).CleanSince.Add(CopiesKept); u.After(until) {
			until = u
		}
	}
	return until, len(dirs) > 0
}

// SweepCopies removes each copies folder whose wait is over and returns the
// ones it removed.
func SweepCopies(dataDir string, now time.Time) ([]string, error) {
	copiesMu.Lock()
	defer copiesMu.Unlock()
	dirs, err := copiesDirs(dataDir)
	var removed []string
	for _, dir := range dirs {
		if now.Before(readCopiesState(dir, now).CleanSince.Add(CopiesKept)) {
			continue
		}
		if rerr := os.RemoveAll(dir); rerr != nil {
			err = errors.Join(err, rerr)
			continue
		}
		removed = append(removed, dir)
	}
	return removed, err
}

func copiesDirs(dataDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir, CopiesPrefix+"*"))
	var dirs []string
	for _, m := range matches {
		if fi, serr := os.Stat(m); serr == nil && fi.IsDir() {
			dirs = append(dirs, m)
		}
	}
	return dirs, err
}

// readCopiesState reads a folder's state. A folder without a readable one
// counts from now, which keeps it longer rather than removing it early.
func readCopiesState(dir string, now time.Time) copiesState {
	var st copiesState
	b, err := os.ReadFile(filepath.Join(dir, copiesStateName))
	if err != nil || json.Unmarshal(b, &st) != nil || st.CleanSince.IsZero() {
		st = copiesState{Made: now, CleanSince: now}
		_ = writeCopiesState(dir, st)
	}
	return st
}

func writeCopiesState(dir string, st copiesState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, copiesStateName), b, 0o600)
}
