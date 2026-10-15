// Package privdrop lets the server start as root, give the folders Docker has
// just created for it to the account it is going to run as, and become that
// account before anything else happens.
//
// Docker creates a missing bind-mount source as root:root 0755, so an image
// that starts as an unprivileged user cannot write its own fresh data
// directory. The account comes from PUID and PGID, 1000:1000 (the image's
// knight) when they are unset.
//
// Only a folder owned by root with nothing in it is handed over, and only that
// folder, never its contents. A folder with something in it is somebody's
// existing data or share, and chowning it would rewrite its ownership; the
// boot line from fileowner.Advise explains that case instead. A process
// started as anyone but root (--user 99:100 on Unraid, runAsUser, rootless
// Docker) is left as it is, and PUID and PGID are not read.
package privdrop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultUID and DefaultGID are the knight account the Dockerfile creates.
const (
	DefaultUID = 1000
	DefaultGID = 1000
)

// Result is what Drop did.
type Result struct {
	// Applied is true when the process started as root and now runs as UID
	// and GID.
	Applied  bool
	UID, GID int
	// Handed lists the folders given to UID:GID.
	Handed []string
	// Failed holds the hand-overs that did not take, such as a chown refused
	// by an NFS export that squashes root.
	Failed []error
}

// applied is written once by Drop, before main starts a goroutine, and only
// read afterwards.
var applied bool

// Applied reports whether this process started as root and switched to PUID
// and PGID.
func Applied() bool { return applied }

// folder is one candidate as a single look saw it.
type folder struct {
	uid int
	// empty is true when the folder holds nothing but the mount points of
	// other volumes.
	empty bool
}

// plan is what Drop does, worked out without touching anything, so the rules
// are tested without root.
type plan struct {
	drop     bool
	uid, gid int
	handOver []string
}

// makePlan decides from the effective uid, the environment and a look at each
// candidate. look answers false for a folder that is missing, unreadable or
// not a directory, a symlink included.
func makePlan(euid int, getenv func(string) string, dirs []string, look func(string) (folder, bool)) (plan, error) {
	if euid != 0 {
		return plan{}, nil
	}
	uid, err := id(getenv, "PUID", DefaultUID)
	if err != nil {
		return plan{}, err
	}
	gid, err := id(getenv, "PGID", DefaultGID)
	if err != nil {
		return plan{}, err
	}
	p := plan{drop: true, uid: uid, gid: gid}
	seen := map[string]bool{}
	for _, d := range dirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		d = filepath.Clean(d)
		if seen[d] {
			continue
		}
		seen[d] = true
		if f, ok := look(d); ok && f.uid == 0 && f.empty {
			p.handOver = append(p.handOver, d)
		}
	}
	return p, nil
}

// id reads PUID or PGID. Zero is refused: it would keep the server running as
// root, which is what the switch exists to end. A value that does not parse
// stops the start too, since falling back to 1000 would hand the folders to an
// account nobody asked for.
func id(getenv func(string) string, name string, def int) (int, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s=%q is not a numeric id", name, v)
	}
	if n == 0 {
		return 0, fmt.Errorf("%s=0 would keep KnightLoader running as root; leave it unset for %d or name another account", name, def)
	}
	return n, nil
}

// emptyBesides reports whether dir holds nothing but mount points. For
// `-v appdata:/data -v downloads:/data/downloads` Docker creates the mount
// point for the second volume inside the first, so a brand-new data directory
// is never quite empty.
func emptyBesides(dir string, mounts map[string]bool) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !mounts[filepath.Join(dir, e.Name())] {
			return false, nil
		}
	}
	return true, nil
}

// mountEscapes undoes the octal escapes the kernel writes for a space, tab,
// newline and backslash in a mount point.
var mountEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// mountPoints reads the mount points out of /proc/self/mountinfo, where the
// fifth field of each line is one.
func mountPoints(mountinfo string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(mountinfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		out[filepath.Clean(mountEscapes.Replace(fields[4]))] = true
	}
	return out
}
