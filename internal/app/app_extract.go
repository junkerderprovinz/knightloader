package app

// Unpacking: which finished download completes an archive, which passwords are
// tried on it, and what a failed extraction does to the task.
//
// An extraction is a job here, not the tail end of a download. It is queued, it
// says how far it has got, it can be started on a download that finished an hour
// ago, and it can be called off - which is the only reason a wrong password or a
// full disk is recoverable without fetching the whole set again.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// keptExtractJobs is how many finished jobs stay in the list. A job outlives its
// extraction on purpose - "what happened to that archive" is asked after the
// fact, and a job that vanished the moment it ended answers nothing - but the
// list is not a log, so the oldest finished ones fall off the end.
const keptExtractJobs = 50

// ExtractStatus is where an unpacking has got to.
//
// It is not core.Status, and the two must not be folded together: the seven
// download states are matched exhaustively in the interface, and an eighth value
// arriving there through an archive breaks every one of those mappings. An
// extraction is a different kind of work that happens to belong to a task.
type ExtractStatus string

const (
	ExtractQueued    ExtractStatus = "queued"
	ExtractRunning   ExtractStatus = "running"
	ExtractDone      ExtractStatus = "done"
	ExtractFailed    ExtractStatus = "error"
	ExtractCancelled ExtractStatus = "cancelled"
)

// ExtractJob is one unpacking as the list sees it: an object in its own right,
// rather than a status the download wears for a while. The archive is the thing
// the user is waiting on at that point, and it has its own progress, its own
// failure and its own cancel.
type ExtractJob struct {
	ID string `json:"id"`
	// TaskID is the volume the job was started on, which for a multi-volume set
	// is the first part and not whichever one finished last.
	TaskID  string        `json:"taskId"`
	Name    string        `json:"name"`
	Dir     string        `json:"dir"`
	Package string        `json:"package,omitempty"`
	Status  ExtractStatus `json:"status"`
	// Archive is the file open right now, which for a deep extraction is one
	// found inside the output rather than the one named above.
	Archive string `json:"archive,omitempty"`
	Depth   int    `json:"depth,omitempty"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	// Volumes is how many files the set is made of, so the row can say "part 3
	// of 5" instead of naming one part of an archive nobody downloaded singly.
	Volumes int `json:"volumes"`
	// Nested counts archives found inside the output and unpacked in turn.
	Nested int    `json:"nested,omitempty"`
	Error  string `json:"error,omitempty"`
	// Password is the failure being a missing password rather than a broken
	// archive. It is a flag as well as a sentence, because it is the one
	// extraction failure with an obvious next step and the interface can offer
	// it: type one in and press start again.
	Password  bool      `json:"password,omitempty"`
	QueuedAt  time.Time `json:"queuedAt"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

// extractJob is an ExtractJob plus what only this package may hold: where the
// archive is, and the handle that stops it.
type extractJob struct {
	ExtractJob
	path   string
	cancel context.CancelFunc
}

// unpackState is the extraction worker. Everything in it is read and written
// under a.mu, including busy: the check "is a worker already running" and the
// decision to start one have to be one critical section, or two finishing
// downloads start two workers and the second one sits blocked inside
// internal/extract, holding a job the list shows as running.
//
// cancel on a job is set and cleared in the same critical section as Status, so
// a job read as ExtractRunning always has one.
type unpackState struct {
	jobs  map[string]*extractJob
	order []string
	busy  bool
}

// filesAreLocal reports whether this task's bytes landed on this box. A JD
// download lives on the JD machine, so everything that opens or renames the
// finished file has to leave it alone. It is one predicate rather than a
// `Resolver != "jd"` at each of those places, because the day a second remote
// backend arrives, the one that was forgotten is the one that deletes or
// renames a file it cannot see.
func filesAreLocal(t *core.Task) bool { return t.Resolver != "jd" }

// extractWanted is the task's own unpacking switch when a Packagizer rule or
// the user set one, and the global setting otherwise. A rule that says "do not
// unpack this" has to survive a global that says otherwise, or the rule is a
// setting that does nothing.
func extractWanted(t *core.Task, cfg settings.Settings) bool {
	if t.AutoExtract != nil {
		return *t.AutoExtract
	}
	return cfg.Extract
}

// setKey identifies the set a file belongs to, and it is the archive families
// plus the plain split files the format layer has no reader for.
//
// A film cut into "film.mkv.001" upwards is a multi-part download in every way
// that matters here - it must not be renamed a part at a time, an unpacking
// override belongs to the whole set, and nothing can be joined until the last
// part lands - and extract.SetKey answers "no set" for it, because from the
// reader layer's point of view there is no archive in sight.
func setKey(name string) (string, bool) {
	if k, ok := extract.SetKey(name); ok {
		return k, true
	}
	if stem, _, ok := extract.SplitPart(name); ok {
		return strings.ToLower(stem) + "|split", true
	}
	return "", false
}

// extractCandidateLocked decides whether a just-finished download completes an
// archive that can now be unpacked, and returns the task to unpack. For a
// multi-volume set that is the moment the LAST part arrives — and what gets
// unpacked is the first volume, not necessarily the part that finished last.
// Caller holds a.mu.
func (a *App) extractCandidateLocked(done *core.Task) (*core.Task, string) {
	key, isVolume := setKey(done.Name)
	if !isVolume {
		if extract.Startable(done.Name) {
			return done, filepath.Join(a.dirFor(done), done.Name)
		}
		return nil, ""
	}
	dir := a.dirFor(done)
	set := a.membersLocked(key, dir)
	var first *core.Task
	for _, t := range set {
		if t.Status != core.StatusDone {
			// A part is still missing (or already extracting, which means
			// another part got here first). Whoever finishes last triggers it.
			return nil, ""
		}
		// By the order the readers consume the set in, never by the order the
		// names sort in: a spanned rar begins at "film.rar" and a spanned zip
		// ends at "film.zip", so a plain sort picks the wrong end of one of them.
		if extract.Startable(t.Name) && (first == nil || volumeBefore(t, first)) {
			first = t
		}
	}
	if first == nil {
		return nil, "" // parts without a first volume: nothing to open
	}
	if _, _, split := extract.SplitPart(first.Name); split && len(set) < 2 {
		// One numbered part and no siblings is not a split file, it is a file
		// whose name happens to end in a number. Joining it would rewrite it
		// under a shorter name for no reason.
		return nil, ""
	}
	return first, filepath.Join(dir, first.Name)
}

// volumeBefore orders two parts of one set the way the archive is read.
func volumeBefore(x, y *core.Task) bool {
	rx, ry := extract.VolumeRank(x.Name), extract.VolumeRank(y.Name)
	if rx != ry {
		return rx < ry
	}
	return x.Name < y.Name
}

// membersLocked is every task in one set, in the folder the set lives in.
// Caller holds a.mu.
func (a *App) membersLocked(key, dir string) []*core.Task {
	var out []*core.Task
	for _, t := range a.tasks {
		if k, ok := setKey(t.Name); ok && k == key && a.dirFor(t) == dir {
			out = append(out, t)
		}
	}
	return out
}

// volumeSetLocked is every task that is a part of the same archive as t,
// including t itself. A file that is not one of several parts is alone in its
// own set, so a caller never has to special-case the ordinary download.
//
// The folder is part of the identity: two unrelated releases both called
// "film.part01.rar", downloaded into two packages, are two archives and not one
// five-part set with three parts missing. Caller holds a.mu.
func (a *App) volumeSetLocked(t *core.Task) []*core.Task {
	key, ok := setKey(t.Name)
	if !ok {
		return []*core.Task{t}
	}
	out := a.membersLocked(key, a.dirFor(t))
	if len(out) == 0 {
		// t is a copy rather than the live task — every caller inside the app
		// passes the live one, but answering "no set at all" here would silently
		// take a whole archive out of a decision that is about it.
		return []*core.Task{t}
	}
	return out
}

// stampPartsLocked numbers the parts of t's set, so the list can show an archive
// as the several files it really is rather than as five unrelated downloads that
// happen to share a name. It reports the rows it changed.
//
// The numbering is the readers' order and not the names' - see volumeBefore.
// Caller holds a.mu.
func (a *App) stampPartsLocked(t *core.Task) []core.Task {
	if t == nil {
		// The task was removed between the job being queued and it running, which
		// is an ordinary thing for somebody to do to a queue.
		return nil
	}
	set := a.volumeSetLocked(t)
	if len(set) < 2 {
		return nil
	}
	sort.Slice(set, func(i, j int) bool { return volumeBefore(set[i], set[j]) })
	var changed []core.Task
	for i, part := range set {
		if part.ArchivePart == i+1 {
			continue
		}
		part.ArchivePart = i + 1
		changed = append(changed, *part)
	}
	return changed
}

// extractionDueLocked decides whether a finished download now completes an
// archive that should be unpacked, and hands back the volume to open.
//
// The order is the whole point. The candidate is found FIRST and the unpacking
// switch is then read off THAT task, because extractCandidateLocked returns the
// first volume of a multi-part set and not the part that happened to finish
// last. Asked of the finishing part instead, one archive would extract or not
// depending on which of its five parts the hoster served quickest — the same
// rule, the same set, a different answer every time.
// Caller holds a.mu.
func (a *App) extractionDueLocked(done *core.Task, cfg settings.Settings) (*core.Task, string) {
	target, path := a.extractCandidateLocked(done)
	if target == nil || !extractWanted(target, cfg) {
		return nil, ""
	}
	return target, path
}

// extractNowLocked starts the extraction a finished download is due and reports
// the task it moved into StatusExtracting, which for a multi-volume set is the
// first volume rather than the one handed in — the caller has to publish that
// row too, or the list shows the wrong part working.
//
// It is called both when a download finishes and when the switch is turned on
// afterwards, and that is the point: the answer is read at extraction time, so
// a task told to unpack an hour after it landed still unpacks. Re-entry is
// free, because the target has left StatusDone by the time a second call could
// look at it. Caller holds a.mu.
func (a *App) extractNowLocked(done *core.Task, cfg settings.Settings) *core.Task {
	if done.Status != core.StatusDone || !filesAreLocal(done) {
		return nil
	}
	target, path := a.extractionDueLocked(done, cfg)
	if target == nil {
		return nil
	}
	if a.enqueueExtractLocked(target, path) == nil {
		return nil
	}
	return target
}

// passwordsFor is the order archive passwords are tried in: the task's own
// first, because it was set for exactly this file, then the global list. It is
// read when the job starts rather than when it was queued, so a password typed
// while three archives were waiting is used on all three.
func (a *App) passwordsFor(t *core.Task) []string {
	var out []string
	if t != nil && t.Password != "" {
		out = append(out, t.Password)
	}
	return append(out, a.Settings.Get().ArchivePasswords...)
}

// extractOptionsFor is the archive settings as this one task's extraction sees
// them: where it writes, what it does about a folder already there, and what
// becomes of the volumes afterwards.
//
// Built when the job starts and not when the download finished, which is the
// whole of the "read at extraction time" rule: a person who turns unpacking on,
// picks a destination or switches the disposal to trash while three archives sit
// in the queue means it for those three.
//
// The destination may be a template, and it is expanded here for the same reason
// dirFor expands the download folder here: this is the only place that knows
// which task the variables are about. internal/extract never sees a task.
func (a *App) extractOptionsFor(t *core.Task, cfg settings.Settings) extract.Options {
	o := extract.Options{
		Passwords:   a.passwordsFor(t),
		Collision:   extract.ParseCollision(cfg.ExtractCollision),
		Disposal:    extract.ParseDisposal(cfg.ArchiveDisposal),
		TrashRoot:   a.defaultDir(),
		TrashMaxAge: time.Duration(cfg.TrashRetentionDays) * 24 * time.Hour,
		InfoFiles:   cfg.DeleteInfoFiles,
		Subfolder:   cfg.ExtractSubfolder,
	}
	if t != nil {
		o.Package = t.Package
	}
	dest := strings.TrimSpace(cfg.ExtractTo)
	if dest != "" && t != nil && pathvars.HasVars(dest) {
		dest = pathvars.Expand(dest, pathvars.Vars{
			Package: t.Package,
			Host:    hostOf(t.URL),
			Name:    t.Name,
			Date:    t.CreatedAt,
		})
	}
	// A template that expanded to something relative is dropped rather than
	// resolved against whatever the process's working directory happens to be:
	// beside the archive is always somewhere real, and it is where every install
	// unpacked before the setting existed.
	if dest != "" && filepath.IsAbs(dest) {
		o.Dest = dest
	}
	return o
}

// packageFilesLocked is every finished file of one task's package that sits in
// the same folder, which is the narrowing the info-file sweep depends on.
//
// The whole safety of that sweep is in this list. Listing the folder and
// deleting every .nfo in it is the obvious implementation and a data-loss bug on
// the default layout: subfolder-by-package is off out of the box, so one folder
// holds several releases, and a neighbour's notes are not ours to remove.
// Caller holds a.mu.
func (a *App) packageFilesLocked(t *core.Task) []string {
	if t == nil {
		return nil
	}
	if strings.TrimSpace(t.Package) == "" {
		// No package is no scope. Matching the empty package name would gather
		// every unpackaged download in the folder, which is the directory-wide
		// sweep this list exists to avoid and is a whole shared download folder
		// on a fresh install. A sweep that does nothing is a setting somebody
		// notices; a sweep that takes the neighbours is not.
		return nil
	}
	dir := a.dirFor(t)
	var out []string
	for _, other := range a.tasks {
		if other.Package != t.Package || other.Name == "" || a.dirFor(other) != dir {
			continue
		}
		out = append(out, filepath.Join(dir, other.Name))
	}
	sort.Strings(out)
	return out
}

// disposable narrows a list of files to the ones that removing cannot take a
// second download with it.
//
// A path more than one task points at is left alone, and that is the whole
// point of this function. The disposal used to run straight down the volume
// list, which is right for the ordinary archive and wrong for the two shapes
// that share a file: a mirror, which the collector stages as its own task
// pointing at the same bytes, and the same link added twice by hand. Deleting
// the archive then settles the row that extracted it and quietly empties the
// other one, which afterwards claims a finished download whose file is gone -
// and nobody connects the missing file with having unpacked something else.
//
// A path no task claims at all is disposable: it is a volume the reader pulled
// in that never had a row of its own, and it is still part of what was consumed.
func (a *App) disposable(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	claims := make(map[string]int, len(a.tasks))
	for _, t := range a.tasks {
		if t.Name == "" {
			continue
		}
		claims[filepath.Clean(filepath.Join(a.dirFor(t), t.Name))]++
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if claims[filepath.Clean(p)] <= 1 {
			out = append(out, p)
		}
	}
	return out
}

// unpackLocked is the extraction worker's state, built the first time anything
// unpacks. Caller holds a.mu.
func (a *App) unpackLocked() *unpackState {
	if a.unpack == nil {
		a.unpack = &unpackState{jobs: map[string]*extractJob{}}
	}
	return a.unpack
}

// enqueueExtractLocked puts one archive on the worklist and moves its task into
// StatusExtracting. It answers nil when that archive is already being unpacked,
// so a finishing download and a person pressing "unpack" at the same moment are
// one job and not two. Caller holds a.mu.
func (a *App) enqueueExtractLocked(target *core.Task, path string) *extractJob {
	st := a.unpackLocked()
	for _, j := range st.jobs {
		if j.TaskID == target.ID && (j.Status == ExtractQueued || j.Status == ExtractRunning) {
			return nil
		}
	}
	job := &extractJob{
		ExtractJob: ExtractJob{
			ID:       newID(),
			TaskID:   target.ID,
			Name:     filepath.Base(path),
			Dir:      filepath.Dir(path),
			Package:  target.Package,
			Status:   ExtractQueued,
			Volumes:  len(a.volumeSetLocked(target)),
			QueuedAt: time.Now(),
		},
		path: path,
	}
	target.Status = core.StatusExtracting
	st.jobs[job.ID] = job
	st.order = append(st.order, job.ID)
	a.pruneJobsLocked(st)
	if !st.busy {
		st.busy = true
		go a.runExtractions()
	}
	return job
}

// pruneJobsLocked drops the oldest finished jobs once the list is longer than it
// is useful. A live job is never dropped, however old the queue is.
func (a *App) pruneJobsLocked(st *unpackState) {
	over := len(st.order) - keptExtractJobs
	if over <= 0 {
		return
	}
	kept := make([]string, 0, len(st.order))
	for _, id := range st.order {
		j := st.jobs[id]
		if over > 0 && j != nil && j.Status != ExtractQueued && j.Status != ExtractRunning {
			delete(st.jobs, id)
			over--
			continue
		}
		kept = append(kept, id)
	}
	st.order = kept
}

// runExtractions is the one goroutine that unpacks. Jobs run one at a time on
// purpose: two archives against the same disk only make each other slower.
//
// internal/extract enforces the same thing for its own reasons, and this queue
// is not a duplicate of it. That one is a slot a second caller waits on, which
// is invisible; this one is the list, where a waiting extraction has a name, a
// place in the order and a button that calls it off before it ever starts.
func (a *App) runExtractions() {
	for {
		a.mu.Lock()
		st := a.unpackLocked()
		job := nextQueuedLocked(st)
		if job == nil {
			// Cleared under the same lock the next enqueue takes, so a job queued
			// at this instant either sees a live worker or starts one.
			st.busy = false
			a.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(a.ctx)
		job.cancel = cancel
		job.Status = ExtractRunning
		job.StartedAt = time.Now()
		target := a.tasks[job.TaskID]
		opts := a.extractOptionsFor(target, a.Settings.Get())
		siblings := a.packageFilesLocked(target)
		parts := a.stampPartsLocked(target)
		snap := job.ExtractJob
		path := job.path
		a.mu.Unlock()

		a.saveAndBroadcast(parts)
		a.Hub.Broadcast("extract", snap)

		id := snap.ID
		out, err := extract.Run(ctx, extract.Request{
			Path:    path,
			Options: opts,
			OnProgress: func(p extract.Progress) {
				a.publishExtractProgress(id, p)
			},
		})
		cancel()
		a.settleExtraction(id, opts, siblings, out, err)
	}
}

// nextQueuedLocked is the oldest job still waiting. Caller holds a.mu.
func nextQueuedLocked(st *unpackState) *extractJob {
	for _, id := range st.order {
		if j := st.jobs[id]; j != nil && j.Status == ExtractQueued {
			return j
		}
	}
	return nil
}

// publishExtractProgress copies what the worker reported onto the job and sends
// it on. internal/extract throttles the callback, so this is not on the hot path
// of the copy itself.
func (a *App) publishExtractProgress(jobID string, p extract.Progress) {
	a.mu.Lock()
	j := a.unpackLocked().jobs[jobID]
	if j == nil || j.Status != ExtractRunning {
		a.mu.Unlock()
		return
	}
	j.Archive, j.Depth, j.Files, j.Bytes = p.Archive, p.Depth, p.Files, p.Bytes
	snap := j.ExtractJob
	a.mu.Unlock()
	a.Hub.Broadcast("extract", snap)
}

// settleExtraction records how a job ended, hands the task back to done, and
// disposes of the volumes the way the options say.
//
// Disposal happens only on success, and only on success: the volumes are the one
// copy of bytes the user paid for in bandwidth, and an extraction that failed is
// exactly the case where they will be needed again. The info files beside them
// go the same way as the archive, never a different one - see InfoFilesIn.
func (a *App) settleExtraction(jobID string, opts extract.Options, siblings []string, out *extract.Outcome, err error) {
	cancelled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if err == nil && out != nil {
		if derr := opts.Dispose(a.disposable(out.Volumes)); derr != nil {
			log.Printf("extraction finished but the archive could not be disposed of: %v", derr)
		}
		if derr := opts.Dispose(a.disposable(opts.InfoFilesIn(siblings))); derr != nil {
			log.Printf("extraction finished but the info files could not be disposed of: %v", derr)
		}
	}
	// Swept on every settle, not only after a disposal. A user who switches the
	// disposal back to "delete" would otherwise leave whatever is already in the
	// trash sitting there for good, because nothing would ever put another file
	// in to trigger the sweep. It costs one ReadDir and does nothing at all when
	// the retention is zero or no trash folder was ever made.
	if n, derr := extract.SweepTrash(opts.TrashRoot, opts.TrashMaxAge); derr != nil {
		log.Printf("the archive trash could not be swept: %v", derr)
	} else if n > 0 {
		log.Printf("swept %d file(s) out of the archive trash", n)
	}

	a.mu.Lock()
	j := a.unpackLocked().jobs[jobID]
	if j == nil {
		a.mu.Unlock()
		return
	}
	j.cancel = nil
	j.EndedAt = time.Now()
	switch {
	case cancelled:
		j.Status = ExtractCancelled
	case err != nil:
		j.Status = ExtractFailed
		j.Error = err.Error()
		j.Password = errors.Is(err, extract.ErrPasswordRequired)
	default:
		j.Status = ExtractDone
		j.Error = ""
		if out != nil {
			j.Files, j.Bytes, j.Nested = out.Files, out.Bytes, out.Nested
			if out.Dir != "" {
				j.Dir = out.Dir
			}
		}
		j.Archive = ""
	}
	snap := j.ExtractJob

	var settled *core.Task
	if t := a.tasks[j.TaskID]; t != nil {
		// Back to done either way: the download itself finished, and an archive
		// that would not open does not undo the bytes on disk.
		if t.Status == core.StatusExtracting {
			t.Status = core.StatusDone
		}
		// Only this package's own sentence is cleared. A rename that was refused
		// left its reason on the same field, and a successful extraction is no
		// reason to tell the user that problem went away.
		if strings.HasPrefix(t.Error, extractErrorPrefix) {
			t.Error = ""
		}
		if err != nil && !cancelled {
			t.Error = extractErrorPrefix + err.Error()
		}
		c := *t
		settled = &c
	}
	a.mu.Unlock()

	if settled != nil {
		a.saveAndBroadcast([]core.Task{*settled})
	}
	a.Hub.Broadcast("extract", snap)
}

// extractErrorPrefix marks the sentences this package puts on a task, so a retry
// that works can clear its own failure and nothing else's.
const extractErrorPrefix = "extract: "

// ExtractJobs is every unpacking the app knows about, oldest first.
func (a *App) ExtractJobs() []ExtractJob {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.unpackLocked()
	out := make([]ExtractJob, 0, len(st.order))
	for _, id := range st.order {
		if j := st.jobs[id]; j != nil {
			out = append(out, j.ExtractJob)
		}
	}
	return out
}

// StartExtraction unpacks finished downloads on demand.
//
// It is the entry point the automatic path never had. Extraction used to happen
// only as the tail of a finishing download, so an archive that failed on a wrong
// password, or on a disk that filled halfway through, could not be tried again
// without fetching every volume a second time. The password is typed, the disk
// is emptied, and this is the button that then does something.
//
// The unpacking switch is deliberately not consulted. Pressing "unpack this" IS
// the answer to that question, and a menu entry that silently does nothing
// because a rule turned unpacking off two weeks ago is worse than no entry.
func (a *App) StartExtraction(ids []string) error {
	var refused []string

	a.mu.Lock()
	var started []core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if !filesAreLocal(t) {
			refused = append(refused, fmt.Sprintf("%s was downloaded on another machine", t.Name))
			continue
		}
		if t.Status == core.StatusExtracting {
			continue // already on the worklist; asking twice is not an error
		}
		if t.Status != core.StatusDone {
			refused = append(refused, fmt.Sprintf("%s has not finished downloading", t.Name))
			continue
		}
		target, path := a.extractCandidateLocked(t)
		if target == nil {
			refused = append(refused, reasonNotAnArchive(t, a.volumeSetLocked(t)))
			continue
		}
		if job := a.enqueueExtractLocked(target, path); job != nil {
			started = append(started, *target)
		}
	}
	a.mu.Unlock()
	a.saveAndBroadcast(started)

	if len(refused) > 0 {
		return errors.New(strings.Join(refused, "; "))
	}
	return nil
}

// reasonNotAnArchive says which of the two things went wrong, because they need
// opposite responses: a set with a part still downloading will unpack itself the
// moment that part lands, while a file that is no archive at all never will.
func reasonNotAnArchive(t *core.Task, set []*core.Task) string {
	for _, part := range set {
		if part.Status != core.StatusDone {
			return fmt.Sprintf("%s is one part of a set and %s has not finished", t.Name, part.Name)
		}
	}
	return fmt.Sprintf("%s is not an archive this build can open", t.Name)
}

// AbortExtraction calls off an unpacking and takes the half-written output back
// off the disk.
//
// Both halves matter. A cancelled extraction that leaves its folder behind is
// indistinguishable from one that finished: nothing on disk says which, the
// person looking at the folder calls the download good, and the next deep pass
// walks into it and unpacks whatever it finds. The removal happens inside the
// worker, which is the only thing that knows which files it wrote and which
// folders were already there - see internal/extract.
func (a *App) AbortExtraction(jobID string) error {
	a.mu.Lock()
	st := a.unpackLocked()
	j := st.jobs[jobID]
	if j == nil {
		a.mu.Unlock()
		return fmt.Errorf("no extraction with id %q", jobID)
	}
	switch j.Status {
	case ExtractRunning:
		// The worker settles the job when Run comes back cancelled, so nothing is
		// written here: two writers for one ending is how a job ends up cancelled
		// and done at the same time.
		j.cancel()
		a.mu.Unlock()
		return nil
	case ExtractQueued:
		j.Status = ExtractCancelled
		j.EndedAt = time.Now()
		snap := j.ExtractJob
		var settled *core.Task
		if t := a.tasks[j.TaskID]; t != nil && t.Status == core.StatusExtracting {
			// Nothing ran, so there is nothing to take back - only the row, which
			// has been sitting there saying "extracting" since it was queued.
			t.Status = core.StatusDone
			c := *t
			settled = &c
		}
		a.mu.Unlock()
		if settled != nil {
			a.saveAndBroadcast([]core.Task{*settled})
		}
		a.Hub.Broadcast("extract", snap)
		return nil
	default:
		a.mu.Unlock()
		return fmt.Errorf("%s has already finished unpacking", j.Name)
	}
}
