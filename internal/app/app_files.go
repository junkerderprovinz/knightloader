package app

// Reaching a task's own file on disk: one safe path, shared by the streaming
// route (internal/api/routes_files.go) and the desktop-only reveal/open
// bindings (desktop/files.go). Neither of those may re-derive the path on its
// own - this is the whole security check package 20 exists for, and a second
// implementation is a second place for it to be gotten right the first time
// and wrong the second.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// envBrowseRoots is the same variable internal/api/routes_folders.go reads
// under the same name, so an operator who has already narrowed the folder
// chooser with it gets the identical narrowing here rather than a second,
// differently-scoped setting to discover. See fileServeRoots below for what
// this package does differently when it is unset.
const envBrowseRoots = "KL_BROWSE_ROOTS"

// The outcomes SafeTaskFile can refuse with. A caller branches on these with
// errors.Is rather than parsing the message, because the HTTP route and the
// desktop bindings each report a refusal in their own way (a status code, a
// rejected JS promise) and both need to tell "no such task" apart from "that
// would leave the folder".
var (
	// ErrTaskFileNotFound is no task by that id.
	ErrTaskFileNotFound = errors.New("no such task")
	// ErrTaskFileNotLocal is a task whose bytes live in another process's
	// filesystem - today that is exactly the JD backend (see filesAreLocal).
	// Nothing here can vouch for a path this app never wrote to.
	ErrTaskFileNotLocal = errors.New("this task's file was not downloaded by this app, so it cannot be reached from here")
	// ErrTaskFileNoBytes is a task with nothing to open yet: still in the
	// collector, not yet resolved to a real name, or resolved but not a
	// single byte written. Distinct from a security refusal on purpose - a
	// link that has not started is not a break-in attempt.
	ErrTaskFileNoBytes = errors.New("nothing has been downloaded yet")
	// ErrTaskFileEscape is the one this whole package exists to return: the
	// task's own Dir and stored name, joined and resolved, land outside the
	// folder the task itself is supposed to be in.
	ErrTaskFileEscape = errors.New("refused: this task's file does not resolve inside its own download folder")
)

// TaskFile is a task's file exactly as it sits on disk right now: Path has
// already been re-derived from the task's own fields, joined the way every
// other part of the app joins them, and checked after symlink resolution -
// open exactly this path and nothing else.
type TaskFile struct {
	// Name is the task's own file name, for a Content-Disposition header - not
	// necessarily the last segment of Path, which is real and resolved.
	Name string
	// Path is safe to open directly: it has already been confirmed to resolve
	// inside the task's own download folder.
	Path string
	// Size is the byte count Path actually has right now, from the same Stat
	// that confirmed Path exists. A running task's file is still growing, so
	// this is a snapshot, not the task's own expected total.
	Size int64
}

// SafeTaskFile re-derives where one task's file lives, the same way dirFor
// already does for every other part of the app, and refuses rather than opens
// whatever the join produced when it does not check out.
//
// THE CHECK, in order:
//
//  1. filesAreLocal - a task fetched through the JD sidecar has no file here
//     to reach at all, local desktop provisioning included: see filesAreLocal's
//     own doc for why that is not narrowed per build.
//  2. the stored name has to be a real, resolved, single-segment name -
//     usableFilename is the same rule a rename already enforces, because a
//     name is not a name once it carries a way out of the folder.
//  3. dirFor(t) itself - not just the joined path - has to resolve inside
//     fileServeRoots' boundary AFTER filepath.EvalSymlinks: t.Dir is a
//     client-supplied per-task override with no validation of its own (see
//     SetTaskOptions/AddLinks), so without this step a Dir set to any
//     directory this process can read serves whatever file by that name
//     lives there, including this app's own settings.json or database.
//  4. dirFor(t) joined with that name has to still be under dirFor(t) AFTER
//     filepath.EvalSymlinks on both sides - a task whose folder holds a
//     symlink planted some other way resolves to somewhere this check
//     catches rather than somewhere a client downloads. (join(dir, name)
//     with a single-segment name is inside dir by construction absent a
//     symlink, so this step is what a symlink specifically defeats and step
//     3 does not.)
//
// A task not yet reachable (still collected, nothing on disk yet) answers
// ErrTaskFileNoBytes rather than ErrTaskFileEscape: the difference matters to
// every caller, because one is an ordinary "not yet" and the other is a
// refusal to open a specific book.
func (a *App) SafeTaskFile(id string) (TaskFile, error) {
	a.mu.Lock()
	t := a.tasks[id]
	var snap core.Task
	if t != nil {
		snap = *t
	}
	a.mu.Unlock()
	if t == nil {
		return TaskFile{}, ErrTaskFileNotFound
	}
	if !filesAreLocal(&snap) {
		return TaskFile{}, ErrTaskFileNotLocal
	}
	name := filename(&snap)
	if name == "" {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	if !usableFilename(name) {
		// A resolver handing back a name with a separator in it, or a store row
		// edited by hand, both land here - refused rather than joined, same as
		// SetTaskOptions already refuses it as a rename target.
		return TaskFile{}, ErrTaskFileEscape
	}

	dir := a.dirFor(&snap)
	full := filepath.Join(dir, name)

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		// The folder itself does not exist yet - a resolved name with nothing
		// written under it, which is "not yet" and not a break-in attempt.
		return TaskFile{}, ErrTaskFileNoBytes
	}
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	// realFull is join(realDir, name) with name already forced single-segment
	// by usableFilename above, so it is ALWAYS inside realDir by construction
	// - checking it against realDir alone proves nothing. What actually needs
	// checking is realDir itself: t.Dir is a client-supplied field (a per-task
	// override, set via POST /api/tasks/options or the "dir" field on
	// POST /api/links, trimmed but never validated - see SetTaskOptions and
	// AddLinks) and dirFor returns it verbatim when set. Without this, any
	// caller who can set a task's Dir can point this route at any file the
	// process can read, name included, regardless of where downloads
	// actually belong.
	roots, err := a.fileServeRoots(realDir)
	if err != nil {
		return TaskFile{}, ErrTaskFileEscape
	}
	inRoots := false
	for _, root := range roots {
		if withinDir(root, realDir) {
			inRoots = true
			break
		}
	}
	if !inRoots || !withinDir(realDir, realFull) {
		return TaskFile{}, ErrTaskFileEscape
	}

	fi, err := os.Stat(realFull)
	if err != nil || fi.IsDir() {
		return TaskFile{}, ErrTaskFileNoBytes
	}
	return TaskFile{Name: name, Path: realFull, Size: fi.Size()}, nil
}

// withinDir reports whether p is dir or sits below it, once both are already
// resolved. filepath.Rel does the comparison rather than strings.HasPrefix,
// because on Windows a path comparison has to be case-insensitive and
// HasPrefix is not - the same reason internal/api's own folder chooser
// (routes_folders.go's within) uses it. That function lives on the other side
// of a one-way import (internal/api already imports internal/app, so this
// package may never import internal/api back), which is the only reason this
// is not simply a call to it.
func withinDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// fileServeRoots is the boundary a task's Dir must resolve inside before
// SafeTaskFile will open anything in it.
//
// When KL_BROWSE_ROOTS is set, this is exactly what
// internal/api/routes_folders.go's own browseRoots answers for the same
// variable - an operator's deliberate narrowing, so a per-task Dir the
// folder chooser could have offered is never refused here either.
//
// When it is UNSET, this deliberately does NOT fall back to "the whole
// filesystem" the way routes_folders.go's own default does. That default is
// reasonable for a route that only lists directory NAMES ("a chooser that
// cannot reach [wherever the user mounted their disk] is one they type
// around" - that file's own header comment); it is not reasonable for a
// route that streams file BYTES. Falling back to it here would still let a
// crafted Dir reach this instance's own settings.json or database - both
// sit on the very same volume as any real download inside a container, so
// "the whole filesystem this process can see" does not actually exclude
// them. The fallback boundary is instead this app's own configured download
// tree (defaultDir, fixed-prefix-stripped the same way Validate already
// does for a templated DownloadDir) - the one place a task's bytes are
// expected to live absent an operator's explicit say-so otherwise.
func (a *App) fileServeRoots(p string) ([]string, error) {
	set := strings.TrimSpace(os.Getenv(envBrowseRoots))
	if set == "" {
		root := fixedPathPrefix(a.defaultDir())
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			// The configured download tree does not exist yet - refuse rather
			// than fall back to something wider just because setup is incomplete.
			return nil, err
		}
		return []string{real}, nil
	}
	var out []string
	for _, part := range filepath.SplitList(set) {
		part = strings.TrimSpace(part)
		if part == "" || !filepath.IsAbs(part) {
			continue
		}
		if real, err := filepath.EvalSymlinks(part); err == nil {
			out = append(out, filepath.Clean(real))
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	if len(out) == 0 {
		return nil, errors.New(envBrowseRoots + " is set but names no absolute folder, so nothing may be reached")
	}
	return out, nil
}

// fixedPathPrefix is internal/settings's own unexported fixedPrefix, ported
// for the one-way-import reason every other port in this file already is:
// the leading segments of a folder template that hold no <jd:...>
// placeholder, which is the deepest directory that is the same for every
// task and therefore the one that actually exists on disk to be checked.
func fixedPathPrefix(dir string) string {
	if !strings.Contains(dir, "<") {
		return dir
	}
	sep := string(filepath.Separator)
	parts := strings.Split(strings.ReplaceAll(dir, "/", sep), sep)
	var keep []string
	for _, p := range parts {
		if strings.Contains(p, "<") {
			break
		}
		keep = append(keep, p)
	}
	if out := strings.Join(keep, sep); out != "" {
		return out
	}
	return sep
}
