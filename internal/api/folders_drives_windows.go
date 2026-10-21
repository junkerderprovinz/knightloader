//go:build windows

package api

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// volumeRoots is every drive letter with a readable filesystem behind it, so
// the chooser can move from C: to another disk. The volume p is on is always
// among them, also when it is a share without a letter.
func volumeRoots(p string) []string {
	own := volumeRoot(p)
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return []string{own}
	}
	var out []string
	seen := false
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if strings.EqualFold(root, own) {
			out = append(out, own)
			seen = true
			continue
		}
		// A card reader or a disc drive has a letter even when it is empty.
		if _, err := os.Stat(root); err != nil {
			continue
		}
		out = append(out, root)
	}
	if !seen {
		out = append([]string{own}, out...)
	}
	return out
}
