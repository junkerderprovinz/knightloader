// Package fileowner reports who the files this process writes belong to and
// who else can open them, which a write probe cannot tell. On a NAS the write
// succeeds, the file lands owned by this process rather than by the share's
// owner, and a media server next door sees an empty library.
//
// Everything is measured rather than computed. A set-group-id parent, NFS root
// squash and a mount's umask= option all override os.Geteuid and the process
// umask, and all are common on a NAS, so Check creates a real file and
// directory, stats them and removes them.
//
// Known false means the numbers mean nothing (Windows has no unix owners), as
// in internal/diskspace; a readout must say so instead of printing zeros.
//
// Nothing here reads PUID, PGID or UMASK or changes an owner: the image runs
// as an unprivileged user, which cannot switch uid. The package reports the
// ownership so it can be fixed in the run command or with a chown on the host.
package fileowner

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// The verdicts are stable ids, not prose, so the client can show them in its
// own language.
const (
	// VerdictOK is a folder whose probe file came out owned by the folder's
	// owner, group-readable, in a sub-directory the group can enter.
	VerdictOK = "ok"
	// VerdictOwnerMismatch is a successful write whose file belongs to
	// somebody other than the folder's owner.
	VerdictOwnerMismatch = "ownerMismatch"
	// VerdictGroupUnreadable is a probe file without group read, so only its
	// owner can open what lands here.
	VerdictGroupUnreadable = "groupUnreadable"
	// VerdictDirUnreadable is a new sub-folder without group read or execute,
	// so nothing gets inside it whatever its files allow.
	VerdictDirUnreadable = "dirUnreadable"
	// VerdictNotWritable is a folder this process cannot write into, on any
	// platform.
	VerdictNotWritable = "notWritable"
	// VerdictMissing is a folder that is not there; see Check.
	VerdictMissing = "missing"
	// VerdictUnknown is a platform with no file owners to compare.
	VerdictUnknown = "unknown"
)

// The permission bits the probe asks for are the ones internal/collide
// creates every download and package folder with. The measured mode is
// requested &^ umask, so other numbers would measure a mode no download gets
// (with 0644, for instance, a umask of 002 changes nothing).
const (
	probeFilePerm = 0o644
	probeDirPerm  = 0o755
)

// probePrefix starts every probe name: a dot-file, so it stays out of file
// browsers and media scanners, named after the app so a leftover can be
// attributed.
const probePrefix = ".knightloader-owner-"

// Identity is who this process writes files as.
type Identity struct {
	// Known is false where there are no unix owners (Windows, wasm); the
	// other fields are then meaningless.
	Known bool
	// UID and GID are the effective ids, which the kernel stamps on new
	// files.
	UID, GID int
	// User and Group are the names behind the ids, or "" when the passwd or
	// group database has no entry, which is normal for --user 99:100.
	User, Group string
	// Umask is the process umask, if UmaskKnown. It is read from
	// /proc/self/status (Linux 4.7+); the umask(x)/umask(back) trick would
	// race with other goroutines creating files. The folder probe's measured
	// mode is authoritative anyway.
	Umask      int
	UmaskKnown bool
}

// Owner is one path's identity and permission bits from a single stat.
type Owner struct {
	Path   string
	Exists bool
	IsDir  bool
	// Known false means the numbers below are absent, not zero.
	Known       bool
	UID, GID    int
	User, Group string
	// Mode is the low twelve permission bits, including setuid, setgid and
	// sticky, without type bits.
	Mode uint32
	// Detail is the raw OS error when the stat failed, since EACCES, ESTALE
	// and "no such device" are different problems.
	Detail string
}

// Probe is one folder as Check measured it with a real file and sub-directory.
type Probe struct {
	Dir    string
	Exists bool
	Known  bool

	// The folder itself.
	DirUID, DirGID    int
	DirUser, DirGroup string
	DirMode           uint32

	// The probe file, standing in for a finished download.
	FileUID, FileGID    int
	FileUser, FileGroup string
	FileMode            uint32

	// SubdirMode is the probe sub-directory's mode, standing in for a
	// per-package folder. Its owner matches the file's.
	SubdirMode uint32

	// Verdict is one of the constants above; Detail carries the raw OS error
	// where there is one.
	Verdict string
	Detail  string
}

// Who is the identity this process creates files with.
func Who() Identity { return who() }

// Of is one path's owner and mode, from a single stat. It writes nothing, so
// the diagnostics page can call it on every load.
func Of(path string) Owner {
	o := Owner{Path: path}
	fi, err := os.Stat(path)
	if err != nil {
		o.Detail = err.Error()
		return o
	}
	o.Exists = true
	o.IsDir = fi.IsDir()
	o.Mode = permBits(fi.Mode())
	if uid, gid, ok := statOwner(fi); ok {
		o.Known = true
		o.UID, o.GID = uid, gid
		o.User, o.Group = names(uid, gid)
	}
	return o
}

// Check measures one folder by writing into it, so nothing answering a GET
// calls it.
//
// A missing folder is not created: it is either one nothing has written into
// yet or a mount that did not come up, and creating it would hide the second.
// The probes are removed in a defer; a failed removal is not reported, since
// the measurement already succeeded.
func Check(dir string) Probe {
	p := Probe{Dir: dir}
	folder := Of(dir)
	if !folder.Exists || !folder.IsDir {
		p.Verdict = VerdictMissing
		p.Detail = folder.Detail
		if p.Detail == "" && folder.Exists {
			p.Detail = dir + " is not a directory"
		}
		return p
	}
	p.Exists = true
	p.Known = folder.Known
	p.DirUID, p.DirGID = folder.UID, folder.GID
	p.DirUser, p.DirGroup = folder.User, folder.Group
	p.DirMode = folder.Mode

	file, err := createProbeFile(dir)
	if err != nil {
		p.Verdict, p.Detail = VerdictNotWritable, err.Error()
		return p
	}
	defer func() { _ = os.Remove(file) }()

	sub, err := createProbeDir(dir)
	if err != nil {
		// Some FUSE mounts or a full inode table take a file but no directory,
		// and downloads land in per-package sub-folders.
		p.Verdict, p.Detail = VerdictNotWritable, err.Error()
		return p
	}
	defer func() { _ = os.Remove(sub) }()

	f := Of(file)
	p.FileUID, p.FileGID = f.UID, f.GID
	p.FileUser, p.FileGroup = f.User, f.Group
	p.FileMode = f.Mode
	p.SubdirMode = Of(sub).Mode
	p.Known = folder.Known && f.Known

	p.Verdict = judge(p)
	return p
}

// judge picks the ownership verdict from a finished measurement. It has no
// syscalls, so its test runs on every platform. The sub-directory is checked
// first because a folder nobody can enter is the bigger problem; the owner
// comparison comes last.
func judge(p Probe) string {
	switch {
	case !p.Known:
		return VerdictUnknown
	// 0o050 is group read and execute; a download folder needs both.
	case p.SubdirMode&0o050 != 0o050:
		return VerdictDirUnreadable
	case p.FileMode&0o040 == 0:
		return VerdictGroupUnreadable
	case p.FileUID != p.DirUID || p.FileGID != p.DirGID:
		return VerdictOwnerMismatch
	}
	return VerdictOK
}

// permBits converts a FileMode to the twelve unix bits. Go keeps setuid,
// setgid and sticky in its own high bits and Perm drops them, but a setgid
// download folder is exactly what explains a file's unexpected group.
func permBits(m os.FileMode) uint32 {
	out := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		out |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		out |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		out |= 0o1000
	}
	return out
}

// Octal formats a permission mask as four octal digits, so a setgid leading
// digit is not dropped.
func Octal(mode uint32) string { return fmt.Sprintf("%04o", mode&0o7777) }

// Advise returns a line to log when the data directory cannot be written, or
// ("", false). Without it the boot fails with a bare "permission denied" from
// SQLite; this names the owners and the chown that fixes it. A missing folder
// gets nothing, since MkdirAll's own error explains it.
func Advise(dir string) (string, bool) {
	folder := Of(dir)
	if !folder.Exists || !folder.IsDir {
		return "", false
	}
	err := probeWritable(dir)
	if err == nil {
		return "", false
	}
	return advice(dir, folder, Who(), err), true
}

// advice builds Advise's sentence; it is separate so a test pins it on every
// platform.
func advice(dir string, folder Owner, me Identity, err error) string {
	if !folder.Known || !me.Known {
		// Windows: the fix is an ACL, so no chown line.
		return fmt.Sprintf("the data directory %s cannot be written by this process: %v", dir, err)
	}
	return fmt.Sprintf(
		"the data directory %s belongs to %s and this process runs as %s, so it cannot be written (%v); on the host, run: chown -R %d:%d %s",
		dir, describe(folder.UID, folder.GID, folder.User, folder.Group),
		describe(me.UID, me.GID, me.User, me.Group), err, me.UID, me.GID, dir)
}

// describe is "1000:1000 (knight:knight)", or "99:100" when neither id has a
// name. The numbers come first because they go into the chown command.
func describe(uid, gid int, user, group string) string {
	if user == "" && group == "" {
		return fmt.Sprintf("%d:%d", uid, gid)
	}
	name := func(s string, n int) string {
		if s == "" {
			return fmt.Sprint(n)
		}
		return s
	}
	return fmt.Sprintf("%d:%d (%s:%s)", uid, gid, name(user, uid), name(group, gid))
}

// createProbeFile makes one empty file with the app's download permissions
// and returns its path.
//
// os.CreateTemp is not used: it always creates 0600, so every probe would
// report a problem. Chmod afterwards would ignore the umask and measure its
// own number. The name is random because two instances may share a folder
// and O_EXCL would otherwise make their checks fail each other.
func createProbeFile(dir string) (string, error) {
	var err error
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, probeName())
		var f *os.File
		f, err = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, probeFilePerm)
		if err == nil {
			// An empty file carries owner and mode just the same.
			_ = f.Close()
			return name, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", err
}

// createProbeDir makes one directory with the app's directory permissions.
// os.MkdirTemp would always create 0700.
func createProbeDir(dir string) (string, error) {
	var err error
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, probeName())
		if err = os.Mkdir(name, probeDirPerm); err == nil {
			return name, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", err
}

// probeWritable reports whether this process can create a file in dir.
func probeWritable(dir string) error {
	name, err := createProbeFile(dir)
	if err != nil {
		return err
	}
	return os.Remove(name)
}

// probeName is a name nothing else will pick. O_EXCL prevents collisions; the
// randomness only keeps two checks apart.
func probeName() string {
	return fmt.Sprintf("%s%d-%d", probePrefix, os.Getpid(), rand.Uint64())
}
