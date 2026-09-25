package app

// Unpacking: which finished download completes an archive, which passwords are
// tried, and what a failed extraction does to the task. An extraction is a job
// of its own: queued, with progress, startable later and cancellable, so a
// wrong password or a full disk does not mean fetching the set again.

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
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// keptExtractJobs is how many finished jobs stay in the list, so "what
// happened to that archive" can still be answered afterwards.
const keptExtractJobs = 50

// ExtractStatus is where an unpacking has got to. It is kept apart from
// core.Status, whose values the interface matches exhaustively.
type ExtractStatus string

const (
	ExtractQueued    ExtractStatus = "queued"
	ExtractRunning   ExtractStatus = "running"
	ExtractDone      ExtractStatus = "done"
	ExtractFailed    ExtractStatus = "error"
	ExtractCancelled ExtractStatus = "cancelled"
)

// ExtractJob is one unpacking as the list shows it, with its own progress,
// failure and cancel.
type ExtractJob struct {
	ID string `json:"id"`
	// TaskID is the volume the job was started on: the first part of a set,
	// not the one that finished last.
	TaskID  string        `json:"taskId"`
	Name    string        `json:"name"`
	Dir     string        `json:"dir"`
	Package string        `json:"package,omitempty"`
	Status  ExtractStatus `json:"status"`
	// Archive is the file open right now, which in a deep extraction may be
	// one found inside the output.
	Archive string `json:"archive,omitempty"`
	Depth   int    `json:"depth,omitempty"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	// Unpacked and Size measure the archive open now, as extract.Progress
	// does, so the list can say how far through it the job is.
	Unpacked int64 `json:"unpacked,omitempty"`
	Size     int64 `json:"size,omitempty"`
	// Volumes is how many files the set is made of.
	Volumes int `json:"volumes"`
	// Parts is the task of every file in the set, in reading order, so each of
	// those rows shows the unpacking and not only the one it started on.
	Parts []string `json:"parts"`
	// Nested counts archives found inside the output and unpacked in turn.
	Nested int `json:"nested,omitempty"`
	// MovedTo is where the unpacked content was moved afterwards, or empty
	// when it stayed in Dir. It is kept on the job because the row outlives
	// the log.
	MovedTo string `json:"movedTo,omitempty"`
	// Moved counts what was moved: one for a whole folder, or one per entry
	// when the contents were moved.
	Moved int    `json:"moved,omitempty"`
	Error string `json:"error,omitempty"`
	// Password marks a failure caused by a missing password, so the interface
	// can offer to enter one and retry.
	Password  bool      `json:"password,omitempty"`
	QueuedAt  time.Time `json:"queuedAt"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

// extractJob adds what only this package holds: the archive path and the
// cancel handle.
type extractJob struct {
	ExtractJob
	path   string
	cancel context.CancelFunc
}

// unpackState is the extraction worker. All of it, busy included, is guarded
// by a.mu, so checking for a running worker and starting one is a single
// critical section. A job's cancel is set and cleared together with its
// Status, so a running job always has one.
type unpackState struct {
	jobs  map[string]*extractJob
	order []string
	busy  bool
}

// filesAreLocal reports whether t's bytes landed on this machine; JD downloads
// live on the JD machine. Every place that opens or renames a finished file
// asks this rather than checking the resolver itself.
func filesAreLocal(t *core.Task) bool { return t.Resolver != "jd" }

// extractWanted resolves the unpacking switch: the task's own setting (from a
// rule or the user), then its category, then the global setting. A category's
// Extract is a pointer so it can say no against a global yes.
func extractWanted(t *core.Task, cfg settings.Settings) bool {
	if t.AutoExtract != nil {
		return *t.AutoExtract
	}
	return cfg.ExtractFor(t.Category)
}

// setKey identifies the set a file belongs to: archive volumes, and plain
// split files ("film.mkv.001") that extract.SetKey does not count as an
// archive but that must be handled as one set all the same.
func setKey(name string) (string, bool) {
	if k, ok := extract.SetKey(name); ok {
		return k, true
	}
	if stem, _, ok := extract.SplitPart(name); ok {
		return strings.ToLower(stem) + "|split", true
	}
	return "", false
}

// extractCandidateLocked decides whether a finished download completes an
// archive and returns the task and path to open. For a multi-volume set that
// happens when the last part arrives, and the first volume is opened. Caller
// holds a.mu.
func (a *App) extractCandidateLocked(done *core.Task) (*core.Task, string) {
	key, isVolume := setKey(done.Name)
	if !isVolume {
		if extract.Startable(done.Name) {
			return done, a.fileOfLocked(done)
		}
		return nil, ""
	}
	// The set is identified by its destination and its names; the path uses
	// where the parts actually are, which differs when a working folder is set.
	dir := a.dirFor(done)
	set := a.membersLocked(key, dir)
	var first *core.Task
	for _, t := range set {
		if t.Status != core.StatusDone {
			// Missing or already extracting; the last part to finish starts it.
			return nil, ""
		}
		// In reading order, not name order: a spanned rar starts at "film.rar"
		// but a spanned zip ends at "film.zip".
		if extract.Startable(t.Name) && (first == nil || volumeBefore(t, first)) {
			first = t
		}
	}
	if first == nil {
		return nil, ""
	}
	if _, _, split := extract.SplitPart(first.Name); split && len(set) < 2 {
		// A single numbered file without siblings is not a split file.
		return nil, ""
	}
	return first, a.setPathLocked(first, len(set))
}

// volumeBefore orders two parts of one set the way the archive is read.
func volumeBefore(x, y *core.Task) bool {
	rx, ry := extract.VolumeRank(x.Name), extract.VolumeRank(y.Name)
	if rx != ry {
		return rx < ry
	}
	return x.Name < y.Name
}

// membersLocked returns every task in one set within one folder. Caller holds
// a.mu.
func (a *App) membersLocked(key, dir string) []*core.Task {
	var out []*core.Task
	for _, t := range a.tasks {
		if k, ok := setKey(t.Name); ok && k == key && a.dirFor(t) == dir {
			out = append(out, t)
		}
	}
	return out
}

// volumeSetLocked returns every part of t's archive, t included; a file that
// is not a part is a set of one. The folder is part of the identity, so two
// releases with the same part names in different packages stay apart. Caller
// holds a.mu.
func (a *App) volumeSetLocked(t *core.Task) []*core.Task {
	key, ok := setKey(t.Name)
	if !ok {
		return []*core.Task{t}
	}
	out := a.membersLocked(key, a.dirFor(t))
	if len(out) == 0 {
		// t may be a copy rather than the live task.
		return []*core.Task{t}
	}
	return out
}

// stampPartsLocked numbers the parts of t's set in reading order, so the list
// shows them as one archive, and returns the rows it changed. Caller holds
// a.mu.
func (a *App) stampPartsLocked(t *core.Task) []taskCopy {
	if t == nil {
		// Removed between queueing and running.
		return nil
	}
	set := a.volumeSetLocked(t)
	if len(set) < 2 {
		return nil
	}
	sort.Slice(set, func(i, j int) bool { return volumeBefore(set[i], set[j]) })
	var changed []taskCopy
	for i, part := range set {
		if part.ArchivePart == i+1 {
			continue
		}
		part.ArchivePart = i + 1
		changed = append(changed, a.copyLocked(part))
	}
	return changed
}

// extractionDueLocked returns the volume to open when a finished download
// completes an archive that should be unpacked. The switch is read from the
// first volume, not the finishing part, so the answer does not depend on which
// part arrived last. Caller holds a.mu.
func (a *App) extractionDueLocked(done *core.Task, cfg settings.Settings) (*core.Task, string) {
	target, path := a.extractCandidateLocked(done)
	if target == nil || !extractWanted(target, cfg) {
		return nil, ""
	}
	return target, path
}

// extractNowLocked starts a due extraction and returns the task it moved into
// StatusExtracting, which the caller must publish too since it may be the
// first volume rather than done. It runs when a download finishes and when the
// switch is turned on later; a second call finds the target already
// extracting. Caller holds a.mu.
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

// passwordsFor returns the passwords to try: the task's own first, then the
// global list. It is read when the job starts, so a password typed while jobs
// wait applies to all of them.
func (a *App) passwordsFor(t *core.Task) []string {
	var out []string
	if t != nil && t.Password != "" {
		out = append(out, t.Password)
	}
	return append(out, a.Settings.Get().ArchivePasswords...)
}

// extractOptionsFor builds one task's extraction options when the job starts,
// so settings changed while jobs wait apply to them. Templates are expanded
// here because internal/extract never sees a task.
func (a *App) extractOptionsFor(t *core.Task, cfg settings.Settings) extract.Options {
	o := extract.Options{
		Passwords:   a.passwordsFor(t),
		Collision:   extract.ParseCollision(cfg.ExtractCollision),
		Disposal:    extract.ParseDisposal(cfg.ArchiveDisposal),
		TrashRoot:   a.trashRootFor(t),
		TrashMaxAge: time.Duration(cfg.TrashRetentionDays) * 24 * time.Hour,
		InfoFiles:   cfg.DeleteInfoFiles,
	}
	if t != nil {
		o.Package = t.Package
	}
	// Where the extraction writes, which is not always where the result ends
	// up (see unpackPlanFor).
	plan := a.unpackPlanFor(t, cfg)
	o.Dest, o.Subfolder = plan.Dest, plan.Subfolder
	return o
}

// trashRootFor returns the folder trashed archives go to. With a working folder
// the archive lives on that filesystem and cannot be renamed into a trash on
// the destination's, so the trash follows it; that keeps a single trash for
// SweepTrash to age.
func (a *App) trashRootFor(t *core.Task) string {
	if root := a.workRoot(); root != "" && deliverable(t) {
		return root
	}
	return a.defaultDir()
}

// packageFilesLocked returns every file of t's package in the same folder,
// which scopes the info-file sweep. Deleting every .nfo in the folder would
// take the neighbours' too, since packages share a folder by default. Caller
// holds a.mu.
func (a *App) packageFilesLocked(t *core.Task) []string {
	if t == nil {
		return nil
	}
	if strings.TrimSpace(t.Package) == "" {
		// Without a package there is no scope; the empty name would match
		// every loose download in the folder.
		return nil
	}
	// Grouped by destination, listed where the files actually are.
	dir := a.dirFor(t)
	var out []string
	for _, other := range a.tasks {
		if other.Package != t.Package || other.Name == "" || a.dirFor(other) != dir {
			continue
		}
		out = append(out, a.fileOfLocked(other))
	}
	sort.Strings(out)
	return out
}

// disposable filters paths down to those no other task also points at. A
// mirror or a link added twice shares the file, and deleting it would leave
// the other row claiming a download that is gone. A path no task claims is a
// volume the reader pulled in and is disposable.
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
		// Where the file was written, because the paths come from
		// internal/extract, which saw the files there.
		claims[filepath.Clean(a.fileOfLocked(t))]++
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if claims[filepath.Clean(p)] <= 1 {
			out = append(out, p)
		}
	}
	return out
}

// unpackLocked returns the extraction worker's state, creating it on first
// use. Caller holds a.mu.
func (a *App) unpackLocked() *unpackState {
	if a.unpack == nil {
		a.unpack = &unpackState{jobs: map[string]*extractJob{}}
	}
	return a.unpack
}

// enqueueExtractLocked queues one archive and moves its task into
// StatusExtracting. It returns nil when that archive is already queued or
// running, so simultaneous triggers make one job. Caller holds a.mu.
func (a *App) enqueueExtractLocked(target *core.Task, path string) *extractJob {
	st := a.unpackLocked()
	for _, j := range st.jobs {
		if j.TaskID == target.ID && (j.Status == ExtractQueued || j.Status == ExtractRunning) {
			return nil
		}
	}
	set := a.volumeSetLocked(target)
	sort.Slice(set, func(i, j int) bool { return volumeBefore(set[i], set[j]) })
	parts := make([]string, len(set))
	for i, t := range set {
		parts[i] = t.ID
	}
	job := &extractJob{
		ExtractJob: ExtractJob{
			ID:       newID(),
			TaskID:   target.ID,
			Name:     filepath.Base(path),
			Dir:      filepath.Dir(path),
			Package:  target.Package,
			Status:   ExtractQueued,
			Volumes:  len(set),
			Parts:    parts,
			QueuedAt: time.Now(),
		},
		path: path,
	}
	target.Status = core.StatusExtracting
	// The last attempt's failure is stale while the archive is unpacked again,
	// and the row would show it beside the progress. A new failure writes its own.
	if strings.HasPrefix(target.Error, extractErrorPrefix) {
		target.Error = ""
	}
	st.jobs[job.ID] = job
	st.order = append(st.order, job.ID)
	a.pruneJobsLocked(st)
	if !st.busy {
		st.busy = true
		a.spawn(a.runExtractions)
	}
	return job
}

// pruneJobsLocked drops the oldest finished jobs beyond keptExtractJobs. Live
// jobs are never dropped.
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

// runExtractions is the single unpacking goroutine; two archives on one disk
// only slow each other down. internal/extract also serialises, but this queue
// is what the list shows, with an order and a cancel for waiting jobs.
func (a *App) runExtractions() {
	for {
		a.mu.Lock()
		st := a.unpackLocked()
		job := nextQueuedLocked(st)
		if job == nil {
			// Cleared under the lock the next enqueue takes, so a new job either
			// sees this worker or starts one.
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
		parts := a.beginUnpackLocked(job, target)
		// Checked when the job starts rather than when it was queued, since a
		// part can finish again in between.
		var refused error
		if target != nil {
			refused = a.volumeMismatchLocked(target)
		}
		snap := job.ExtractJob
		path := job.path
		a.mu.Unlock()

		a.publishTasks(parts)
		a.Hub.Broadcast("extract", snap)

		id := snap.ID
		if refused != nil {
			cancel()
			a.settleExtraction(id, opts, siblings, nil, refused)
			continue
		}
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

// beginUnpackLocked readies the parts of a job about to run and returns the
// rows it changed. They are numbered in reading order (stampPartsLocked), and
// they forget how the last unpacking ended, which stops being true once this
// one writes: cancelled or cut off by a restart, it leaves no result. Caller
// holds a.mu.
func (a *App) beginUnpackLocked(job *extractJob, target *core.Task) []taskCopy {
	forgot := map[string]bool{}
	for _, id := range job.Parts {
		if t := a.tasks[id]; t != nil && t.Unpack != core.UnpackNone {
			t.Unpack = core.UnpackNone
			forgot[id] = true
		}
	}
	changed := a.stampPartsLocked(target)
	for _, c := range changed {
		delete(forgot, c.ID)
	}
	for _, id := range job.Parts {
		if forgot[id] {
			changed = append(changed, a.copyLocked(a.tasks[id]))
		}
	}
	return changed
}

// nextQueuedLocked returns the oldest waiting job. Caller holds a.mu.
func nextQueuedLocked(st *unpackState) *extractJob {
	for _, id := range st.order {
		if j := st.jobs[id]; j != nil && j.Status == ExtractQueued {
			return j
		}
	}
	return nil
}

// publishExtractProgress copies the worker's progress onto the job and
// broadcasts it. internal/extract throttles the callback.
func (a *App) publishExtractProgress(jobID string, p extract.Progress) {
	a.mu.Lock()
	j := a.unpackLocked().jobs[jobID]
	if j == nil || j.Status != ExtractRunning {
		a.mu.Unlock()
		return
	}
	j.Archive, j.Depth, j.Files, j.Bytes = p.Archive, p.Depth, p.Files, p.Bytes
	j.Unpacked, j.Size = p.Unpacked, p.Size
	snap := j.ExtractJob
	a.mu.Unlock()
	a.Hub.Broadcast("extract", snap)
}

// extractionTaskID returns the task an extraction job belongs to, or "" when
// the job is gone.
func (a *App) extractionTaskID(jobID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if j := a.unpackLocked().jobs[jobID]; j != nil {
		return j.TaskID
	}
	return ""
}

// settleExtraction records how a job ended, returns the task to done, disposes
// of the volumes and moves the unpacked content where it belongs. Disposal
// happens only on success, since a failed extraction is when the volumes are
// needed again. The move comes after disposal and before the job settles, so
// the row shows where the files ended up.
func (a *App) settleExtraction(jobID string, opts extract.Options, siblings []string, out *extract.Outcome, err error) {
	cancelled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	// Read first so the disposal failures below can be tagged with the task for
	// the per-download log card.
	taskID := a.extractionTaskID(jobID)
	var moved delivery
	if err == nil && out != nil {
		if derr := opts.Dispose(a.disposable(out.Volumes)); derr != nil {
			log.Printf("extraction finished but the archive could not be disposed of: %v%s", derr, taskTag(taskID))
		}
		if derr := opts.Dispose(a.disposable(opts.InfoFilesIn(siblings))); derr != nil {
			log.Printf("extraction finished but the info files could not be disposed of: %v%s", derr, taskTag(taskID))
		}
		moved = a.deliverExtraction(jobID, out)
	}
	// Swept on every settle, so the trash still ages after the disposal is
	// switched away from it. Without retention or a trash folder it does
	// nothing.
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
	result := core.UnpackNone
	switch {
	case cancelled:
		j.Status = ExtractCancelled
	case err != nil:
		j.Status = ExtractFailed
		j.Error = err.Error()
		j.Password = errors.Is(err, extract.ErrPasswordRequired)
		result = core.UnpackFailed
		if j.Password {
			result = core.UnpackPassword
		}
	default:
		j.Status = ExtractDone
		result = core.UnpackDone
		j.Error = ""
		if out != nil {
			j.Files, j.Bytes, j.Nested = out.Files, out.Bytes, out.Nested
			if out.Dir != "" {
				j.Dir = out.Dir
			}
		}
		// Done with an error means the archive unpacked but the move failed;
		// both are true, so both are recorded.
		if moved.Err != nil {
			j.Error = moved.Err.Error()
		}
		j.MovedTo, j.Moved = moved.Dir, moved.Entries
		j.Archive = ""
	}
	snap := j.ExtractJob

	touched := map[string]bool{}
	if t := a.tasks[j.TaskID]; t != nil {
		// The download itself finished, whatever the archive did.
		if t.Status == core.StatusExtracting {
			t.Status = core.StatusDone
		}
		// Only this package's own error is cleared.
		if strings.HasPrefix(t.Error, extractErrorPrefix) {
			t.Error = ""
		}
		if err != nil && !cancelled {
			t.Error = extractErrorPrefix + err.Error()
		} else if moved.Err != nil {
			// Under the extraction's prefix, so the next extraction of the same
			// archive clears it.
			t.Error = extractErrorPrefix + moved.Err.Error()
		}
		touched[t.ID] = true
	}
	// On every part, since each row shows the unpacking, and after a restart
	// no job is left to say it.
	if !cancelled {
		for _, id := range j.Parts {
			if t := a.tasks[id]; t != nil && t.Unpack != result {
				t.Unpack = result
				touched[id] = true
			}
		}
	}
	settled := make([]taskCopy, 0, len(touched))
	for id := range touched {
		settled = append(settled, a.copyLocked(a.tasks[id]))
	}
	a.mu.Unlock()

	a.publishTasks(settled)
	a.Hub.Broadcast("extract", snap)
	// The remaining volumes may leave the working folder only after extraction.
	a.deliverVolumes(snap.TaskID)
	// Nothing else ever sweeps the working folder.
	a.sweepWorkRoot()
	// A cancelled extraction does not fire: the user just pressed the button.
	if !cancelled {
		a.fireExtractDone(snap, snap.Status == ExtractFailed)
	}
}

// extractErrorPrefix marks the errors this package puts on a task, so a
// successful retry clears only its own.
const extractErrorPrefix = "extract: "

// ExtractJobs returns every unpacking the app knows about, oldest first.
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

// StartExtraction unpacks finished downloads on demand, for example after a
// wrong password or a full disk. The unpacking switch is not consulted, since
// pressing the button is the answer to it.
func (a *App) StartExtraction(ids []string) error {
	var refused []string

	a.mu.Lock()
	var started []taskCopy
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
			continue // already queued
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
			started = append(started, a.copyLocked(target))
		}
	}
	a.mu.Unlock()
	a.publishTasks(started)

	if len(refused) > 0 {
		return errors.New(strings.Join(refused, "; "))
	}
	return nil
}

// reasonNotAnArchive tells an unfinished set, which unpacks by itself once its
// last part lands, from a file that is no archive at all.
func reasonNotAnArchive(t *core.Task, set []*core.Task) string {
	for _, part := range set {
		if part.Status != core.StatusDone {
			return fmt.Sprintf("%s is one part of a set and %s has not finished", t.Name, part.Name)
		}
	}
	return fmt.Sprintf("%s is not an archive this build can open", t.Name)
}

// AbortExtraction cancels an unpacking. The worker removes its half-written
// output, since only it knows which files it created, and a leftover folder
// would look like a finished extraction.
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
		// The worker settles the job when Run returns cancelled; settling it
		// here too would give one job two endings.
		j.cancel()
		a.mu.Unlock()
		return nil
	case ExtractQueued:
		j.Status = ExtractCancelled
		j.EndedAt = time.Now()
		snap := j.ExtractJob
		var settled *taskCopy
		if t := a.tasks[j.TaskID]; t != nil && t.Status == core.StatusExtracting {
			// Nothing ran, so only the row needs to go back to done.
			t.Status = core.StatusDone
			c := a.copyLocked(t)
			settled = &c
		}
		a.mu.Unlock()
		if settled != nil {
			a.publish(settled)
		}
		a.Hub.Broadcast("extract", snap)
		return nil
	default:
		a.mu.Unlock()
		return fmt.Errorf("%s has already finished unpacking", j.Name)
	}
}
