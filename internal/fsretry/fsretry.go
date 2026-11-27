// Package fsretry renames and replaces files, retrying while Windows refuses.
package fsretry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Rename is os.Rename, retried for up to three seconds while Windows refuses
// it. A virus scanner or the search indexer opens a file it has just seen
// written and holds it briefly, and until it lets go the file cannot be
// renamed or replaced. Elsewhere a refusal is final.
func Rename(from, to string) error {
	err := os.Rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for range 30 {
		if !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		err = os.Rename(from, to)
	}
	return err
}

// Replace puts data at path through a synced temporary file in the same
// folder, so a crash leaves the old file or the new one and never a cut-off
// one. Only the owner may read the file.
func Replace(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	// The rename lives in the folder. Windows cannot sync a folder, and there
	// the rename is durable without it.
	if runtime.GOOS != "windows" {
		if d, err := os.Open(filepath.Dir(path)); err == nil {
			_ = d.Sync()
			d.Close()
		}
	}
	return nil
}
