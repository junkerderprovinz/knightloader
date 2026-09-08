// Package fileowner answers the question a write probe cannot: not "may this
// process write here", but "who does what it writes here BELONG to, and who
// else can open it afterwards".
//
// WHY THE WRITE PROBE THAT ALREADY EXISTS IS NOT ENOUGH. settings.Validate
// creates .knightloader-write-test, writes two bytes, removes it and calls the
// folder good; internal/watch does the same under its own name. Both are right
// about what they ask. Neither can see the failure this package exists for: on
// a NAS the share is reachable, the write SUCCEEDS, and the file simply lands
// owned by whoever this process is rather than by whoever owns the share. Every
// check built on "could I write" reports green while the media server next door
// reports an empty library, and there is nothing on any page to connect the two.
// So the comparison here is between the owner the probe file CAME OUT WITH and
// the owner the folder already had.
//
// EVERYTHING IS MEASURED AND NOTHING IS COMPUTED. The obvious implementation is
// os.Geteuid() for the owner and the process umask for the mode, and it is
// wrong on precisely the filesystems this feature is for: a set-group-id parent
// directory hands out its OWN group rather than the process's, an NFS export
// with root squash rewrites the owner on the server side, and any mount carrying
// a umask= option ignores the process's umask entirely. All three are ordinary
// on a NAS. So Check creates a real file and a real sub-directory, stats both,
// and removes them again; the numbers it reports are the numbers the filesystem
// actually produced.
//
// KNOWN IS A THIRD ANSWER AND NOT A ZERO, the same rule internal/diskspace is
// built on and for the same reason. A Windows desktop build has no unix owners
// at all; a uid of 0 there would read as "root owns your downloads", which is a
// confident answer to a question that cannot be asked. When Known is false every
// number in these structs means NOTHING, and whatever draws them has to say so
// in words rather than print them.
//
// NOTHING HERE READS PUID, PGID OR UMASK, and nothing here changes an owner.
// The image declares USER knight (Dockerfile), so this process starts as uid
// 1000 and an unprivileged process cannot become another uid: syscall.Setuid
// from here fails, full stop. A package that "implemented PUID" without the
// image starting as root would be a variable that is read and then silently
// ignored, which is worse than not reading it. What this package does instead is
// report what the ownership IS, so the operator can fix it where it can actually
// be fixed: in the run command, or with one chown on the host.
package fileowner

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// The verdicts, as stable ids that never carry English prose.
//
// Same rule as app_diskreport.go's role constants and routes_features.go's
// Feature.ID: the server has no idea which of the forty-two locales is reading,
// so it sends the id and whatever draws it looks the sentence up in its own
// language. A verdict that travelled as a sentence would be an English string
// on a German page, permanently.
const (
	// VerdictOK is a folder whose probe file came out owned by the folder's own
	// owner, group-readable, in a sub-directory the group can enter.
	VerdictOK = "ok"
	// VerdictOwnerMismatch is the headline failure this package was written for:
	// the write worked and the file belongs to somebody else than the folder.
	VerdictOwnerMismatch = "ownerMismatch"
	// VerdictGroupUnreadable is a probe file with no group read bit, so only its
	// own owner can open what lands here.
	VerdictGroupUnreadable = "groupUnreadable"
	// VerdictDirUnreadable is the case a file-only probe passes and the library
	// still stays invisible: the FOLDER that new downloads are put in has no
	// group read or execute bit, so nothing gets inside it whatever the files in
	// it allow.
	VerdictDirUnreadable = "dirUnreadable"
	// VerdictNotWritable is a folder this process cannot write into at all. It
	// is meaningful on every platform, Windows included, because it is about the
	// attempt rather than about the owners.
	VerdictNotWritable = "notWritable"
	// VerdictMissing is a folder that is not there. It is deliberately not
	// created: see Check.
	VerdictMissing = "missing"
	// VerdictUnknown is a platform with no file owners to compare. Not a fault,
	// and not a zero either.
	VerdictUnknown = "unknown"
)

// The permission bits the probe asks for.
//
// THESE TWO NUMBERS ARE NOT A STYLE CHOICE, they are copied from
// internal/collide's Options.perm() and Options.dirPerm(), which is what every
// finished download and every package sub-folder in this app is actually
// created with. The measured mode is `requested &^ umask`, so a probe that
// asked for anything else would measure a mode no real download ever gets.
//
// It also settles a question that would otherwise be answered wrongly on the
// page: with the app asking for 0644, a umask of 002 changes NOTHING (0644 &^
// 002 is still 0644). Advice to "set UMASK=002 so files become 0664" is
// therefore false here, however standard it is elsewhere, and the only way to
// know that is to probe with the app's own numbers.
const (
	probeFilePerm = 0o644
	probeDirPerm  = 0o755
)

// probePrefix is the name every probe file and probe directory starts with.
//
// A dot-file for the same reason the two existing probes are one: it must not
// show up in a file browser, in an SMB listing or in a media scanner's watch
// folder for the fraction of a second it exists. The name says which app left
// it, because the one thing worse than a probe file is an unattributable one
// somebody finds after a crash and dares not delete.
const probePrefix = ".knightloader-owner-"

// Identity is who this process writes files as.
type Identity struct {
	// Known is false where the concept does not exist (Windows, wasm). Every
	// other field is then meaningless rather than zero.
	Known bool
	// UID and GID are the EFFECTIVE ids, not the real ones. The effective id is
	// what the kernel stamps on a file at creation, which is the only thing this
	// package is about; on every deployment this ships for the two are equal
	// anyway, so the choice costs nothing and is right in the case where it
	// would matter.
	UID, GID int
	// User and Group are the names behind those ids, or "" when the passwd or
	// group database has no entry. THAT IS NORMAL AND NOT AN ERROR: a container
	// started with --user 99:100 has a uid that no /etc/passwd inside the image
	// mentions, and the number on its own is a perfectly usable answer.
	User, Group string
	// Umask is the process umask and UmaskKnown says whether it could be read at
	// all. There is no portable read-only umask(2): the classic trick of calling
	// umask(x) and then umask(back) is a race in a process with dozens of
	// goroutines, any of which may be creating a file in between, so it is not
	// done. Linux exposes it in /proc/self/status, which arrived in 4.7 and is
	// absent everywhere else - hence the flag rather than a guess. The folder
	// probe's measured mode is the authoritative number in any case.
	Umask      int
	UmaskKnown bool
}

// Owner is one path's identity and permission bits as a stat saw it. Nothing is
// created and nothing is written to get one.
type Owner struct {
	Path   string
	Exists bool
	IsDir  bool
	// Known has the same meaning it has on Identity: false means the numbers
	// below are not small, they are absent.
	Known       bool
	UID, GID    int
	User, Group string
	// Mode is the permission bits only (the low twelve, so setuid/setgid/sticky
	// survive). The type bits are left out deliberately: a mode of 020000000755
	// printed in a readout is a directory bit somebody has to know to ignore.
	Mode uint32
	// Detail is the raw OS error when the stat failed, kept verbatim. A
	// translated "could not read the folder" loses the one word (EACCES, ESTALE,
	// "no such device") that says which of five completely different problems it
	// is.
	Detail string
}

// Probe is one folder as Check MEASURED it: a real file and a real
// sub-directory were created inside it, stat-ed, and removed again.
type Probe struct {
	Dir    string
	Exists bool
	Known  bool

	// The folder itself.
	DirUID, DirGID    int
	DirUser, DirGroup string
	DirMode           uint32

	// The probe file, which is what a finished download will look like.
	FileUID, FileGID    int
	FileUser, FileGroup string
	FileMode            uint32

	// The probe sub-directory's mode, which is what a per-package folder will
	// look like. Its owner is not reported separately because it is created by
	// the same process in the same folder as the file and cannot differ from it
	// in any way this readout could act on.
	SubdirMode uint32

	// Verdict is one of the constants above. Detail carries the raw OS error for
	// the two verdicts that have one.
	Verdict string
	Detail  string
}

// Who is the identity this process creates files with.
func Who() Identity { return who() }

// Of is one path's owner and mode, read with a single stat.
//
// It writes nothing, which is what makes it usable from a readout: the
// diagnostics bundle is fetched by the page on mount, and a bundle that littered
// a probe file into every configured folder on every page open is exactly the
// mistake app_diskreport.go's header refuses by name.
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

// Check measures one folder by writing into it, and is therefore never called
// from anything that answers a GET.
//
// THE FOLDER IS NOT CREATED WHEN IT IS MISSING. settings.Validate already
// MkdirAll's every configured folder as it is saved, so by the time anybody can
// press a button here the folder exists unless something went away - a mount
// that did not come up, a share that was renamed. Creating it here would hide
// exactly that, and would put a directory on disk as a side effect of a check
// nobody asked to change anything.
//
// Both probes are removed in a defer, so a panic between the two still cleans
// up. A removal that fails is not reported: the measurement already succeeded,
// and a verdict that turned into "could not tidy up" would bury the answer the
// operator pressed the button for.
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
		// A folder that takes a file and refuses a directory is rare and real
		// (some FUSE mounts, an exhausted inode table), and it is worth the
		// separate attempt: the per-package sub-folder is where every download
		// actually lands, so a folder that cannot hold one is not usable at all.
		p.Verdict, p.Detail = VerdictNotWritable, err.Error()
		return p
	}
	defer func() { _ = os.Remove(sub) }()

	f := Of(file)
	p.FileUID, p.FileGID = f.UID, f.GID
	p.FileUser, p.FileGroup = f.User, f.Group
	p.FileMode = f.Mode
	p.SubdirMode = Of(sub).Mode
	// Known is the AND of the folder and the file: if either could not be read,
	// there is no comparison to make. In practice they agree, because they are
	// two stats on one filesystem a microsecond apart.
	p.Known = folder.Known && f.Known

	p.Verdict = judge(p)
	return p
}

// judge picks the headline verdict from a finished measurement.
//
// It is pure, portable and has no syscall in it, which is the whole reason it
// is a function rather than four lines inside Check: the rule then has one
// implementation, and a test of it runs on every platform. The same argument
// diskspace.spaceFrom's own comment makes about arithmetic living outside the
// build-tagged files. A rule written twice behind build tags is a rule pinned
// only on whichever machine happened to run the test.
//
// It decides between the OWNERSHIP verdicts only. A missing folder and a refused
// write are settled by Check itself, because those two are the measurement
// failing rather than a reading of it.
//
// THE ORDER IS THE JUDGEMENT. The sub-directory comes first because it is the
// total failure: nothing gets inside it whatever the files in it allow, so
// reporting a mismatched owner while the folder itself is a locked door would
// send somebody to fix the smaller of two problems. The owner comparison comes
// last because it is the one that is still worth reporting when everything else
// is fine.
func judge(p Probe) string {
	switch {
	case !p.Known:
		return VerdictUnknown
	// 0o050 is group read plus group execute. Both are needed on a directory and
	// they fail differently: without x it cannot be entered at all, without r its
	// contents cannot be listed. Neither is a state anybody wants a download
	// folder in, and separating them would buy a second verdict that leads to
	// the same fix.
	case p.SubdirMode&0o050 != 0o050:
		return VerdictDirUnreadable
	case p.FileMode&0o040 == 0:
		return VerdictGroupUnreadable
	case p.FileUID != p.DirUID || p.FileGID != p.DirGID:
		return VerdictOwnerMismatch
	}
	return VerdictOK
}

// permBits is one FileMode as the twelve bits `ls -l` and `chmod` talk in.
//
// Go keeps setuid, setgid and sticky in its OWN high bits (ModeSetuid is bit 23)
// rather than in the unix 04000/02000/01000 the rest of the world writes them
// as, and FileMode.Perm() drops all three outright. Both halves matter here:
// a set-group-id download folder is one of the two things this whole package
// exists to explain (it is what hands a new file a group its creator is not in),
// so a readout that silently dropped that bit would be describing a different
// folder than the one on disk.
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

// Octal formats a permission mask the way anybody reading it expects to see it,
// zero-padded to four digits so 0o644 and 0o022 line up under each other and
// the leading digit of a setgid folder is not silently dropped.
func Octal(mode uint32) string { return fmt.Sprintf("%04o", mode&0o7777) }

// Advise is the one line to log when the data directory cannot be written, or
// ("", false) when there is nothing to say.
//
// It exists because of what the failure looks like without it. app.New opens
// SQLite in that folder, fails, and main reports `start: permission denied` with
// no path, no owner and no uid - and the cause is nearly always one chown on the
// host that nobody can guess from that sentence. This does not repair anything
// and does not stop the boot: the existing failure still happens, it simply
// stops being anonymous.
//
// A folder that is not there yet produces nothing, because os.MkdirAll is about
// to try to create it and its own error names the reason perfectly well.
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

// advice is Advise's sentence, split out so that the words, the chown line and
// the two owner descriptions are pinned by a test on every platform rather than
// only on a machine that happens to have an unwritable directory lying about.
func advice(dir string, folder Owner, me Identity, err error) string {
	if !folder.Known || !me.Known {
		// No owners to name, so the sentence says the one thing it knows. This is
		// the Windows desktop build, where the fix is an ACL rather than a chown
		// and inventing a chown line would send somebody to a command that does
		// not exist.
		return fmt.Sprintf("the data directory %s cannot be written by this process: %v", dir, err)
	}
	return fmt.Sprintf(
		"the data directory %s belongs to %s and this process runs as %s, so it cannot be written (%v); on the host, run: chown -R %d:%d %s",
		dir, describe(folder.UID, folder.GID, folder.User, folder.Group),
		describe(me.UID, me.GID, me.User, me.Group), err, me.UID, me.GID, dir)
}

// describe is "1000:1000 (knight:knight)", or just "99:100" when neither number
// has a name behind it. The numbers always lead, because the numbers are what
// goes into the chown command and what appears in the run command; the names
// are the part that makes the line readable and the part that is often absent.
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

// createProbeFile makes one file with the app's own download permissions and
// returns its path.
//
// os.CreateTemp is deliberately not used, and this is the single most important
// line in the package to get right: it creates with 0600, hard-coded, so every
// probe would measure 0600 regardless of the umask and the whole readout would
// report a permission problem on a perfectly healthy folder. Chmod-ing
// afterwards is worse still - chmod sets the mode outright and ignores the
// umask, so the probe would measure the number it just wrote and always agree
// with itself.
//
// The unique suffix therefore has to be built here. Two instances can share one
// download folder (that is what the whole federation half of this app is for),
// and a fixed name would make two simultaneous checks report notWritable at each
// other through O_EXCL.
func createProbeFile(dir string) (string, error) {
	var err error
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, probeName())
		var f *os.File
		f, err = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, probeFilePerm)
		if err == nil {
			// Closed immediately: nothing is written into it. A zero-length file
			// carries an owner and a mode exactly as a full one does, and writing
			// bytes would only add a way for the probe to fail on a full disk.
			_ = f.Close()
			return name, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", err
}

// createProbeDir makes one directory with the app's own directory permissions.
//
// os.MkdirTemp is not used for the same reason os.CreateTemp is not: it creates
// with 0700 and the measurement would be of that number rather than of the
// folder's own behaviour.
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

// probeWritable is the plain "can this process write here at all" question, used
// by Advise where there is nothing to compare owners with yet.
func probeWritable(dir string) error {
	name, err := createProbeFile(dir)
	if err != nil {
		return err
	}
	return os.Remove(name)
}

// probeName is a name nothing else will pick. math/rand/v2 is seeded by the
// runtime and is safe from several goroutines, and this is not a security
// boundary: O_EXCL is what actually prevents a collision, the randomness only
// keeps two checks from meeting in the first place.
func probeName() string {
	return fmt.Sprintf("%s%d-%d", probePrefix, os.Getpid(), rand.Uint64())
}
