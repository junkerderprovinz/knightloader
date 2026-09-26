package app

// A renamed package's folder. Where the download folder is built from the
// package name, the folder takes the new name on disk together with everything
// already in it: finished files, partial ones and their share of the working
// folder. What writes into it is stopped while it moves and carries on from
// the new place. The package's extraction folder follows the name as well.

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// packageFolder is one folder a package rename affects: from is the folder the
// package's links download to, to the one the new name gives them, and tasks
// every task whose files are in from.
type packageFolder struct {
	from, to string
	tasks    []*core.Task
	// written is whether one of them has put a file there or been handed to a
	// backend that is about to. Nothing moves on disk otherwise.
	written bool
	// stays is a folder that keeps its name because the new name is taken and
	// the collision policy is to skip.
	stays bool
	// landed is where the folder goes, which is not to when the collision
	// policy counted the name up.
	landed string
}

// moves reports whether the folder is moved on disk.
func (f *packageFolder) moves() bool { return f.written && !f.stays }

// folderMove is one folder going to another name.
type folderMove struct{ from, to string }

// renamePlan is one package rename as RenamePackage settles it under a.mu.
type renamePlan struct {
	members []*core.Task
	name    string
	folders []*packageFolder
	// occupied holds the folder of every task with files, so a name a live
	// package writes into counts as taken although nothing may be there on
	// disk yet: its files can all still be in the working folder.
	occupied map[string]bool
	// unpacked is where the members' extraction folders go, when those are
	// named after the package (settings.ExtractSubfolder or a template).
	unpacked []folderMove
	// policy is the collision rule for a taken folder name, and moveOpts how
	// unpacked content moves, both from the members' category.
	policy   collide.Policy
	moveOpts workdir.Options
}

// RenamePackage gives the named tasks, and every task sharing one of their
// links (sharingLinksLocked), a new package name. Where the download folder is
// built from the package name, the folder follows it on disk with everything
// already downloaded into it, and the transfers writing there carry on in the
// new folder.
func (a *App) RenamePackage(ids []string, name string) ([]string, error) {
	name, err := checkName("package", name)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	p, err := a.planRenameLocked(ids, name)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	moving := false
	for _, f := range p.folders {
		if !f.moves() {
			continue
		}
		moving = true
		if a.relocating == nil {
			a.relocating = map[string]bool{}
		}
		for _, t := range f.tasks {
			a.relocating[t.ID] = true
		}
	}
	if !moving {
		copies := a.applyRenameLocked(p, nil)
		a.mu.Unlock()
		a.publishTasks(copies)
		a.followJobs(a.moveUnpacked(p), p)
		return idsOf(p.members), nil
	}
	a.mu.Unlock()

	if err := a.relocate(p); err != nil {
		return nil, err
	}
	return idsOf(p.members), nil
}

// planRenameLocked settles what renaming the package of ids to name touches.
// The rename is turned down while something it would move is being unpacked,
// moved into place or recorded from a live stream. Caller holds a.mu.
func (a *App) planRenameLocked(ids []string, name string) (*renamePlan, error) {
	p := &renamePlan{members: a.sharingLinksLocked(ids), name: name, occupied: map[string]bool{}}
	if len(p.members) == 0 {
		return p, nil
	}
	cfg := a.Settings.Get()
	category := p.members[0].Category
	p.policy = collide.ParsePolicy(cfg.CollisionFor(category))
	p.moveOpts = moveOptions(&core.Task{Category: category}, cfg)
	if err := a.packageFoldersLocked(p); err != nil {
		return nil, err
	}
	if err := a.unpackFoldersLocked(p); err != nil {
		return nil, err
	}
	return p, nil
}

// refuseBusy turns a rename of pkg down until what holds its files is done.
func refuseBusy(pkg string) error {
	return refuseRename("busy", pkg,
		"part of %s is being unpacked, moved into place or recorded live; try again once that is done", pkg)
}

// packageFoldersLocked lists the folders the rename affects, with every task
// whose files are in each. A member with a folder of its own, or one not named
// after the package, is in none. Caller holds a.mu.
func (a *App) packageFoldersLocked(p *renamePlan) error {
	member := make(map[string]bool, len(p.members))
	byFrom := map[string]*packageFolder{}
	for _, t := range p.members {
		member[t.ID] = true
		if t.Dir != "" {
			continue
		}
		from := filepath.Clean(a.dirFor(t))
		renamed := *t
		renamed.Package = p.name
		to := filepath.Clean(a.dirFor(&renamed))
		if from == to || byFrom[from] != nil {
			continue
		}
		f := &packageFolder{from: from, to: to}
		byFrom[from] = f
		p.folders = append(p.folders, f)
	}
	if len(p.folders) == 0 {
		return nil
	}
	for _, t := range a.tasks {
		dir := filepath.Clean(a.dirFor(t))
		written := a.hasFilesLocked(t)
		if written {
			p.occupied[dir] = true
		}
		f := byFrom[dir]
		if f == nil {
			continue
		}
		// A link of the old name that has neither started nor been kept in
		// the folder goes on deriving it, which is its own package's folder.
		if !member[t.ID] && t.Dir == "" && !written {
			continue
		}
		f.tasks = append(f.tasks, t)
		f.written = f.written || written
	}
	for _, f := range p.folders {
		sortByAge(f.tasks)
		if !f.moves() {
			continue
		}
		for _, t := range f.tasks {
			if t.Status == core.StatusExtracting || a.relocating[t.ID] || a.placing[t.ID] > 0 || a.moving[t.ID] || a.recordingLocked(t) {
				return refuseBusy(p.members[0].Package)
			}
		}
	}
	return nil
}

// unpackFoldersLocked lists the members' extraction folders that are named
// after the package and already hold something. Caller holds a.mu.
func (a *App) unpackFoldersLocked(p *renamePlan) error {
	cfg := a.Settings.Get()
	seen := map[string]bool{}
	for _, t := range p.members {
		from := unpackRoot(a.expandFolder(t, cfg.ExtractTo), t, cfg.ExtractSubfolder)
		renamed := *t
		renamed.Package = p.name
		to := unpackRoot(a.expandFolder(&renamed, cfg.ExtractTo), &renamed, cfg.ExtractSubfolder)
		if from == "" || to == "" || sameDir(from, to) || seen[from] {
			continue
		}
		if fi, err := os.Stat(from); err != nil || !fi.IsDir() {
			continue
		}
		seen[from] = true
		p.unpacked = append(p.unpacked, folderMove{from: from, to: to})
	}
	if len(p.unpacked) == 0 {
		return nil
	}
	for _, t := range p.members {
		if t.Status == core.StatusExtracting {
			return refuseBusy(p.members[0].Package)
		}
	}
	return nil
}

// recorder is a backend that can tell a live stream it is recording from a
// download. Stopping a recording ends it rather than pausing it.
type recorder interface {
	Recording(taskID string) bool
}

// recordingLocked reports whether t is a live stream being recorded right now.
// Caller holds a.mu.
func (a *App) recordingLocked(t *core.Task) bool {
	r, ok := a.backendFor(t.Resolver).(recorder)
	return ok && a.active[t.ID] && r.Recording(t.ID)
}

// settle decides where f goes when its new name is taken, by the rule the app
// applies to a folder a download would land on (collide.HandoverFolder):
// rename counts the name up and skip leaves the folder where it is. Ask and
// overwrite turn the rename down, since the person who typed the name is the
// one to ask, and a folder is never overwritten. A name is taken by a folder
// with something in it, or by a package with files that derives it (occupied).
func (f *packageFolder) settle(policy collide.Policy, occupied map[string]bool) error {
	f.landed = f.to
	if !occupied[f.to] {
		// An empty folder is free, and so is the same folder under another
		// case, on a disk that does not tell the two apart.
		if _, err := os.Stat(f.to); err != nil || sameFolder(f.from, f.to) || emptyDir(f.to) {
			return nil
		}
	}
	switch policy {
	case collide.Skip:
		f.stays = true
		return nil
	case collide.Rename:
		o := collide.Options{Mkdir: func(dir string, perm fs.FileMode) error {
			if occupied[filepath.Clean(dir)] {
				return fs.ErrExist
			}
			return os.Mkdir(dir, perm)
		}}
		r, err := o.HandoverFolder(f.to, collide.Rename)
		if err != nil {
			return err
		}
		f.landed = r.Path
		return nil
	}
	return refuseRename("folderExists", filepath.Base(f.to), "a folder called %s already exists", f.to)
}

// holdKind is how a transfer was stopped for its folder to move, and so how it
// is carried on.
type holdKind int

const (
	// notHeld is nothing running: a later start or resume reads the new
	// folder.
	notHeld holdKind = iota
	// engineHeld is Engine.Hold, carried over by Engine.Release.
	engineHeld
	// halted is a halter's Halt; Resume starts it in the new folder.
	halted
	// paused is the backend's own Pause, for a backend that has not written
	// anything yet, such as a debrid service still unlocking the link.
	paused
	// dropped is a torrent taken out of the engine, which opens its files in
	// the folder it was added in. It starts again and takes up its files in
	// the new place.
	dropped
)

// halter is a backend that can stop a transfer without reporting a pause and
// wait until nothing writes to its files, so their folder can be moved under
// it. It reports whether a transfer was running.
type halter interface {
	Halt(taskID string) bool
}

// redirector is a backend that keeps its own record of where a task's files
// are, JDownloader, and moves them itself when told the new folder.
type redirector interface {
	MoveTo(taskID, dir string) error
}

// heldTransfer is one transfer of a moving folder and how it was stopped.
type heldTransfer struct {
	id, resolver string
	how          holdKind
	// engineID is the id the engine fetches it under (engineIDFor).
	engineID string
	// resume is whether it goes on after the move: it was running, and still
	// counts as running once the folder has moved.
	resume bool
}

// relocate moves the folders of a package being renamed and carries what
// writes into them over. Every task of a moving folder is in a.relocating, so
// the dispatcher and the delivery out of the working folder leave it alone
// until this is over.
func (a *App) relocate(p *renamePlan) error {
	var moving []*packageFolder
	for _, f := range p.folders {
		if !f.moves() {
			continue
		}
		if err := f.settle(p.policy, p.occupied); err != nil {
			a.endRelocation(p.folders, nil)
			return err
		}
		if f.moves() {
			moving = append(moving, f)
		}
	}

	a.mu.Lock()
	var ids []string
	for _, f := range moving {
		for _, t := range f.tasks {
			ids = append(ids, t.ID)
		}
	}
	a.awaitHandoversLocked(ids)
	var held []*heldTransfer
	running := map[string]bool{}
	started := map[string]bool{}
	for _, f := range moving {
		for _, t := range f.tasks {
			held = append(held, &heldTransfer{id: t.ID, resolver: t.Resolver, engineID: a.torrentFiles.engineIDFor(t.ID)})
			running[t.ID] = a.active[t.ID]
			started[t.ID] = a.started[t.ID] || t.Seeding
		}
	}
	a.mu.Unlock()
	for _, h := range held {
		h.how = a.stopTransfer(h, running[h.id], started[h.id])
	}

	reloc, err := a.moveFolders(moving)
	a.mu.Lock()
	var copies []taskCopy
	if err == nil {
		copies = a.applyRenameLocked(p, reloc)
	}
	restarted, seeds := a.restartDroppedLocked(held)
	copies = append(copies, restarted...)
	for _, h := range held {
		h.resume = a.active[h.id]
	}
	a.mu.Unlock()
	if err == nil {
		a.redirect(held)
	}
	if reloc == nil {
		reloc = relocation{}
	}
	for _, h := range held {
		a.carryOn(h, reloc)
	}
	for _, j := range seeds {
		a.Engine.Start(j)
	}
	a.publishTasks(copies)
	if err == nil {
		for from, to := range a.moveUnpacked(p) {
			reloc[from] = to
		}
		a.followJobs(reloc, p)
	}
	a.endRelocation(p.folders, held)
	return err
}

// stopTransfer stops what writes into a moving folder for one task. running
// is whether the app counts the task as running, and started whether a backend
// has it at all.
func (a *App) stopTransfer(h *heldTransfer, running, started bool) holdKind {
	if h.resolver == (torrent.Resolver{}).Info().ID {
		if !started && !running {
			return notHeld
		}
		a.Engine.Remove(h.id, false)
		return dropped
	}
	if a.Engine.Hold(h.engineID) {
		return engineHeld
	}
	if !running {
		return notHeld
	}
	be := a.backendFor(h.resolver)
	if s, ok := be.(halter); ok {
		if s.Halt(h.id) {
			return halted
		}
		return notHeld
	}
	be.Pause(h.id)
	return paused
}

// redirect tells a backend that records where a task's files are the task's
// folder now, before anything carries on writing. A task the backend never
// had is left to find the folder when it starts.
func (a *App) redirect(held []*heldTransfer) {
	for _, h := range held {
		r, ok := a.backendFor(h.resolver).(redirector)
		if !ok {
			continue
		}
		if err := r.MoveTo(h.id, a.taskDir(h.id)); err != nil {
			log.Printf("task %s: %s could not be told its new folder: %v", h.id, h.resolver, err)
		}
	}
}

// carryOn lets a stopped transfer go on, from wherever its folder went.
func (a *App) carryOn(h *heldTransfer, reloc relocation) {
	switch h.how {
	case engineHeld:
		a.Engine.Release(h.engineID, reloc.path, h.resume)
	case halted, paused:
		if h.resume {
			a.backendFor(h.resolver).Resume(h.id)
		}
	}
}

// restartDroppedLocked lets the torrents taken out of the engine start again.
// One that was downloading waits in the queue for the dispatcher to start it
// in the new folder, and one that was seeding gets a job that takes it up
// there and seeds on, for the caller to start. Caller holds a.mu.
func (a *App) restartDroppedLocked(held []*heldTransfer) ([]taskCopy, []engine.Job) {
	var out []taskCopy
	var seeds []engine.Job
	for _, h := range held {
		t := a.tasks[h.id]
		if h.how != dropped || t == nil {
			continue
		}
		delete(a.started, h.id)
		switch {
		case a.active[h.id]:
			delete(a.active, h.id)
			t.Status, t.Speed = core.StatusQueued, 0
			if !slices.Contains(a.queue, h.id) {
				a.queue = append(a.queue, h.id)
			}
		case t.Seeding:
			job, err := a.seedJobLocked(t)
			if err != nil {
				log.Printf("task %s could not go on seeding: %v", t.ID, err)
				t.Seeding, t.SeedingEnded = false, time.UnixMilli(time.Now().UnixMilli())
				break
			}
			a.started[h.id] = true
			seeds = append(seeds, job)
		}
		out = append(out, a.copyLocked(t))
	}
	return out, seeds
}

// seedJobLocked is the engine job that takes a finished torrent up again
// where its files are, to seed on from what it has uploaded. Caller holds a.mu.
func (a *App) seedJobLocked(t *core.Task) (engine.Job, error) {
	res, err := (torrent.Resolver{}).Resolve(a.ctx, resolver.Request{URL: t.URL})
	if err != nil {
		return engine.Job{}, err
	}
	cfg := a.Settings.Get()
	job := a.engineJobLocked(t, cfg, res.DirectURL, nil, 0)
	job.Route, _ = a.routeForLocked(t, hostOf(t.URL))
	job.TorrentSelect = core.SelectedTorrentIndices(t.TorrentFiles)
	a.torrentJobLocked(&job, t, cfg, t.File)
	job.Seed = true
	job.SeedFrom = core.TorrentStats{Uploaded: t.Uploaded, Ratio: t.Ratio}
	return job, nil
}

// endRelocation lets the dispatcher and the delivery have the tasks of the
// folders back. A transfer the user paused while its folder moved was carried
// on regardless, and is paused again. An unpacking that came due meanwhile
// starts now, and a download that finished meanwhile, or sat waiting in the
// working folder, is delivered unless its archive is being unpacked.
func (a *App) endRelocation(folders []*packageFolder, held []*heldTransfer) {
	a.mu.Lock()
	cfg := a.Settings.Get()
	var done []*core.Task
	var copies []taskCopy
	for _, f := range folders {
		for _, t := range f.tasks {
			if !a.relocating[t.ID] {
				continue
			}
			delete(a.relocating, t.ID)
			if t.Status != core.StatusDone || a.tasks[t.ID] != t {
				continue
			}
			done = append(done, t)
			if target := a.extractNowLocked(t, cfg); target != nil {
				copies = append(copies, a.copyLocked(target))
			}
		}
	}
	var deliver []string
	for _, t := range done {
		unpacking := slices.ContainsFunc(a.volumeSetLocked(t), func(part *core.Task) bool {
			return part.Status == core.StatusExtracting
		})
		if !unpacking {
			deliver = append(deliver, t.ID)
		}
	}
	var repause []*heldTransfer
	for _, h := range held {
		if h.resume && h.how != dropped && !a.active[h.id] && a.tasks[h.id] != nil {
			repause = append(repause, h)
		}
	}
	a.dispatchLocked()
	a.handoverCondLocked().Broadcast()
	a.mu.Unlock()
	a.publishTasks(copies)
	for _, h := range repause {
		a.backendFor(h.resolver).Pause(h.id)
	}
	for _, id := range deliver {
		a.spawn(func() { a.deliverDownload(id) })
	}
}

// moveFolders moves each folder and its share of the working folder to where
// it landed, and returns where everything went. A failure puts back what was
// already moved and turns the rename down.
func (a *App) moveFolders(folders []*packageFolder) (relocation, error) {
	reloc := relocation{}
	var undo []func()
	fail := func(from, to string, err error) (relocation, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		log.Printf("%s could not be moved to %s: %v", from, to, err)
		return nil, refuseRename("notMoved", filepath.Base(from),
			"the folder %s could not be renamed, so nothing was changed: %v", from, err)
	}
	root := a.workRoot()
	for _, f := range folders {
		if _, err := os.Lstat(f.from); err == nil {
			if !sameFolder(f.from, f.landed) && emptyDir(f.landed) {
				// Windows renames nothing onto a folder, even an empty one.
				_ = os.Remove(f.landed)
			}
			if err := a.moveFolder(f.from, f.landed); err != nil {
				return fail(f.from, f.landed, err)
			}
			reloc[f.from] = f.landed
			from, landed := f.from, f.landed
			undo = append(undo, func() { a.putBack(landed, from) })
		}
		if root == "" {
			continue
		}
		work, workTo := workdir.For(root, f.from), workdir.For(root, f.landed)
		if _, err := os.Lstat(work); err != nil || sameFolder(work, workTo) {
			continue
		}
		if _, err := os.Lstat(workTo); err == nil && !emptyDir(workTo) {
			// No task with files derives the landing folder (see settle), so
			// this was left behind by a download removed while it ran, within
			// the day the sweep gives it: the working folder is this app's
			// own, so what is in the way there is replaced.
			rep, err := workdir.MoveContents(a.ctx, work, workTo, workdir.Options{Policy: collide.Overwrite, PruneSourceDir: true})
			if err != nil && rep.Moved == 0 {
				return fail(work, workTo, err)
			}
			if err != nil {
				log.Printf("the working folder %s was only partly moved to %s: %v", work, workTo, err)
			}
			reloc[work] = workTo
			continue
		}
		_ = os.Remove(workTo)
		if err := a.moveFolder(work, workTo); err != nil {
			return fail(work, workTo, err)
		}
		reloc[work] = workTo
		undo = append(undo, func() { a.putBack(workTo, work) })
	}
	return reloc, nil
}

// moveFolder moves one folder by a rename, or by a copy where the new place is
// on another disk; it fails only when nothing reached the new place. A rename
// refused for another reason, such as a file held open on Windows, is not
// turned into a copy, which would take as long as the folder is large and
// leave the held file behind.
func (a *App) moveFolder(from, to string) error {
	res, err := workdir.MoveAs(a.ctx, from, to, workdir.Options{OnlyAcrossDisks: true})
	if res.Path != to {
		if err == nil {
			err = errors.New("the folder was not moved")
		}
		return err
	}
	if err != nil {
		log.Printf("%s was moved to %s, but not all of it could be removed: %v", from, to, err)
	}
	return nil
}

// putBack undoes a folder move after a later one failed.
func (a *App) putBack(moved, to string) {
	if err := a.moveFolder(moved, to); err != nil {
		log.Printf("%s could not be moved back to %s: %v", moved, to, err)
	}
}

// applyRenameLocked writes the rename onto the tasks and returns the rows it
// changed. The members take the new name. In a folder that keeps its name the
// members keep it too, by writing it into Dir; in one that moved, every task
// follows its files, into Dir where the new name alone would not take it
// there. reloc is nil when nothing moved. Caller holds a.mu.
func (a *App) applyRenameLocked(p *renamePlan, reloc relocation) []taskCopy {
	touched := map[string]*core.Task{}
	for _, f := range p.folders {
		if !f.stays || !f.written {
			continue
		}
		for _, t := range f.tasks {
			if t.Dir == "" && slices.Contains(p.members, t) {
				t.Dir = f.from
			}
		}
	}
	for _, t := range p.members {
		if a.tasks[t.ID] != t {
			continue
		}
		t.Package = p.name
		// A probe that names the link later must not put the package back.
		t.ManualPackage = true
		touched[t.ID] = t
	}
	for _, f := range p.folders {
		if reloc == nil || !f.moves() {
			continue
		}
		for _, t := range f.tasks {
			if a.tasks[t.ID] != t {
				continue
			}
			if !sameDir(a.dirFor(t), f.landed) {
				t.Dir = f.landed
			}
			t.File = reloc.path(t.File)
			if j := t.ServiceJob; j != nil && j.Partial != "" {
				moved := *j
				moved.Partial = reloc.path(j.Partial)
				t.ServiceJob = &moved
			}
			touched[t.ID] = t
		}
	}
	copies := make([]taskCopy, 0, len(touched))
	for _, t := range touched {
		copies = append(copies, a.copyLocked(t))
	}
	return copies
}

// moveUnpacked takes the members' extraction folders to the new name, entry by
// entry under the collision rule a delivery follows, since what a package of
// the new name unpacked may be there already. It returns where they went. A
// failure is logged: nothing but the extraction list remembers these folders,
// and whatever did not move is still where it was.
func (a *App) moveUnpacked(p *renamePlan) relocation {
	reloc := relocation{}
	for _, m := range p.unpacked {
		// A folder inside the download folder has already moved with it.
		if _, err := os.Lstat(m.from); err != nil {
			continue
		}
		rep, err := workdir.MoveContents(a.ctx, m.from, m.to, p.moveOpts)
		if err != nil {
			log.Printf("the unpacked files in %s could not all be moved to %s: %v", m.from, m.to, err)
		}
		if rep.Moved > 0 {
			reloc[m.from] = m.to
		}
	}
	return reloc
}

// followJobs carries the extraction list along with what a rename moved, so a
// finished unpacking still says where its archive and its content are, and
// gives the members' unpackings the new package name.
func (a *App) followJobs(reloc relocation, p *renamePlan) {
	member := make(map[string]bool, len(p.members))
	for _, t := range p.members {
		member[t.ID] = true
	}
	a.mu.Lock()
	st := a.unpackLocked()
	var changed []ExtractJob
	for _, id := range st.order {
		j := st.jobs[id]
		if j == nil {
			continue
		}
		path, dir, movedTo := reloc.path(j.path), reloc.path(j.Dir), reloc.path(j.MovedTo)
		pkg := j.Package
		if member[j.TaskID] {
			pkg = p.name
		}
		if path == j.path && dir == j.Dir && movedTo == j.MovedTo && pkg == j.Package {
			continue
		}
		j.path, j.Dir, j.MovedTo, j.Package = path, dir, movedTo, pkg
		changed = append(changed, j.ExtractJob)
	}
	a.mu.Unlock()
	for _, snap := range changed {
		a.Hub.Broadcast("extract", snap)
	}
}

// relocation maps each folder a rename moved to where it went. A path inside
// one of them goes with it.
type relocation map[string]string

func (r relocation) path(p string) string {
	if p == "" {
		return p
	}
	for from, to := range r {
		rel, err := filepath.Rel(from, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.Join(to, rel)
	}
	return p
}

// emptyDir reports whether dir is a folder with nothing in it.
func emptyDir(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.IsDir() {
		return false
	}
	names, err := f.Readdirnames(1)
	return len(names) == 0 && err != nil
}

// sameFolder reports whether two paths name one folder on disk, which on a
// disk that does not tell case apart includes two spellings of one name.
func sameFolder(a, b string) bool {
	if sameDir(a, b) {
		return true
	}
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

// beginPlacingLocked counts a delivery of id under way, which a package
// rename turns itself down for. Caller holds a.mu.
func (a *App) beginPlacingLocked(id string) {
	if a.placing == nil {
		a.placing = map[string]int{}
	}
	a.placing[id]++
}

// endPlacing ends what beginPlacingLocked began.
func (a *App) endPlacing(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placing[id]--; a.placing[id] <= 0 {
		delete(a.placing, id)
	}
}

// beginHandoverLocked counts a start of id on its way to its backend, with
// the folder to write in already decided. Until the backend has it there is
// nothing a package rename could stop, so the rename waits for it
// (awaitHandoversLocked). Caller holds a.mu.
func (a *App) beginHandoverLocked(id string) {
	if a.handing == nil {
		a.handing = map[string]int{}
	}
	a.handing[id]++
}

// endHandover ends what beginHandoverLocked began.
func (a *App) endHandover(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.handing[id]--; a.handing[id] <= 0 {
		delete(a.handing, id)
	}
	a.handoverCondLocked().Broadcast()
}

// awaitHandoversLocked waits until none of ids is on its way to a backend.
// Caller holds a.mu, which the wait gives up in between.
func (a *App) awaitHandoversLocked(ids []string) {
	for slices.ContainsFunc(ids, func(id string) bool { return a.handing[id] > 0 }) {
		a.handoverCondLocked().Wait()
	}
}

// awaitRelocationLocked waits while id's folder is being moved, for a start
// that decides its folder outside the dispatcher, such as a debrid service's
// once it has unlocked the link. A start made inside a handover the rename is
// already waiting for goes ahead. Caller holds a.mu.
func (a *App) awaitRelocationLocked(id string) {
	for a.relocating[id] && a.handing[id] == 0 {
		a.handoverCondLocked().Wait()
	}
}

// handoverCondLocked is what the waits above wait on, signalled when a
// handover ends and when a relocation does. Caller holds a.mu.
func (a *App) handoverCondLocked() *sync.Cond {
	if a.handed == nil {
		a.handed = sync.NewCond(&a.mu)
	}
	return a.handed
}
