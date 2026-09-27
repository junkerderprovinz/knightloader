// Package filemode holds the modes downloaded output is created with and the
// process umask that shapes them.
//
// Every file and folder a download produces is requested as 0666 or 0777 and
// the umask takes bits away, so the umask alone decides what lands on the
// share. On Unraid the convention is UMASK=000, which leaves downloads
// writable for the SMB account that has to move and delete them. The app's own
// secrets are written with explicit private modes that no umask can widen.
package filemode

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
)

// File and Dir are the modes every download, extracted file and folder made
// for one is requested with.
const (
	File fs.FileMode = 0o666
	Dir  fs.FileMode = 0o777
)

var (
	mu      sync.Mutex
	mask    fs.FileMode
	known   bool
	applied bool
)

// Parse reads a UMASK value the way linuxserver.io images do: octal digits,
// with or without leading zeros, so "0", "000", "002" and "0022" all work.
func Parse(s string) (fs.FileMode, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil || n > 0o777 {
		return 0, fmt.Errorf("%q is not an octal mask such as 000 or 022", s)
	}
	return fs.FileMode(n), nil
}

// Apply makes value, the UMASK setting, this process's umask. Children such as
// yt-dlp, ffmpeg and JDownloader inherit it. An empty value keeps the mask the
// process started with; one Parse refuses changes nothing and is returned.
//
// It belongs at the top of main: the mask is process-wide, and a file created
// by another goroutine while it changes would get one mask or the other.
func Apply(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	m, err := Parse(value)
	if err != nil {
		return err
	}
	if !supported {
		return errors.New("this system has no umask")
	}
	mu.Lock()
	defer mu.Unlock()
	setUmask(m)
	mask, known, applied = m, true, true
	return nil
}

// Applied reports whether Apply took a UMASK value as the process umask.
func Applied() bool {
	mu.Lock()
	defer mu.Unlock()
	return applied
}

// Mask is the umask in force, read once and then remembered, since nothing
// but Apply changes it.
func Mask() fs.FileMode {
	mu.Lock()
	defer mu.Unlock()
	if !known {
		mask, known = currentUmask(), true
	}
	return mask
}

// Settle gives a file or folder the mode this process would have created it
// with, File or Dir under the umask in force. It is for output a library
// writes with fixed modes of its own. Anything else, such as a link, is left
// alone.
func Settle(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case fi.IsDir():
		return os.Chmod(path, Dir&^Mask())
	case fi.Mode().IsRegular():
		return os.Chmod(path, File&^Mask())
	}
	return nil
}
