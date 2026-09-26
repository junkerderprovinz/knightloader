package app

// A renamed package's folder. Where the download folder is built from the
// package name, the folder takes the new name on disk together with everything
// already in it: finished files, partial ones and their share of the working
// folder. What writes into it is stopped while it moves and carries on from
// the new place.

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
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
	// stays is a folder that keeps its name: JDownloader is still writing into
	// it on its own, or the new name is taken and the collision policy is to
	// skip.
	stays bool
	// landed is where the folder goes, which is not to when the collision
	// policy counted the name up.
	landed string
}

// moves reports whether the folder is moved on disk.
func (f *packageFolder) moves() bool { return f.written && !f.stays }

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
	members := a.sharingLinksLocked(ids)
	folders, err := a.packageFoldersLocked(members, name)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	var moving []*packageFolder
	for _, f := range folders {
		if f.moves() {
			moving = append(moving, f)
		}
	}
	if len(moving) == 0 {
		copies := a.applyRenameLocked(members, name, folders, nil)
		a.mu.Unlock()
		a.publishTasks(copies)
		return idsOf(members), nil
	}
	if a.relocating == nil {
		a.relocating = map[string]bool{}
	}
	for _, f := range moving {
		for _, t := range f.tasks {
			a.relocating[t.ID] = true
		}
	}
	policy := collide.ParsePolicy(a.Settings.Get().CollisionFor(members[0].Category))
	a.mu.Unlock()

	if err := a.relocate(members, name, folders, policy); err != nil {
		return nil, err
	}
	return idsOf(members), nil
}

// packageFoldersLocked lists the folders renaming members' package to name
// affects, with every task whose files are in each. A member with a folder of
// its own, or one not named after the package, is in none. The rename is
// turned down while something in a folder that would move is being unpacked
// or moved into place. Caller holds a.mu.
func (a *App) packageFoldersLocked(members []*core.Task, name string) ([]*packageFolder, error) {
	member := make(map[string]bool, len(members))
	byFrom := map[string]*packageFolder{}
	var out []*packageFolder
	for _, t := range members {
		member[t.ID] = true
		if t.Dir != "" {
			continue
		}
		from := filepath.Clean(a.dirFor(t))
		renamed := *t
		renamed.Package = name
		to := filepath.Clean(a.dirFor(&renamed))
		if from == to || byFrom[from] != nil {
			continue
		}
		f := &packageFolder{from: from, to: to}
		byFrom[from] = f
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, nil
	}
	for _, t := range a.tasks {
		f := byFrom[filepath.Clean(a.dirFor(t))]
		if f == nil {
			continue
		}
		written := a.hasFilesLocked(t)
		// A link of the old name that has neither started nor been kept in
		// the folder goes on deriving it, which is its own package's folder.
		if !member[t.ID] && t.Dir == "" && !written {
			continue
		}
		f.tasks = append(f.tasks, t)
		f.written = f.written || written
		// JDownloader writes where it was told when the link was handed over,
		// and nothing here can stop it or point it elsewhere.
		if written && !filesAreLocal(t) && t.Status != core.StatusDone {
			f.stays = true
		}
	}
	for _, f := range out {
		sortByAge(f.tasks)
		if !f.moves() {
			continue
		}
		for _, t := range f.tasks {
			if t.Status == core.StatusExtracting || a.relocating[t.ID] || a.placing[t.ID] > 0 || a.moving[t.ID] {
				pkg := members[0].Package
				return nil, refuseRename("busy", pkg,
					"part of %s is being unpacked or moved into place; try again once that is done", pkg)
			}
		}
	}
	return out, nil
}

// settle decides where f goes when its new name is taken, by the rule the app
// applies to a folder a download would land on (collide.HandoverFolder):
// rename counts the name up and skip leaves the folder where it is. Ask and
// overwrite turn the rename down, since the person who typed the name is the
// one to ask, and a folder is never overwritten. An empty folder under the new
// name is no collision.
func (f *packageFolder) settle(policy collide.Policy) error {
	f.landed = f.to
	at, err := os.Stat(f.to)
	if err != nil {
		return nil
	}
	if from, err := os.Stat(f.from); err == nil && os.SameFile(at, from) {
		// The same folder under another case, on a disk that does not tell
		// the two apart.
		return nil
	}
	if emptyDir(f.to) {
		return nil
	}
	switch policy {
	case collide.Skip:
		f.stays = true
		return nil
	case collide.Rename:
		r, err := collide.HandoverFolder(f.to, collide.Rename)
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
func (a *App) relocate(members []*core.Task, name string, folders []*packageFolder, policy collide.Policy) error {
	var moving []*packageFolder
	for _, f := range folders {
		if !f.moves() {
			continue
		}
		if err := f.settle(policy); err != nil {
			a.endRelocation(folders, nil)
			return err
		}
		if f.moves() {
			moving = append(moving, f)
		}
	}

	a.mu.Lock()
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
		copies = a.applyRenameLocked(members, name, folders, reloc)
	}
	copies = append(copies, a.restartDroppedLocked(held)...)
	for _, h := range held {
		h.resume = a.active[h.id]
	}
	a.mu.Unlock()
	if reloc == nil {
		reloc = relocation{}
	}
	for _, h := range held {
		a.carryOn(h, reloc)
	}
	a.publishTasks(copies)
	a.endRelocation(folders, held)
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

// restartDroppedLocked lets the torrents taken out of the engine start again,
// in the new folder once the dispatcher lets them. A torrent that was seeding
// has stopped, as it does at a shutdown. Caller holds a.mu.
func (a *App) restartDroppedLocked(held []*heldTransfer) []taskCopy {
	var out []taskCopy
	for _, h := range held {
		t := a.tasks[h.id]
		if h.how != dropped || t == nil {
			continue
		}
		delete(a.started, h.id)
		if a.active[h.id] {
			delete(a.active, h.id)
			t.Status, t.Speed = core.StatusQueued, 0
			if !slices.Contains(a.queue, h.id) {
				a.queue = append(a.queue, h.id)
			}
		}
		if t.Seeding {
			t.Seeding, t.SeedingEnded = false, time.UnixMilli(time.Now().UnixMilli())
		}
		out = append(out, a.copyLocked(t))
	}
	return out
}

// endRelocation lets the dispatcher and the delivery have the tasks of the
// folders back. A transfer the user paused while its folder moved was carried
// on regardless, and is paused again. A download that finished meanwhile, or
// sat waiting in the working folder, is delivered once the tasks are free.
func (a *App) endRelocation(folders []*packageFolder, held []*heldTransfer) {
	a.mu.Lock()
	var deliver []string
	for _, f := range folders {
		for _, t := range f.tasks {
			if !a.relocating[t.ID] {
				continue
			}
			delete(a.relocating, t.ID)
			if t.Status == core.StatusDone && a.tasks[t.ID] == t {
				deliver = append(deliver, t.ID)
			}
		}
	}
	var repause []*heldTransfer
	for _, h := range held {
		if h.resume && h.how != dropped && !a.active[h.id] && a.tasks[h.id] != nil {
			repause = append(repause, h)
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	for _, h := range repause {
		a.backendFor(h.resolver).Pause(h.id)
	}
	for _, id := range deliver {
		a.spawn(func() { a.deliverDownload(id) })
	}
}

// moveFolders moves each folder and its share of the working folder to where
// it landed, and returns where everything went. A failure puts back what was
// already moved.
func (a *App) moveFolders(folders []*packageFolder) (relocation, error) {
	reloc := relocation{}
	var undo []func()
	fail := func(err error) (relocation, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}
	root := a.workRoot()
	for _, f := range folders {
		if _, err := os.Lstat(f.from); err == nil {
			if !sameDir(f.from, f.landed) && emptyDir(f.landed) {
				// Windows renames nothing onto a folder, even an empty one.
				_ = os.Remove(f.landed)
			}
			if err := a.moveFolder(f.from, f.landed); err != nil {
				return fail(err)
			}
			reloc[f.from] = f.landed
			from, landed := f.from, f.landed
			undo = append(undo, func() { a.putBack(landed, from) })
		}
		if root == "" {
			continue
		}
		work, workTo := workdir.For(root, f.from), workdir.For(root, f.landed)
		if _, err := os.Lstat(work); err != nil || sameDir(work, workTo) {
			continue
		}
		if _, err := os.Lstat(workTo); err == nil && !emptyDir(workTo) {
			// Left behind by a download removed while it ran, within the day
			// the sweep gives it: the working folder is this app's own, so
			// what is in the way there is replaced.
			rep, err := workdir.MoveContents(a.ctx, work, workTo, workdir.Options{Policy: collide.Overwrite, PruneSourceDir: true})
			if err != nil && rep.Moved == 0 {
				return fail(err)
			}
			if err != nil {
				log.Printf("the working folder %s was only partly moved to %s: %v", work, workTo, err)
			}
			reloc[work] = workTo
			continue
		}
		_ = os.Remove(workTo)
		if err := a.moveFolder(work, workTo); err != nil {
			return fail(err)
		}
		reloc[work] = workTo
		undo = append(undo, func() { a.putBack(workTo, work) })
	}
	return reloc, nil
}

// moveFolder moves one folder, and fails only when nothing reached the new
// place: a copy across disks whose source could not be removed has still
// moved the files.
func (a *App) moveFolder(from, to string) error {
	res, err := workdir.MoveAs(a.ctx, from, to, workdir.Options{})
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
func (a *App) applyRenameLocked(members []*core.Task, name string, folders []*packageFolder, reloc relocation) []taskCopy {
	touched := map[string]*core.Task{}
	for _, f := range folders {
		if !f.stays || !f.written {
			continue
		}
		for _, t := range f.tasks {
			if t.Dir == "" && slices.Contains(members, t) {
				t.Dir = f.from
			}
		}
	}
	for _, t := range members {
		if a.tasks[t.ID] != t {
			continue
		}
		t.Package = name
		// A probe that names the link later must not put the package back.
		t.ManualPackage = true
		touched[t.ID] = t
	}
	for _, f := range folders {
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
