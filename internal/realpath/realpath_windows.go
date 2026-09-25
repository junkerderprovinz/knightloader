//go:build windows

package realpath

import (
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Resolve is the path p really names, with every symlink, junction and mounted
// folder on the way resolved. filepath.EvalSymlinks has left junctions alone
// since Go 1.23, and any user can make a junction without a privilege, so this
// asks Windows for the final path of an open handle instead.
func Resolve(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	// No access rights are needed to ask for the name, and the backup flag is
	// what lets CreateFile open a directory.
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
	}
	defer windows.CloseHandle(h)

	// Zero asks for the normalised name under its drive letter.
	const dosName = 0
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), dosName)
		if err != nil {
			return "", &fs.PathError{Op: "resolve", Path: p, Err: err}
		}
		if int(n) < len(buf) {
			return filepath.Clean(trimLongPathPrefix(windows.UTF16ToString(buf[:n]))), nil
		}
		// Too small; n is the size the name needs.
		buf = make([]uint16, n+1)
	}
}

// trimLongPathPrefix turns the \\?\ form GetFinalPathNameByHandle answers with
// back into the spelling a boundary is written in.
func trimLongPathPrefix(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	return strings.TrimPrefix(p, `\\?\`)
}
