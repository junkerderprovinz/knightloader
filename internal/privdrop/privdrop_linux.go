//go:build linux

package privdrop

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// Drop hands the empty, root-owned folders among dirs to PUID:PGID and
// switches the process to that account. It does nothing unless the process
// runs as root. An error means the start has to stop: the ids did not parse,
// or the process could still be root.
//
// Go applies setgroups, setgid and setuid to every thread of the process. The
// groups go first because only root may change them, and root's supplementary
// groups would otherwise survive the switch.
func Drop(dirs []string) (Result, error) {
	p, err := makePlan(os.Geteuid(), os.Getenv, dirs, look(readMounts()))
	if err != nil || !p.drop {
		return Result{}, err
	}
	res := Result{Applied: true, UID: p.uid, GID: p.gid}
	for _, d := range p.handOver {
		if err := os.Lchown(d, p.uid, p.gid); err != nil {
			res.Failed = append(res.Failed, err)
			continue
		}
		res.Handed = append(res.Handed, d)
	}

	if err := syscall.Setgroups([]int{}); err != nil {
		return Result{}, fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setgid(p.gid); err != nil {
		return Result{}, fmt.Errorf("setgid %d: %w", p.gid, err)
	}
	if err := syscall.Setuid(p.uid); err != nil {
		return Result{}, fmt.Errorf("setuid %d: %w", p.uid, err)
	}
	if os.Getuid() != p.uid || os.Geteuid() != p.uid || os.Getgid() != p.gid || os.Getegid() != p.gid {
		return Result{}, fmt.Errorf("asked for %d:%d and still running as uid %d (effective %d), gid %d (effective %d)",
			p.uid, p.gid, os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid())
	}

	// Docker sets HOME for the user it starts, so it still names /root, where
	// the JVM and yt-dlp would try to write. "/" is what Docker gives an id
	// with no passwd entry.
	home := "/"
	if u, err := user.LookupId(strconv.Itoa(p.uid)); err == nil && u.HomeDir != "" {
		home = u.HomeDir
	}
	if err := os.Setenv("HOME", home); err != nil {
		return Result{}, err
	}
	applied = true
	return res, nil
}

// look stats a candidate without following a symlink, so a link cannot
// redirect the chown to a folder somewhere else.
func look(mounts map[string]bool) func(string) (folder, bool) {
	return func(dir string) (folder, bool) {
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() {
			return folder{}, false
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return folder{}, false
		}
		empty, err := emptyBesides(dir, mounts)
		if err != nil {
			return folder{}, false
		}
		return folder{uid: int(st.Uid), empty: empty}, true
	}
}

// readMounts is empty when /proc is not mounted; a data directory holding a
// mount point then counts as in use and keeps its owner.
func readMounts() map[string]bool {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return map[string]bool{}
	}
	return mountPoints(string(b))
}
