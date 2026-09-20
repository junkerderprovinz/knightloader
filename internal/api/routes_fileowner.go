package api

// Who this instance writes files as, and what actually happens to a file it
// writes into each configured folder. A browser cannot stat a folder or read a
// uid, yet "the media server cannot see my downloads" usually comes down to
// those two numbers.
//
// GET /api/fileowner only reads the process identity and the environment. POST
// /api/fileowner/check creates a real file and sub-directory in every folder it
// measures, which is why it is a POST (see app_diskreport.go).
//
// Neither route is forwarded to peers (routes_federation.go, routes_relay.go):
// a peer's answer names that box's uids and folders, and would send somebody
// to chown a path on the wrong machine. They send the folder paths /api/folders
// already lists, the uid and gid, and three environment variables visible in
// `docker inspect`; no file names or contents.
//
// The image runs as USER knight and nothing reads PUID or PGID, so the readout
// says what the ownership is and that those variables are not read (see
// envReadByThisBuild).

import (
	"errors"
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

// The variables an operator reaches for when downloads land with the wrong
// owner, reported verbatim and acted on by nothing. They use the names other
// NAS containers use rather than a KL_ prefix, which also keeps
// check-docs-claims.mjs from expecting a Go reader for them; in turn nothing
// automatic notices if an entrypoint starts reading them.
const (
	envPUID  = "PUID"
	envPGID  = "PGID"
	envUMASK = "UMASK"
)

// envReadByThisBuild says whether anything in this image acts on PUID, PGID or
// UMASK. The Dockerfile declares USER knight, and an unprivileged process
// cannot change its uid; honouring PUID would mean starting as root and
// dropping privileges in an entrypoint, which has not been decided. An
// identity chosen before start (--user, runAsUser, rootless Docker) is what
// the readout reports as in force. A test keeps this in step with the
// Dockerfile.
const envReadByThisBuild = false

// OwnerEnv is what the operator set, verbatim. "" means unset, which differs
// from 0: an unset PUID means the image's uid 1000, not root.
type OwnerEnv struct {
	PUID  string `json:"puid"`
	PGID  string `json:"pgid"`
	Umask string `json:"umask"`
}

// OwnerIdentity is who this instance writes files as.
type OwnerIdentity struct {
	// Known is false where files have no owner (the Windows desktop build);
	// the numbers below then mean nothing, not root.
	Known bool `json:"known"`
	// Deployment is "container" or "desktop"; PUID is a container idea.
	Deployment string `json:"deployment"`
	// UID and GID are the effective ids the kernel stamps on new files. User
	// and Group are "" when there is no passwd entry, which is normal under
	// --user 99:100.
	UID   int    `json:"uid"`
	GID   int    `json:"gid"`
	User  string `json:"user"`
	Group string `json:"group"`
	// Umask is the effective mask as four octal digits. It comes from
	// /proc/self/status where that exists, since reading umask(2) means
	// setting it, which races with other goroutines creating files.
	Umask      string   `json:"umask"`
	UmaskKnown bool     `json:"umaskKnown"`
	Env        OwnerEnv `json:"env"`
	// EnvRead says whether anything in this build acts on Env; see
	// envReadByThisBuild.
	EnvRead bool `json:"envRead"`
}

// FolderOwnership is one configured folder as a stat saw it, with nothing
// created to find out. It goes into the diagnostics bundle, so it carries a
// role but no path and no OS error text, which would contain the path.
type FolderOwnership struct {
	Role   string `json:"role"`
	Exists bool   `json:"exists"`
	Known  bool   `json:"known"`
	UID    int    `json:"uid"`
	GID    int    `json:"gid"`
	User   string `json:"user"`
	Group  string `json:"group"`
	// Mode is four octal digits, comparable with what `ls -l` shows.
	Mode string `json:"mode"`
}

// FolderOwnerProbe is one folder as the check measured it, by creating,
// stat-ing and removing a file and a sub-directory inside it. Owners travel as
// numbers, for chown and --user, and as names, which are often absent.
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

	// SubdirMode is what a new per-package folder comes out as; with a tight
	// umask the files can be fine while their folder cannot be entered.
	SubdirMode string `json:"subdirMode"`

	// Verdict is one of fileowner's Verdict ids, translated by the page.
	Verdict string `json:"verdict"`
	Detail  string `json:"detail,omitempty"`
}

// FolderOwnerReport is one run of the check.
type FolderOwnerReport struct {
	CheckedAt time.Time `json:"checkedAt"`
	// Folders is never nil, so it never encodes as null.
	Folders []FolderOwnerProbe `json:"folders"`
}

func registerFileOwner(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/fileowner",
		"the uid, gid and umask this instance writes files with, beside what PUID, PGID and UMASK were set to",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, ownerIdentity())
		})

	// A POST because it creates a file in every configured folder, which a
	// link, a prefetch or a refresh must not trigger.
	reg.Add(http.MethodPost, "/api/fileowner/check",
		"measure what a file written into each configured folder actually comes out as; creates and removes one probe file and one probe folder per directory",
		func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				// Dirs narrows the run; an absent body means all folders.
				Dirs []string `json:"dirs"`
			}
			_ = decodeBody(r, &req)

			targets, err := selectTargets(a.TargetFolders(), req.Dirs)
			if err != nil {
				// Refused rather than silently measuring only the known
				// folders, which would look like a complete report.
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

// selectTargets narrows the configured folders to the ones the caller named,
// or returns all of them when it named none. A directory that is not one of
// this instance's is refused, or the check would create files anywhere on the
// host. Names are compared after the same FixedPrefix and Clean the list was
// built with.
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
	// Sorted so the same request always produces the same message.
	sort.Strings(left)
	return nil, errors.New("not a folder this instance writes into: " + strings.Join(left, ", "))
}

// ownerIdentity is the GET answer: what is in force beside what was asked
// for, so a PUID that did nothing is visible at a glance.
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
	// Left empty when unknown, since "0000" is a real and alarming mask.
	if me.UmaskKnown {
		id.Umask = fileowner.Octal(uint32(me.Umask))
	}
	return id
}

// ownershipDiagnostics is the read-only half for the diagnostics bundle: one
// stat per configured folder, no probe.
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
