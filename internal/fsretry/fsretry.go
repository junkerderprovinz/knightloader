// Package fsretry renames files the way Windows lets them be renamed.
package fsretry

import (
	"errors"
	"io/fs"
	"os"
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
