package app

// The working folder: downloads are written there while they arrive and moved
// to their destination once nothing more is owed on them, after the checksum
// and after extraction, so a multi-volume set stays together while it is
// unpacked. An empty settings.WorkDir switches all of this off.
//
// The working folder is derived from the destination, so dirFor is right both
// before and after the move and nothing about the task changes.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// deliverErrorPrefix marks the errors this file puts on a task, so a later
// successful move clears only its own failure.
const deliverErrorPrefix = "not moved: "

// orphanWorkAge is how long a working folder no task points at survives. A day
// is generous because sweeping too early deletes downloaded bytes, while
// sweeping late only costs disk space.
const orphanWorkAge = 24 * time.Hour

// workRoot is the folder downloads are written into while they arrive, or ""
// when they are written straight to their destination.
func (a *App) workRoot() string { return strings.TrimSpace(a.Settings.Get().WorkDir) }

// workDirFor is where t's bytes currently sit. Anything that opens, hashes,
// unpacks or deletes the file must use this; dirFor answers where the file
// belongs.
func (a *App) workDirFor(t *core.Task) string {
	dest := a.dirFor(t)
	root := a.workRoot()
	if root == "" || !deliverable(t) {
		return dest
	}
	return workdir.For(root, dest)
}

// stagedDirFor is the working folder for a backend, or "" when the download is
// written to its destination. engine.Job skips the collision policy when
// WorkDir is set, so it must stay empty rather than repeat Dir.
func (a *App) stagedDirFor(t *core.Task) string {
	if work := a.workDirFor(t); work != a.dirFor(t) {
		return work
	}
	return ""
}

// deliverable reports whether this app can move t's finished file itself. A
// download fetched on another machine has no local file. A multi-file torrent
// writes into a folder named after the torrent, which the task does not know,
// so torrents are written to their destination directly.
func deliverable(t *core.Task) bool {
	return t != nil && filesAreLocal(t) && t.InfoHash == ""
}

// moveOptions returns the collision policy for a move: the download's own,
// with the task's category rule applied. It is read again here rather than
// passed down, because the category may have changed since the download
// started. A nil task, whose row was removed during extraction, gets the
// instance policy.
func moveOptions(t *core.Task, cfg settings.Settings) workdir.Options {
	category := ""
	if t != nil {
		category = t.Category
	}
	return workdir.Options{
		Policy:      collide.ParsePolicy(cfg.CollisionFor(category)),
		MaxAttempts: cfg.CollisionMaxAttempts,
		// Emptied working folders are removed so they do not pile up.
		PruneSourceDir: true,
	}
}

// deliverDownload moves one finished download from the working folder to its
// destination. It does nothing when no working folder is set, the task still
// owes work, or the file is not there, which is an ordinary state (already
// moved, removed by hand, or not deliverable).
func (a *App) deliverDownload(id string) {
	if a.workRoot() == "" {
		return
	}
	a.mu.Lock()
	t := a.tasks[id]
	// StatusDone only: a task in StatusExtracting still needs its volumes
	// together.
	if t == nil || t.Status != core.StatusDone || !deliverable(t) || t.Name == "" || t.Name == t.URL {
		a.mu.Unlock()
		return
	}
	dest, work := a.dirFor(t), a.workDirFor(t)
	src := a.fileOfLocked(t)
	c := *t
	a.mu.Unlock()
	// Only what sits in the working folder is this move's to make.
	if dest == work || !sameDir(filepath.Dir(src), work) {
		return
	}
	if _, err := os.Lstat(src); err != nil {
		return
	}
	res, err := workdir.Move(a.ctx, src, dest, moveOptions(&c, a.Settings.Get()))
	a.recordDelivery(id, src, res.Path, err)
}

// recordDelivery puts a failed move on the task, or clears an earlier one when
// the move worked. It goes on the row because the file is not where the list
// says it is. A task that recorded its file follows it from src to where it
// ended up, also when src was the file renamed back to the task's own name
// (see fileOfLocked).
func (a *App) recordDelivery(id, src, moved string, err error) {
	if err != nil {
		log.Printf("task %s could not be moved out of the working folder: %v", id, err)
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	named, _ := namedBeside(t)
	followed := err == nil && (samePath(t.File, src) || samePath(named, src)) && !samePath(moved, src)
	if followed {
		t.File = moved
	}
	switch {
	case err != nil:
		t.Error = deliverErrorPrefix + err.Error()
	case strings.HasPrefix(t.Error, deliverErrorPrefix):
		// Only this file's own error; an extraction failure stays.
		t.Error = ""
	case !followed:
		a.mu.Unlock()
		return
	}
	c := *t
	a.mu.Unlock()
	a.saveAndBroadcast([]core.Task{c})
}

// unpackPlan is where an extraction writes and where its result goes
// afterwards, so a media folder never holds a half-unpacked release.
type unpackPlan struct {
	// Dest and Subfolder are extract.Options' fields of the same names.
	Dest      string
	Subfolder bool
	// Deliver is where the finished result is moved; empty leaves it where it
	// unpacked.
	Deliver string
	// Contents moves the entries inside the unpacked folder rather than the
	// folder itself (see rules.Action.ExtractDir).
	Contents bool
}

// unpackPlanFor decides both halves for one task:
//
//	no working folder, no move folder   unpack where it always unpacked, move nothing
//	working folder                      unpack in the working folder, move the finished
//	                                    folder to where it would have unpacked
//	move folder (rule or setting)       move the unpacked content there instead, whether
//	                                    or not a working folder is in play
//
// With a working folder the per-package level is built into the path here, so
// the result moves to the folder it would have unpacked into; Subfolder is then
// switched off so the level is not applied twice.
func (a *App) unpackPlanFor(t *core.Task, cfg settings.Settings) unpackPlan {
	p := unpackPlan{Dest: a.expandFolder(t, cfg.ExtractTo), Subfolder: cfg.ExtractSubfolder}
	root := a.workRoot()
	if root != "" && deliverable(t) {
		if base := unpackRoot(p.Dest, t, cfg.ExtractSubfolder); base != "" {
			p.Dest, p.Subfolder, p.Deliver = workdir.For(root, base), false, base
		} else {
			// Unpacking beside the archive already lands in the working folder;
			// the result moves to the archive's own destination.
			p.Deliver = a.dirFor(t)
		}
	}
	if move := a.extractMoveTarget(t, cfg); move != "" {
		p.Deliver, p.Contents = move, true
	}
	return p
}

// unpackRoot is the folder an extraction's output folder is created in, with
// the per-package level applied. Empty means beside the archive.
func unpackRoot(dest string, t *core.Task, subfolder bool) string {
	if dest == "" {
		return ""
	}
	if subfolder && t != nil {
		// collide.SafeName, to match the path extract.Options builds itself.
		if pkg := collide.SafeName(strings.TrimSpace(t.Package)); pkg != "" {
			return filepath.Join(dest, pkg)
		}
	}
	return dest
}

// extractMoveTarget is where t's unpacked content moves after extraction: a
// folder a Packagizer rule set on the task, else the instance setting expanded
// for this task.
func (a *App) extractMoveTarget(t *core.Task, cfg settings.Settings) string {
	// The rules package already expanded it; a relative path would name a
	// folder nobody could find.
	if t != nil {
		if own := strings.TrimSpace(t.ExtractDir); filepath.IsAbs(own) {
			return own
		}
	}
	return a.expandFolder(t, cfg.ExtractMoveTo)
}

// expandFolder resolves a folder template for one task and drops the result
// unless it is absolute, rather than resolve it against the working directory.
func (a *App) expandFolder(t *core.Task, template string) string {
	out := strings.TrimSpace(template)
	if out == "" {
		return ""
	}
	if t != nil && pathvars.HasVars(out) {
		out = pathvars.Expand(out, pathvars.Vars{
			Package: t.Package,
			Host:    hostOf(t.URL),
			Name:    t.Name,
			Date:    t.CreatedAt,
		})
	}
	if !filepath.IsAbs(out) {
		return ""
	}
	return out
}

// delivery is what the last move did, as the extraction log shows it.
type delivery struct {
	// Dir is where the content ended up, or empty when nothing moved.
	Dir string
	// Entries counts what was moved: one for a whole folder, or one per entry
	// when the contents were moved.
	Entries int
	Err     error
}

// deliverExtraction moves a finished extraction's output where the settings or
// a rule say. It runs before the job settles, so the row shows the final
// location.
func (a *App) deliverExtraction(jobID string, out *extract.Outcome) delivery {
	if out == nil || strings.TrimSpace(out.Dir) == "" {
		return delivery{}
	}
	a.mu.Lock()
	j := a.unpackLocked().jobs[jobID]
	if j == nil {
		a.mu.Unlock()
		return delivery{}
	}
	archive := j.path
	var t *core.Task
	if live := a.tasks[j.TaskID]; live != nil {
		c := *live
		t = &c
	}
	a.mu.Unlock()

	cfg := a.Settings.Get()
	plan := a.unpackPlanFor(t, cfg)
	if plan.Deliver == "" || sameDir(out.Dir, plan.Deliver) {
		return delivery{}
	}
	// A single compressed stream ("dump.sql.gz") unpacks beside its archive, so
	// out.Dir is a shared folder and moving it would take everything else too.
	if sameDir(out.Dir, filepath.Dir(archive)) {
		return delivery{Err: fmt.Errorf("%s unpacked beside its own archive rather than into a folder of its own, so its content was left there instead of being moved to %s", filepath.Base(archive), plan.Deliver)}
	}
	o := moveOptions(t, cfg)
	if plan.Contents {
		rep, err := workdir.MoveContents(a.ctx, out.Dir, plan.Deliver, o)
		d := delivery{Entries: rep.Moved, Err: err}
		if rep.Moved > 0 {
			d.Dir = plan.Deliver
		}
		// Skipped files are reported so the row accounts for the ones left
		// behind.
		if err == nil && rep.Skipped > 0 {
			d.Err = fmt.Errorf("%d of the unpacked files were already in %s and the collision policy is to skip, so they were left in %s", rep.Skipped, plan.Deliver, out.Dir)
		}
		return d
	}
	if sameDir(filepath.Dir(out.Dir), plan.Deliver) {
		return delivery{}
	}
	res, err := workdir.Move(a.ctx, out.Dir, plan.Deliver, o)
	if err != nil {
		return delivery{Err: err}
	}
	if res.Skipped {
		return delivery{Err: fmt.Errorf("%s already exists and the collision policy is to skip, so the unpacked content was left in %s", filepath.Join(plan.Deliver, filepath.Base(out.Dir)), out.Dir)}
	}
	return delivery{Dir: res.Path, Entries: 1}
}

// deliverVolumes moves what is left of an archive's set out of the working
// folder after extraction: volumes a "keep" disposal left and the info files
// beside them.
func (a *App) deliverVolumes(taskID string) {
	if a.workRoot() == "" {
		return
	}
	a.mu.Lock()
	var ids []string
	if t := a.tasks[taskID]; t != nil {
		for _, part := range a.volumeSetLocked(t) {
			ids = append(ids, part.ID)
		}
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.deliverDownload(id)
	}
}

// sweepWorkRoot removes working folders left behind when a task went away while
// its download was in flight. Folders a task still points at, and anything
// written to in the last day, are kept (see workdir.Sweep).
func (a *App) sweepWorkRoot() {
	root := a.workRoot()
	if root == "" {
		return
	}
	live := a.liveWorkKeys()
	n, err := workdir.Sweep(root, func(key string) bool { return live[key] }, orphanWorkAge)
	if err != nil {
		log.Printf("the working folder could not be swept: %v", err)
		return
	}
	if n > 0 {
		log.Printf("removed %d working folder(s) no download points at any more", n)
	}
}

// liveWorkKeys returns every working folder a task still needs, derived from
// current destinations. A folder from an older destination is left to the age
// check.
func (a *App) liveWorkKeys() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]bool, len(a.tasks))
	for _, t := range a.tasks {
		out[workdir.Key(a.dirFor(t))] = true
	}
	return out
}

// sameDir reports whether two folder paths are the same once cleaned.
func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
