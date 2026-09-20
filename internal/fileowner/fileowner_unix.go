//go:build unix

package fileowner

// The tag covers every unix, unlike internal/diskspace's, because Stat_t.Uid
// and Stat_t.Gid have the same names and types everywhere and os/user is
// portable.

import (
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// supported lets the test tell a platform that cannot answer from an
// implementation that stopped working.
const supported = true

// who reads the effective identity, which is what the kernel stamps on new
// files.
func who() Identity {
	id := Identity{Known: true, UID: os.Geteuid(), GID: os.Getegid()}
	id.User, id.Group = names(id.UID, id.GID)
	if m, ok := readUmask(); ok {
		id.Umask, id.UmaskKnown = m, true
	}
	return id
}

// statOwner reads uid and gid from the platform's Stat_t. The assertion is
// checked because a FileInfo can also come from an fs.FS or a test double.
func statOwner(fi fs.FileInfo) (int, int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// names resolves a uid and gid to names, or "" each. A failed lookup is
// normal: a container run with --user 99:100 has no passwd entry, and the
// number alone is what goes into a chown.
func names(uid, gid int) (string, string) {
	var u, g string
	if e, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		u = e.Username
	}
	if e, err := user.LookupGroupId(strconv.Itoa(gid)); err == nil {
		g = e.Name
	}
	return u, g
}

// readUmask reads the process umask from /proc/self/status (Linux 4.7+) and
// reports false elsewhere. umask(0) followed by umask(old) would briefly zero
// the mask while other goroutines create files.
func readUmask() (int, bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "Umask:")
		if !ok {
			continue
		}
		// The value is octal.
		n, err := strconv.ParseInt(strings.TrimSpace(rest), 8, 32)
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}
