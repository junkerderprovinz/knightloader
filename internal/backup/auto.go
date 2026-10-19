package backup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// AutoDirName is the folder in the data directory that automatic backups are
// written to.
const AutoDirName = "backups"

// An automatic backup is named after the moment it was made, in UTC, so the
// names sort by time and a backup made by hand, which the browser saves as
// knightloader-backup-..., is never mistaken for one.
const (
	autoPrefix = "knightloader-auto-"
	autoStamp  = "20060102-150405"
	autoExt    = ".zip"
	// tempSuffix ends every file a run writes before it is complete. The dot
	// in front of the name keeps those files out of a file browser's view.
	tempSuffix = ".tmp"
)

// AutoFile is one automatic backup on disk.
type AutoFile struct {
	Name      string    `json:"name"`
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"createdAt"`
}

// AutoName is the file name of an automatic backup made at t, sealed with the
// app key or not.
func AutoName(t time.Time, sealed bool) string {
	name := autoPrefix + t.UTC().Format(autoStamp) + autoExt
	if sealed {
		name += SealedExt
	}
	return name
}

// ParseAutoName reports when the automatic backup called name was made. It
// accepts only names AutoName produces, which is what keeps every other file
// in the folder safe from PruneAuto and a download route from reading
// anything else.
func ParseAutoName(name string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, autoPrefix)
	if !ok {
		return time.Time{}, false
	}
	rest = strings.TrimSuffix(rest, SealedExt)
	stamp, ok := strings.CutSuffix(rest, autoExt)
	if !ok || len(stamp) != len(autoStamp) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(autoStamp, stamp, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ListAuto returns the automatic backups in dir, newest first. A folder that
// does not exist yet holds none.
func ListAuto(dir string) ([]AutoFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: could not read %s: %w", dir, err)
	}
	var files []AutoFile
	for _, e := range entries {
		created, ok := ParseAutoName(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// Removed since the listing was read.
			continue
		}
		files = append(files, AutoFile{Name: e.Name(), Bytes: info.Size(), CreatedAt: created})
	}
	slices.SortFunc(files, func(a, b AutoFile) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return files, nil
}

// NextAuto is when the next automatic backup is due: intervalDays after the
// newest of files, as ListAuto returns them. It is the zero time when there
// is none, which is due at once.
//
// The schedule is read off the files rather than kept anywhere, so a process
// that restarts every night neither backs up on every start nor never.
func NextAuto(files []AutoFile, intervalDays int) time.Time {
	if len(files) == 0 {
		return time.Time{}
	}
	return files[0].CreatedAt.AddDate(0, 0, intervalDays)
}

// WriteAuto writes the automatic backup made at t into dir. write gets the
// file to write the archive to and scratch, an unused path in dir for the
// database snapshot.
//
// The archive is renamed to its real name only once it is complete and
// synced, so a failed or killed run never leaves a file that looks finished,
// and whatever such a run left is removed first. It carries every secret in
// the settings, so like settings.json only this account can read it.
func WriteAuto(dir string, t time.Time, sealed bool, write func(w io.Writer, scratch string) error) (AutoFile, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return AutoFile{}, fmt.Errorf("backup: could not create %s: %w", dir, err)
	}
	if err := removeTemps(dir); err != nil {
		return AutoFile{}, err
	}
	name := AutoName(t, sealed)
	final := filepath.Join(dir, name)
	tmp := filepath.Join(dir, "."+name+tempSuffix)
	scratch := filepath.Join(dir, "."+autoPrefix+t.UTC().Format(autoStamp)+".db"+tempSuffix)
	defer os.Remove(scratch)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return AutoFile{}, fmt.Errorf("backup: could not create %s: %w", tmp, err)
	}
	err = write(f, scratch)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, final)
	}
	if err != nil {
		os.Remove(tmp)
		return AutoFile{}, err
	}
	info, err := os.Stat(final)
	if err != nil {
		return AutoFile{}, fmt.Errorf("backup: wrote %s but could not read it back: %w", final, err)
	}
	created, _ := ParseAutoName(name)
	return AutoFile{Name: name, Bytes: info.Size(), CreatedAt: created}, nil
}

// removeTemps deletes the files an interrupted WriteAuto left in dir.
func removeTemps(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: could not read %s: %w", dir, err)
	}
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(n, "."+autoPrefix) || !strings.HasSuffix(n, tempSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backup: could not remove the unfinished %s: %w", n, err)
		}
	}
	return nil
}

// PruneAuto deletes the oldest automatic backups in dir until keep are left,
// and returns the names it removed, newest first. A file whose name AutoName
// would not produce is never touched, a backup made by hand included.
func PruneAuto(dir string, keep int) ([]string, error) {
	files, err := ListAuto(dir)
	if err != nil || len(files) <= keep {
		return nil, err
	}
	var removed []string
	for _, f := range files[keep:] {
		if err := os.Remove(filepath.Join(dir, f.Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("backup: could not remove %s: %w", f.Name, err)
		}
		removed = append(removed, f.Name)
	}
	return removed, nil
}
