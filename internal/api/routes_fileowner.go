package api

// Who this instance writes files as, and what actually happens to a file it
// writes into each configured folder.
//
// WHY IT IS A ROUTE AT ALL. Nothing in a browser can answer any part of this. A
// page cannot stat a folder, cannot read a uid, and cannot tell that the
// perfectly successful save it just made produced a file the media server next
// door will not be able to touch. The symptom people arrive with is "Jellyfin
// says the library is empty" or "I cannot delete the downloads over SMB", and
// the cause is two numbers nothing in the app has ever shown them.
//
// TWO ROUTES BECAUSE ONE OF THEM WRITES. GET /api/fileowner reads the process's
// own identity and the environment, which costs nothing and changes nothing, so
// a page may hold it and refresh it freely. POST /api/fileowner/check creates a
// real file and a real sub-directory in every folder it measures, and that is
// the whole reason it is a POST: app_diskreport.go's header states the rule in
// full, and settings.Validate breaking it (MkdirAll plus a probe file, from a
// validator) is what makes the rule worth writing down. A GET that littered
// every configured folder each time a page mounted would be the same mistake
// one layer further out.
//
// THIS MACHINE'S OWNERS, AND ONLY THIS MACHINE'S. Neither route is on the
// federation forwarder's list (routes_federation.go forwards by path pattern,
// and only tasks, links and the queue) and neither is relay-forwardable
// (routes_relay.go, whose own comment says a new route stays outside until
// somebody decides otherwise). routes_diskspace.go makes the argument in full
// for disks and it is stronger here: a peer's answer names THAT box's uids and
// THAT box's folders, and shown while a peer is selected it would tell an
// operator to run a chown against a path on the wrong machine.
//
// SECURITY, because this answers to whoever holds a session. It sends the same
// folder paths /api/folders already lists and the settings page already shows in
// a text box, plus the numeric uid and gid this process runs as, plus the
// verbatim value of three environment variables that are visible in
// `docker inspect` to anybody who can reach the daemon. No file names, no
// contents, no folder this app would not itself write into. reg.Add and never
// AddOpen: there is no credential of its own in either request.
//
// AND IT PROMISES NOTHING IT CANNOT DO. The image declares USER knight, so this
// process cannot change its own uid and PUID/PGID are read by nothing at any
// layer. The readout says what the ownership IS and says plainly that those
// variables are not read; it never implies that setting one would change
// anything. See envReadByThisBuild.

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/fileowner"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The three variables an operator reaches for when downloads land with the
// wrong owner. They are reported VERBATIM and acted on by nothing.
//
// PUID/PGID/UMASK rather than KL_PUID and friends, and the naming is not free
// either way. These are the names every other container on a NAS uses, which is
// the whole reason somebody types them; a KL_-prefixed pair would also break
// check-docs-claims.mjs, which fails when the README documents a `KL_*` row that
// no Go file mentions. The flip side is worth saying out loud: these three get
// no machine check at all, so the day an entrypoint does read them, nothing
// automatic will notice if this list falls behind it.
const (
	envPUID  = "PUID"
	envPGID  = "PGID"
	envUMASK = "UMASK"
)

// envReadByThisBuild says whether anything in this image acts on PUID, PGID or
// UMASK. It is false, and it is a named constant with a test behind it rather
// than a literal, because the day it changes it has to change in company.
//
// WHY FALSE. Dockerfile declares USER knight, so the process starts as uid 1000
// and an unprivileged process cannot become another uid - syscall.Setuid from
// here fails, full stop. Making PUID take effect means starting the container as
// root and dropping privileges in an entrypoint, which turns a deliberately
// non-root image into a root-started one; that is a decision with a security
// posture attached and it has not been taken. Until it is, a variable somebody
// set is a variable that did nothing, and saying so is the single most useful
// line this feature produces at three in the morning: PUID=99 sitting beside an
// effective uid of 1000 is otherwise indistinguishable, from inside the app,
// from a setting that worked.
//
// WHAT DOES WORK IS STILL REPORTED HONESTLY. A container started with
// --user 99:100, a Kubernetes runAsUser, or rootless Docker all pick this
// process's identity before it starts, and the uid below is then that identity.
// Which is why the readout leads with what is IN FORCE and treats the variables
// as a footnote rather than the other way round.
const envReadByThisBuild = false

// OwnerEnv is what the operator asked for, verbatim and untouched.
//
// "" means the variable is not set, which is NOT the same as 0. Unset PUID means
// the image's own uid 1000, unset UMASK means the runtime's mask (022 on every
// image here); reporting either as a zero would turn "the default applies" into
// "somebody asked for root" and "somebody asked for world-writable".
type OwnerEnv struct {
	PUID  string `json:"puid"`
	PGID  string `json:"pgid"`
	Umask string `json:"umask"`
}

// OwnerIdentity is who this instance writes files as.
type OwnerIdentity struct {
	// Known is false where files have no owner at all (the Windows desktop
	// build). Every number below then means NOTHING - not uid 0, not root - and
	// whatever draws it has to say that in words rather than print zeroes. Same
	// third-answer rule as VolumeReport.Known.
	Known bool `json:"known"`
	// Deployment is "container" or "desktop" (internal/buildinfo), because the
	// two need completely different sentences: PUID is a container idea, and on
	// the desktop the app runs as the person sitting in front of it.
	Deployment string `json:"deployment"`
	// UID and GID are the effective ids, which is what the kernel stamps on a
	// file at creation. User and Group are the names behind them, or "" when
	// there is no passwd entry - normal under --user 99:100 and not an error.
	UID   int    `json:"uid"`
	GID   int    `json:"gid"`
	User  string `json:"user"`
	Group string `json:"group"`
	// Umask is the effective mask as four octal digits ("0022"), and UmaskKnown
	// says whether it could be read at all. There is no portable read-only
	// umask(2) and the classic read-then-restore trick is a race in a process
	// that creates files on a dozen goroutines, so this comes from
	// /proc/self/status where that exists and is absent everywhere else.
	Umask      string `json:"umask"`
	UmaskKnown bool   `json:"umaskKnown"`
	// Env is what the operator set, verbatim.
	Env OwnerEnv `json:"env"`
	// EnvRead says whether anything in this build acts on those three. It is
	// false; see envReadByThisBuild for why, and note that the page's job is then
	// to explain that rather than to hide it.
	EnvRead bool `json:"envRead"`
}

// FolderOwnership is one configured folder as a STAT saw it: who owns it, what
// its mode is, and nothing created to find out.
//
// It is the cheap half, and it is what the diagnostics bundle carries. That
// bundle is fetched by the page on mount, so it may not write: a bundle that
// dropped a probe file into every configured folder each time somebody opened
// the diagnostics page is exactly the mistake the disk report refused to make.
//
// IT CARRIES NO PATH, AND NO RAW OS ERROR EITHER. That is the diagnostics
// bundle's own rule (see routes_diagnostics.go's StoreBytes comment): the file
// is meant to be attached to a PUBLIC bug report, and on the desktop build the
// download folder with nothing configured is
// C:\Users\<a person's real name>\AppData\..., because app.New puts the default
// one inside the data directory. A stat error carries the same path inside its
// message, which is why Detail is dropped here and kept on the session-guarded
// check route, where the person reading it is looking at their own settings
// page.
//
// The role and the numbers are the whole of what this is for anyway: "the
// download folder belongs to 99:100 and this process is 1000:1000" is the
// finding, and it needs no path to be understood.
type FolderOwnership struct {
	Role   string `json:"role"`
	Exists bool   `json:"exists"`
	Known  bool   `json:"known"`
	UID    int    `json:"uid"`
	GID    int    `json:"gid"`
	User   string `json:"user"`
	Group  string `json:"group"`
	// Mode as four octal digits, so a set-group-id folder reads "2775" rather
	// than as a decimal nobody can compare with what `ls -l` said.
	Mode string `json:"mode"`
}

// FolderOwnerProbe is one folder as the check MEASURED it: a real file and a
// real sub-directory were created inside it, stat-ed, and removed again.
//
// The modes travel as strings for the reason FolderOwner.Mode does. The owners
// travel as numbers AND names because the number is what goes into a chown or a
// --user, and the name is often absent.
type FolderOwnerProbe struct {
	Dir    string `json:"dir"`
	Role   string `json:"role"`
	Exists bool   `json:"exists"`
	Known  bool   `json:"known"`

	DirUID   int    `json:"dirUid"`
	DirGID   int    `json:"dirGid"`
	DirUser  string `json:"dirUser"`
	DirGroup string `json:"dirGroup"`
	DirMode  string `json:"dirMode"`

	FileUID   int    `json:"fileUid"`
	FileGID   int    `json:"fileGid"`
	FileUser  string `json:"fileUser"`
	FileGroup string `json:"fileGroup"`
	FileMode  string `json:"fileMode"`

	// SubdirMode is what a new per-package folder comes out as, and it is the
	// field a file-only probe would not have: with a tight umask the files can be
	// fine while the folder containing them cannot be entered.
	SubdirMode string `json:"subdirMode"`

	// Verdict is a stable id, never prose - fileowner's Verdict* constants. The
	// server has no idea which of the forty-two locales is reading.
	Verdict string `json:"verdict"`
	Detail  string `json:"detail,omitempty"`
}

// FolderOwnerReport is one run of the check.
type FolderOwnerReport struct {
	CheckedAt time.Time `json:"checkedAt"`
	// Folders is never nil: a nil slice encodes as JSON null and the page that
	// maps over it throws rather than drawing nothing.
	Folders []FolderOwnerProbe `json:"folders"`
}

func registerFileOwner(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/fileowner",
		"the uid, gid and umask this instance writes files with, beside what PUID, PGID and UMASK were set to",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, ownerIdentity())
		})

	// POST because it WRITES. It is not a mutation of any stored state, and a
	// GET would be the natural verb for a readout - but a route that creates a
	// file in every configured folder must not be reachable by a link, a
	// prefetch, or a page refresh.
	reg.Add(http.MethodPost, "/api/fileowner/check",
		"measure what a file written into each configured folder actually comes out as; creates and removes one probe file and one probe folder per directory",
		func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				// Dirs narrows the run to some of the folders. An absent or
				// unreadable body means all of them, which is what decodeBody is
				// for: a bare POST with no payload at all is a valid request here.
				Dirs []string `json:"dirs"`
			}
			_ = decodeBody(r, &req)

			targets, err := selectTargets(a.TargetFolders(), req.Dirs)
			if err != nil {
				// A 400 and not a silent drop. The alternative - measuring the
				// folders it recognised and saying nothing about the rest - is a
				// report that looks complete and is not, which is the failure mode
				// this whole feature exists to remove rather than add to.
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rep := FolderOwnerReport{CheckedAt: time.Now().UTC(), Folders: []FolderOwnerProbe{}}
			for _, tf := range targets {
				p := fileowner.Check(tf.Dir)
				rep.Folders = append(rep.Folders, FolderOwnerProbe{
					Dir: p.Dir, Role: tf.Role, Exists: p.Exists, Known: p.Known,

					DirUID: p.DirUID, DirGID: p.DirGID,
					DirUser: p.DirUser, DirGroup: p.DirGroup,
					DirMode: fileowner.Octal(p.DirMode),

					FileUID: p.FileUID, FileGID: p.FileGID,
					FileUser: p.FileUser, FileGroup: p.FileGroup,
					FileMode: fileowner.Octal(p.FileMode),

					SubdirMode: fileowner.Octal(p.SubdirMode),
					Verdict:    p.Verdict,
					Detail:     p.Detail,
				})
			}
			writeJSON(w, rep)
		})
}

// selectTargets narrows the configured folders to the ones the caller named, or
// returns all of them when it named none.
//
// A DIRECTORY THAT IS NOT ONE OF THIS INSTANCE'S IS REFUSED RATHER THAN
// MEASURED, and that is a deliberate narrowing of what the route can be made to
// do. The check writes, so an unfiltered version would be "create and delete a
// dot-file anywhere on this host, as this process" behind one session. Nobody
// gains anything by it: the page only ever asks about folders it read out of
// this same list a moment earlier, and the case the parameter exists for is
// re-checking the two folders somebody just saved rather than all fourteen.
//
// The comparison runs through the same FixedPrefix-and-Clean the list itself was
// built with, so a caller may send back exactly the string it was given.
func selectTargets(all []app.TargetFolder, dirs []string) ([]app.TargetFolder, error) {
	if len(dirs) == 0 {
		return all, nil
	}
	wanted := map[string]bool{}
	for _, d := range dirs {
		wanted[filepath.Clean(settings.FixedPrefix(strings.TrimSpace(d)))] = true
	}
	keep := make([]app.TargetFolder, 0, len(dirs))
	for _, tf := range all {
		if wanted[tf.Dir] {
			keep = append(keep, tf)
			delete(wanted, tf.Dir)
		}
	}
	if len(wanted) == 0 {
		return keep, nil
	}
	left := make([]string, 0, len(wanted))
	for d := range wanted {
		left = append(left, d)
	}
	// Sorted so the same bad request produces the same sentence twice running.
	// A message whose word order comes out of a map is a message nobody can
	// search a log for.
	sort.Strings(left)
	return nil, &ownerError{"not a folder this instance writes into: " + strings.Join(left, ", ")}
}

// ownerError is a plain message. It exists rather than errors.New so that the
// handler above reads as one refusal with one sentence, and so that nothing
// downstream is tempted to match on its text.
type ownerError struct{ msg string }

func (e *ownerError) Error() string { return e.msg }

// ownerIdentity is the GET answer: what is in force, and what was asked for,
// side by side.
//
// THE TWO HAVE TO TRAVEL TOGETHER OR THE READOUT IS WORTHLESS. An operator who
// typed PUID=99 into the template and is looking at downloads owned by 1000 has
// no way, from inside the app, to tell "the variable is ignored by this image"
// from "the variable is wrong" from "something overrode it". One line with both
// halves on it answers all three at once.
func ownerIdentity() OwnerIdentity {
	me := fileowner.Who()
	id := OwnerIdentity{
		Known:      me.Known,
		Deployment: buildinfo.Deployment,
		UID:        me.UID,
		GID:        me.GID,
		User:       me.User,
		Group:      me.Group,
		UmaskKnown: me.UmaskKnown,
		Env: OwnerEnv{
			PUID:  os.Getenv(envPUID),
			PGID:  os.Getenv(envPGID),
			Umask: os.Getenv(envUMASK),
		},
		EnvRead: envReadByThisBuild,
	}
	if me.UmaskKnown {
		id.Umask = fileowner.Octal(uint32(me.Umask))
	}
	// Left empty rather than sent as "0000" when it could not be read: "0000"
	// is a real and alarming mask (every new file world-writable) and must not
	// be what "this kernel does not report it" looks like on screen.
	return id
}

// ownershipDiagnostics is the read-only half, for the bug report bundle.
//
// A report that does not carry the uid this instance writes as makes whoever
// reads it go and ask for it, which is a round trip per report - and the person
// filing it usually does not know how to find out. One stat per configured
// folder is cheap enough to include unconditionally; the probe half is not, and
// is not here.
func ownershipDiagnostics(a *app.App) (OwnerIdentity, []FolderOwnership) {
	folders := []FolderOwnership{}
	for _, tf := range a.TargetFolders() {
		o := fileowner.Of(tf.Dir)
		folders = append(folders, FolderOwnership{
			Role: tf.Role, Exists: o.Exists, Known: o.Known,
			UID: o.UID, GID: o.GID, User: o.User, Group: o.Group,
			Mode: fileowner.Octal(o.Mode),
		})
	}
	return ownerIdentity(), folders
}
