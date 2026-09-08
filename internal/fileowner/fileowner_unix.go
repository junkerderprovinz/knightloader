//go:build unix

package fileowner

// The half that needs a kernel.
//
// THE BUILD TAG IS WIDER THAN internal/diskspace's ON PURPOSE, and the
// difference is worth stating because the two packages sit next to each other
// and look like they should agree. diskspace enumerates linux, darwin and
// freebsd because Statfs_t is a different shape on almost every kernel - NetBSD
// exposes statvfs, OpenBSD spells the fields differently - so a wider tag there
// is a build that stops compiling on a platform nobody can test. Nothing of the
// sort applies here: Stat_t.Uid and Stat_t.Gid are spelled Uid and Gid, as
// uint32, on every unix Go builds for, and os/user is portable by definition.
// So this file covers all of them and the stub next door covers only the
// platforms that genuinely have no such concept.

import (
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// supported says this build can read owners. Read only by the package's own
// test, which otherwise could not tell a platform that genuinely cannot answer
// from an implementation that has stopped working.
const supported = true

// who reads the identity from the process itself.
//
// EFFECTIVE AND NOT REAL. Geteuid is what the kernel stamps on a file at
// creation; Getuid is who started the process. They differ only under setuid,
// which this app never uses, so the choice costs nothing today and is the
// correct one the day something wraps the binary.
func who() Identity {
	id := Identity{Known: true, UID: os.Geteuid(), GID: os.Getegid()}
	id.User, id.Group = names(id.UID, id.GID)
	if m, ok := readUmask(); ok {
		id.Umask, id.UmaskKnown = m, true
	}
	return id
}

// statOwner pulls the two numbers out of whatever the platform's Stat_t is.
//
// The type assertion is checked rather than assumed. Sys() is documented as
// returning "the underlying data source", which for os.Stat is *syscall.Stat_t
// on every unix - but a FileInfo can also come from an fs.FS, a zip reader or a
// test double, and an unchecked assertion turns any of those into a panic
// inside a readout. Answering "cannot tell" is the same fail-open behaviour the
// rest of this package has.
func statOwner(fi fs.FileInfo) (int, int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// names resolves a uid and a gid to their names, or to "" each.
//
// A FAILED LOOKUP IS NORMAL AND MUST NEVER BE FATAL. A container started with
// --user 99:100 runs as a uid that nothing in the image's /etc/passwd mentions,
// which is the single most common configuration this feature reports on. The
// number alone is a complete answer: it is what goes in the chown command and
// what goes in the run command. The name is a convenience.
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

// readUmask reports the process umask, and whether it could be read at all.
//
// THE OBVIOUS IMPLEMENTATION IS A RACE AND IS NOT USED. umask(2) has no
// read-only form: the portable trick is umask(0) followed by umask(old), which
// leaves a window in which the mask is 0 - and this process has dozens of
// goroutines, several of which create files for a living. A download that
// happened to land in that window would be written world-writable, once,
// unreproducibly, because somebody opened a settings page.
//
// So it is read from /proc/self/status, which is a plain read of one file and
// cannot affect anything. That line arrived in Linux 4.7 and exists on no other
// kernel, hence the second return value: darwin and the BSDs simply answer
// false here, and the folder probe's MEASURED mode is the authoritative number
// in any case. Reporting "not known" is the same third answer diskspace gives a
// platform it has no call for.
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
		// Octal, always, and parsed as such: "0022" read as decimal is 22, which
		// is 0o026 - a plausible-looking mask that is simply the wrong one.
		n, err := strconv.ParseInt(strings.TrimSpace(rest), 8, 32)
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}
