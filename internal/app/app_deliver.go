package app

// The working folder and the last move: where a download's bytes are written
// while they are still arriving, and how the finished result gets from there to
// the folder it belongs in.
//
// The off state is an empty settings.WorkDir, it is what every install has
// until somebody types a path, and off means every path in this file answers
// exactly what it answered before the file existed. That is not caution for its
// own sake: switching it on can mean copying every download across a filesystem
// boundary, and nobody may be handed that by an update they did not read.
//
// THE ORDER IS THE POINT. A download is delivered when nothing is owed on it
// any more - after the checksum has read it where it was written, and after the
// extraction has opened it there together with its four sibling volumes.
// Delivering earlier would move the first volume of a five-part set out from
// under the four still arriving, and the set would stop being a set.
//
// Nothing about the task changes when a delivery succeeds, and that is the
// point of keying the working folder off the DESTINATION rather than off the
// task: dirFor already answers where the finished file is, so the list, the
// store and every connected browser are right both before and after the move
// without being told anything.

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

// deliverErrorPrefix marks the sentences this file puts on a task, so a later
// delivery that works can clear its own failure and nothing else's - the same
// arrangement extractErrorPrefix already has, and for the same reason: a rename
// that was refused and a move that ran out of space are two problems, and one
// being solved says nothing about the other.
const deliverErrorPrefix = "not moved: "

// orphanWorkAge is how long a working folder nothing points at survives.
//
// A day, and generously so, because the cost of the two mistakes is not the
// same. Sweeping too eagerly deletes bytes somebody paid for in bandwidth;
// sweeping too late leaves a folder on a disk that has room for it. The only
// folders that reach the age check at all are ones no task in the list claims
// any more - somebody removed the download while it was running - so a day is
// spent on a folder that is already known to be nobody's.
const orphanWorkAge = 24 * time.Hour

// workRoot is the folder downloads are written into while they are still
// arriving, or "" when every download is written straight to its destination.
func (a *App) workRoot() string { return strings.TrimSpace(a.Settings.Get().WorkDir) }

// workDirFor is where this task's bytes actually sit, which is not always the
// folder the finished file belongs in.
//
// Every path in this app that opens, hashes, unpacks or disposes of a file has
// to be built from this and not from dirFor; dirFor stays the answer to "where
// does this file belong", which is a different question and the one the
// interface, the store and the collision policy all ask.
func (a *App) workDirFor(t *core.Task) string {
	dest := a.dirFor(t)
	root := a.workRoot()
	if root == "" || !deliverable(t) {
		return dest
	}
	return workdir.For(root, dest)
}

// stagedDirFor is workDirFor as a BACKEND takes it: the working folder when one
// is in play, and the empty string when the download is written straight to its
// destination.
//
// The difference from workDirFor is not cosmetic, and it is the sort that
// compiles. engine.Job reads a WorkDir that is set as "the folder I am writing
// into is not the folder this file belongs in", and stops applying the
// collision policy it was handed because that policy belongs at the
// destination. A job whose WorkDir merely repeated its Dir would say the same
// thing untruthfully, and every download on an install with no working folder
// would quietly lose its rename, its skip and its overwrite.
func (a *App) stagedDirFor(t *core.Task) string {
	if work := a.workDirFor(t); work != a.dirFor(t) {
		return work
	}
	return ""
}

// deliverable reports whether this app can take a task's finished file the last
// step on its own. Two kinds cannot, for opposite reasons.
//
// A download fetched on another machine (filesAreLocal) has no file here to
// move, and reaching for one would be the same mistake as renaming or deleting
// it.
//
// A TORRENT is the interesting one, and it is a carve-out rather than an
// oversight. A multi-file torrent writes a folder named after the torrent and
// puts its files inside it, while the task is named after the FIRST FILE - so
// what is on disk is a folder whose name this side never holds, and a move
// built from the task's own name would take one episode out of a season and
// leave the rest behind. Until the torrent's own folder name is carried on the
// task, a torrent keeps writing straight to its destination, which is what it
// did before this feature existed.
func deliverable(t *core.Task) bool {
	return t != nil && filesAreLocal(t) && t.InfoHash == ""
}

// moveOptions is the collision policy as a move reads it. It is the download's
// own policy and not a second setting, because the question is the same one:
// something is already at that name in the folder the file is going into. What
// the move does about "ask" and about a folder under "overwrite" is
// internal/workdir's business - see Options.Policy there.
func moveOptions(cfg settings.Settings) workdir.Options {
	return workdir.Options{
		Policy:      collide.ParsePolicy(cfg.CollisionPolicy),
		MaxAttempts: cfg.CollisionMaxAttempts,
		// The working folder is emptied as it is drained. It is ours, it holds
		// nothing but downloads in flight, and a folder per destination left
		// standing for every destination ever used is a directory listing
		// nobody can read after a year.
		PruneSourceDir: true,
	}
}

// deliverDownload takes one finished download out of the working folder and
// puts it in the folder it belongs in. It does nothing at all when no working
// folder is configured, when the file is not there, or when the task still has
// something owed on it.
//
// A file that is not where this expects it is not a failure and says nothing:
// it has already been delivered, or it was removed by hand, or it is one of the
// kinds deliverable refuses. Any of the three is an ordinary state and none of
// them is worth a red mark on a download that finished.
func (a *App) deliverDownload(id string) {
	if a.workRoot() == "" {
		return
	}
	a.mu.Lock()
	t := a.tasks[id]
	// StatusDone and not merely "finished once": a task that has moved on to
	// StatusExtracting still owes an unpacking, and the archive it is about to
	// open must stay where its sibling volumes are.
	if t == nil || t.Status != core.StatusDone || !deliverable(t) || t.Name == "" || t.Name == t.URL {
		a.mu.Unlock()
		return
	}
	dest, work := a.dirFor(t), a.workDirFor(t)
	c := *t
	a.mu.Unlock()
	if dest == work {
		return
	}
	src := filepath.Join(work, c.Name)
	if _, err := os.Lstat(src); err != nil {
		return
	}
	_, err := workdir.Move(a.ctx, src, dest, moveOptions(a.Settings.Get()))
	a.recordDelivery(id, err)
}

// recordDelivery puts a failed move on the task and takes an old one back off
// when the move worked.
//
// The failure belongs on the row rather than only in the log, because the file
// is not where the list says it is: it is still in the working folder, whole
// and openable, and the person who has to empty a disk is the person looking at
// that row.
func (a *App) recordDelivery(id string, err error) {
	if err != nil {
		log.Printf("task %s could not be moved out of the working folder: %v", id, err)
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	switch {
	case err != nil:
		t.Error = deliverErrorPrefix + err.Error()
	case strings.HasPrefix(t.Error, deliverErrorPrefix):
		// Only this file's own sentence. An extraction that failed left its
		// reason on the same field, and a move that worked is no reason to tell
		// the user that problem went away.
		t.Error = ""
	default:
		a.mu.Unlock()
		return
	}
	c := *t
	a.mu.Unlock()
	a.saveAndBroadcast([]core.Task{c})
}

// unpackPlan is where an extraction writes and where its result goes
// afterwards. The two are separate questions, and conflating them is what makes
// a media folder hold a half-unpacked release for twenty minutes.
type unpackPlan struct {
	// Dest and Subfolder are extract.Options' own two fields, verbatim.
	Dest      string
	Subfolder bool
	// Deliver is where the finished result is moved. Empty leaves it exactly
	// where it unpacked, which is what every install did before either half of
	// this existed.
	Deliver string
	// Contents moves the entries INSIDE the unpacked folder rather than the
	// folder itself - see rules.Action.ExtractDir for why that is the useful
	// reading of "put the unpacked files here".
	Contents bool
}

// unpackPlanFor works out both halves for one task.
//
// The three cases, in the order they are decided:
//
//	no working folder, no move folder   unpack where it always unpacked, move nothing
//	working folder                      unpack in the working folder, move the finished
//	                                    folder to where it would have unpacked
//	move folder (rule or setting)       move the unpacked CONTENT there instead, whether
//	                                    or not a working folder is in play
//
// The middle case has to fold ExtractSubfolder into the destination itself
// rather than leaving it to extract.Options, and that is the one subtlety here.
// The middle case has to ask for the per-package level itself rather than hand
// ExtractSubfolder on unchanged, and THAT is the load-bearing part. The folder a
// release is moved to has to be the one it would have unpacked into, package
// level and all: aimed at the collect folder instead, "Serien/The Show/release"
// would arrive as "Serien/release" and the level would be gone for exactly the
// installs that use a working folder. Switching the flag off afterwards is only
// tidiness - the level is already in the path, and applying it twice would leave
// an empty folder in the working folder rather than change where anything lands.
func (a *App) unpackPlanFor(t *core.Task, cfg settings.Settings) unpackPlan {
	p := unpackPlan{Dest: a.expandFolder(t, cfg.ExtractTo), Subfolder: cfg.ExtractSubfolder}
	root := a.workRoot()
	if root != "" && deliverable(t) {
		if base := unpackRoot(p.Dest, t, cfg.ExtractSubfolder); base != "" {
			p.Dest, p.Subfolder, p.Deliver = workdir.For(root, base), false, base
		} else {
			// Beside the archive, which IS the working folder: the archive was
			// written there, so the extraction already lands there and there is
			// nothing to redirect. What it has to be moved to afterwards is the
			// folder the archive itself belongs in.
			p.Deliver = a.dirFor(t)
		}
	}
	if move := a.extractMoveTarget(t, cfg); move != "" {
		p.Deliver, p.Contents = move, true
	}
	return p
}

// unpackRoot is the folder an extraction's own output folder is created in,
// with the per-package level already applied. Empty means "beside the archive",
// which has no folder of its own to name.
func unpackRoot(dest string, t *core.Task, subfolder bool) string {
	if dest == "" {
		return ""
	}
	if subfolder && t != nil {
		// collide.SafeName and not the app's own sanitizeSegment, because this
		// has to agree with extract.Options.baseDest, which is what builds the
		// same path when no working folder is in play.
		if pkg := collide.SafeName(strings.TrimSpace(t.Package)); pkg != "" {
			return filepath.Join(dest, pkg)
		}
	}
	return dest
}

// extractMoveTarget is where this task's unpacked content is moved to once the
// extraction is over: the instance-wide setting, expanded for this task.
//
// THE PER-LINK ANSWER BELONGS IN FRONT OF IT and is not wired yet. A Packagizer
// rule can already name the folder (rules.Action.ExtractDir, validated and
// expanded there), and reading it here is one line - the same shape
// extractWanted uses to read Task.AutoExtract in front of Settings.Extract. It
// needs a field on core.Task to travel on, which this wave did not add.
func (a *App) extractMoveTarget(t *core.Task, cfg settings.Settings) string {
	return a.expandFolder(t, cfg.ExtractMoveTo)
}

// expandFolder resolves a folder template for one task, and drops anything that
// is not an absolute path afterwards.
//
// It is here rather than in internal/extract for the reason extractOptionsFor
// already gives: this is the only place that knows which task the variables are
// about, and internal/extract never sees a task. A template that expanded to
// something relative is dropped rather than resolved against whatever the
// process's working directory happens to be - the folder it would then name is
// one nobody can find afterwards.
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

// delivery is what the last move did, in the terms the extraction log shows it.
type delivery struct {
	// Dir is where the content ended up, and it is what the job row reports. It
	// is empty when nothing moved, which is not the same as a failure.
	Dir string
	// Entries counts what was moved: one for a whole folder, or one per file
	// when the content was moved rather than the folder around it.
	Entries int
	Err     error
}

// deliverExtraction moves a finished extraction's output to where the settings
// or a rule say it goes.
//
// It runs after the disposal and before the job is settled, so the row the user
// ends up looking at already says where the files are rather than where they
// were unpacked.
func (a *App) deliverExtraction(jobID string, out *extract.Outcome) delivery {
	if out == nil || strings.TrimSpace(out.Dir) == "" {
		return delivery{}
	}
	// The job carries the archive it was started on and the task it belongs to,
	// and both are read here rather than passed down through settleExtraction:
	// the job is the record of what this extraction was, and a second copy
	// threaded through three signatures is a second one to get wrong.
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
	// A SINGLE COMPRESSED STREAM HAS NO FOLDER OF ITS OWN. "dump.sql.gz"
	// unpacks to "dump.sql" BESIDE the archive, so out.Dir is the folder the
	// archive is in - which for a working folder holds other downloads in
	// flight and for a download folder holds everything the user owns. Moving
	// it, or its contents, would take all of that with it. The refusal is
	// reported rather than silent, because the alternative is a setting that
	// visibly does nothing for one format.
	if sameDir(out.Dir, filepath.Dir(archive)) {
		return delivery{Err: fmt.Errorf("%s unpacked beside its own archive rather than into a folder of its own, so its content was left there instead of being moved to %s", filepath.Base(archive), plan.Deliver)}
	}
	o := moveOptions(cfg)
	if plan.Contents {
		rep, err := workdir.MoveContents(a.ctx, out.Dir, plan.Deliver, o)
		d := delivery{Entries: rep.Moved, Err: err}
		if rep.Moved > 0 {
			d.Dir = plan.Deliver
		}
		// A skip is a decision the user made and not a failure, but it is still
		// the answer to "where are my files", so it is said out loud. Reporting
		// only the folder they were moved to would leave the ones that stayed
		// behind unaccounted for on the one row that is about them.
		if err == nil && rep.Skipped > 0 {
			d.Err = fmt.Errorf("%d of the unpacked files were already in %s and the collision policy is to skip, so they were left in %s", rep.Skipped, plan.Deliver, out.Dir)
		}
		return d
	}
	if sameDir(filepath.Dir(out.Dir), plan.Deliver) {
		return delivery{} // already in the folder it was going to be moved into
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

// deliverVolumes takes what is left of an archive's own set out of the working
// folder: the volumes a "keep" disposal left standing, and the info files
// beside them.
//
// It runs after the extraction and not with the download, which is the whole
// ordering this file exists for - the four sibling volumes have to still be
// where the reader can find them while the first one is being opened.
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

// sweepWorkRoot removes working folders that belong to nothing any more.
//
// What it is for is a crash, and what it is NOT for is tidying up after a
// normal run: a working folder is emptied and removed as its downloads are
// delivered, so anything this finds is a folder whose task went away while its
// download was in flight. Everything a task still points at is protected by
// name, and everything written to in the last day is protected by age - see
// workdir.Sweep, which is deliberately the only thing in this build that
// deletes a download nobody asked it to.
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

// liveWorkKeys is every working folder something in the list still needs.
//
// It is built from the destinations rather than from the folders on disk, which
// is what makes it safe to be wrong about: a task whose destination has since
// been changed protects the folder it would use NOW, and the one it used before
// is swept by age. The other way round - listing the disk and asking which
// folders look busy - would have to guess.
func (a *App) liveWorkKeys() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]bool, len(a.tasks))
	for _, t := range a.tasks {
		out[workdir.Key(a.dirFor(t))] = true
	}
	return out
}

// sameDir compares two folder paths the way the filesystem would rather than
// the way the strings do.
func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
