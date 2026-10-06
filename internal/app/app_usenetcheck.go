package app

// The par2 check of a release fetched from the own Usenet servers. Once every
// file of the job is here, the release's par2 files say whether it is whole.
// A file posted under another name is renamed to the one the set gives it,
// the recovery volumes held back are fetched as far as a repair needs them,
// and the repair runs. Until the release has passed, none of it is unpacked or
// moved out of the working folder. A release beyond repair goes on to the next
// account like one with articles missing, or fails.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/par2"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
)

// checkMemory caps what a repair holds at once.
const checkMemory = 256 << 20

// checkProgressEvery is how often a running check tells the list how far it
// has got.
const checkProgressEvery = 500 * time.Millisecond

// checkWorkers is how many cores a check uses: half of them, since the
// downloads and whatever else the machine serves go on meanwhile.
func checkWorkers() int { return max(1, runtime.NumCPU()/2) }

// usenetCheck is one job's check. Its fields are guarded by a.mu.
type usenetCheck struct {
	remote  string
	outcome usenet.Check
	// running is a check under way, and again a reason to go round once
	// more when it ends, such as a file that finished meanwhile.
	running, again bool
	// damaged holds the tasks whose files are known to be damaged: they
	// arrived with gaps, or a verify found bad blocks in them. A job handed
	// on keeps none of them.
	damaged map[string]bool
	// lost is how many bytes each damaged file that is not a par2 file
	// lacks, for the early estimate.
	lost map[string]int64
	// report and paths are the last verify's, kept while recovery volumes
	// come so the files are not read again.
	report *par2.Report
	paths  []string
	// distrust is set once a file taken as whole from its download turned
	// out not to be, after which every file is read.
	distrust bool
	// shown is what the job's files show of the check.
	shown *core.RepairProgress
	// stop ends the check under way, and ended is closed once it has.
	stop  context.CancelFunc
	ended chan struct{}
}

// usenetCheckLocked returns the check of the job the own servers know as
// remote, or nil when they do not hold such a job. Caller holds a.mu.
func (a *App) usenetCheckLocked(remote string) *usenetCheck {
	st := a.usenetStateFor()
	if c := st.checks[remote]; c != nil {
		return c
	}
	j, ok := st.jobs.Lookup(local.ResolverID, remote)
	if !ok {
		return nil
	}
	c := &usenetCheck{remote: remote, outcome: j.Check, damaged: map[string]bool{}, lost: map[string]int64{}}
	st.checks[remote] = c
	return c
}

// usenetHeldLocked reports whether t is a file of a release from the own
// Usenet servers that has not passed its par2 check, which keeps it from being
// unpacked and moved. Caller holds a.mu.
func (a *App) usenetHeldLocked(t *core.Task) bool {
	job := local.LinkJob(t.URL)
	if job == "" {
		return false
	}
	c := a.usenetCheckLocked(job)
	return c != nil && (c.outcome == usenet.CheckPending || c.outcome == usenet.CheckFailed)
}

// usenetDamagedLocked reports whether t's file is known to be damaged, so a
// job handed on does not keep it. Caller holds a.mu.
func (a *App) usenetDamagedLocked(t *core.Task) bool {
	c := a.usenetStateFor().checks[local.LinkJob(t.URL)]
	return c != nil && c.damaged[t.ID]
}

// noteUsenetDoneLocked marks a file of a release still owing its check as
// waiting for it, in the same breath as it is marked done, so the SABnzbd
// bridge never reports the release complete in between. It reports whether a
// check is owed. Caller holds a.mu.
func (a *App) noteUsenetDoneLocked(t *core.Task) bool {
	job := local.LinkJob(t.URL)
	if job == "" {
		return false
	}
	c := a.usenetCheckLocked(job)
	if c == nil || c.outcome != usenet.CheckPending {
		return false
	}
	switch {
	case c.shown != nil:
		t.Repair = c.shown
	case t.Repair == nil:
		t.Repair = &core.RepairProgress{Stage: core.RepairWaiting}
	}
	return true
}

// resumeUsenetChecks takes up the checks a restart interrupted. The files
// waiting for one are marked before anything can read the list.
func (a *App) resumeUsenetChecks() {
	st := a.usenetStateFor()
	var remotes []string
	a.mu.Lock()
	for _, j := range st.jobs.Jobs() {
		if j.Service != local.ResolverID || j.State != usenet.StateStaged || j.Check != usenet.CheckPending {
			continue
		}
		remotes = append(remotes, j.Remote)
		for _, id := range j.TaskIDs {
			if t := a.tasks[id]; t != nil && t.Status == core.StatusDone {
				t.Repair = &core.RepairProgress{Stage: core.RepairWaiting}
			}
		}
	}
	a.mu.Unlock()
	for _, r := range remotes {
		a.spawn(func() { a.checkUsenetJob(r) })
	}
}

// checkUsenetJob runs the check of the job the own servers know as remote.
// While one is under way it only asks that one to go round again.
func (a *App) checkUsenetJob(remote string) {
	a.mu.Lock()
	c := a.usenetCheckLocked(remote)
	if c == nil || c.outcome != usenet.CheckPending {
		a.mu.Unlock()
		return
	}
	if c.running {
		c.again = true
		a.mu.Unlock()
		return
	}
	ctx, stop := context.WithCancel(a.ctx)
	ended := make(chan struct{})
	c.running, c.stop, c.ended = true, stop, ended
	a.mu.Unlock()
	defer func() {
		stop()
		close(ended)
	}()
	for {
		a.checkUsenetOnce(ctx, c)
		a.mu.Lock()
		again := c.again && c.outcome == usenet.CheckPending && ctx.Err() == nil
		c.again = false
		if !again {
			c.running = false
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()
	}
}

// haltUsenetCheck ends the check of the job the own servers know as remote and
// returns once it has let go of the files, which Windows does not delete while
// they are open.
func (a *App) haltUsenetCheck(remote string) {
	a.mu.Lock()
	c := a.usenetStateFor().checks[remote]
	if c == nil || !c.running {
		a.mu.Unlock()
		return
	}
	stop, ended := c.stop, c.ended
	a.mu.Unlock()
	stop()
	<-ended
}

// checkFile is one finished file of a job as the check sees it.
type checkFile struct {
	id, path string
	whole    bool
}

// spareVolume is a recovery volume held back, with the blocks its name says
// it holds.
type spareVolume struct {
	id     string
	blocks int
}

func (a *App) checkUsenetOnce(ctx context.Context, c *usenetCheck) {
	st := a.usenetStateFor()
	j, ok := st.jobs.Lookup(local.ResolverID, c.remote)
	if !ok || j.State != usenet.StateStaged {
		return
	}
	var (
		done  []checkFile
		spare []spareVolume
		ready = true
	)
	a.mu.Lock()
	for _, id := range j.TaskIDs {
		t := a.tasks[id]
		switch {
		case t == nil:
		case HeldSpare(t):
			spare = append(spare, spareVolume{id, par2.VolumeBlocks(t.Name)})
		case t.Status == core.StatusDone:
			done = append(done, checkFile{id: id, path: a.fileOfLocked(t)})
		case t.Status == core.StatusError && t.NextTry.IsZero():
			// Failed for good, so its file counts as missing.
		default:
			ready = false
		}
	}
	a.mu.Unlock()
	if !ready {
		return
	}

	// One check at a time: each reads every file of its release.
	a.showRepair(c, j.TaskIDs, &core.RepairProgress{Stage: core.RepairWaiting})
	select {
	case st.checkSlot <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-st.checkSlot }()

	var index, data []checkFile
	for _, f := range done {
		f.whole = st.articles.Whole(f.id)
		if isPar2File(f.path) {
			index = append(index, f)
		} else {
			data = append(data, f)
		}
	}
	if len(index) == 0 {
		a.mu.Lock()
		damaged := len(c.damaged) > 0
		a.mu.Unlock()
		if damaged {
			a.failUsenetJob(c, j, "some articles of this release are missing, and it came without par2 files to repair it")
			return
		}
		a.passUsenetCheck(c, j, "it came without par2 files", nil)
		return
	}
	set, err := par2.Load(pathsOf(index)...)
	if err != nil {
		if len(spare) > 0 {
			smallest := slices.MinFunc(spare, func(x, y spareVolume) int { return x.blocks - y.blocks })
			log.Printf("usenet: %s: the par2 index is unreadable (%v), fetching a recovery volume", j.Name, err)
			a.fetchVolumes(c, j, []spareVolume{smallest}, &core.RepairProgress{Stage: core.RepairFetching})
			return
		}
		a.failUsenetJob(c, j, "its par2 files are damaged beyond use: "+err.Error())
		return
	}

	paths := a.renameToSet(set, set.Match(pathsOf(data)), data)
	a.mu.Lock()
	distrust := c.distrust
	rep := c.report
	if !slices.Equal(c.paths, paths) {
		rep = nil
	}
	a.mu.Unlock()
	trusted := map[string]bool{}
	for _, f := range data {
		if f.whole && !distrust {
			trusted[f.path] = true
		}
	}
	opts := par2.Options{
		Workers: checkWorkers(),
		Memory:  checkMemory,
		Trusted: func(p string) bool { return trusted[p] },
	}

	if rep == nil {
		opts.Progress = a.repairMeter(c, j.TaskIDs, core.RepairVerifying, 0, 0)
		a.showRepair(c, j.TaskIDs, &core.RepairProgress{Stage: core.RepairVerifying})
		if rep, err = par2.Verify(ctx, set, paths, opts); err != nil {
			if ctx.Err() == nil {
				a.failUsenetJob(c, j, "the par2 check could not read the files: "+err.Error())
			}
			return
		}
	}
	if rep.Whole(set) {
		a.passUsenetCheck(c, j, "it is whole", setFiles(set, rep))
		return
	}

	damaged := rep.Damaged()
	a.mu.Lock()
	for i, fr := range rep.Files {
		if len(fr.Bad) == 0 && fr.Size == set.Files[i].Size {
			continue
		}
		if k := indexOfPath(data, paths[i]); k >= 0 {
			c.damaged[data[k].id] = true
		}
	}
	c.report, c.paths = rep, paths
	a.mu.Unlock()

	have := len(set.Recovery)
	held := 0
	for _, v := range spare {
		held += v.blocks
	}
	if damaged > have+held {
		a.failUsenetJob(c, j, notRepairable(damaged, have+held))
		return
	}
	if damaged > have {
		pick := pickVolumes(spare, damaged-have)
		coming := 0
		for _, v := range pick {
			coming += v.blocks
		}
		log.Printf("usenet: %s: %d blocks are damaged, %d recovery blocks are here, fetching %d more", j.Name, damaged, have, coming)
		a.fetchVolumes(c, j, pick, &core.RepairProgress{Stage: core.RepairFetching, Damaged: damaged, Recovery: have + coming})
		return
	}

	opts.Progress = a.repairMeter(c, j.TaskIDs, core.RepairRepairing, damaged, have)
	a.showRepair(c, j.TaskIDs, &core.RepairProgress{Stage: core.RepairRepairing, Damaged: damaged, Recovery: have})
	err = par2.Repair(ctx, set, rep, filepath.Dir(index[0].path), opts)
	if errors.Is(err, par2.ErrMismatch) && len(trusted) > 0 {
		// A file taken as whole from its download was not, so the check
		// starts over and reads every file.
		log.Printf("usenet: %s: %v, checking every file in full", j.Name, err)
		a.mu.Lock()
		c.distrust, c.again = true, true
		c.report, c.paths = nil, nil
		a.mu.Unlock()
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		var short *par2.NotEnoughError
		if errors.As(err, &short) {
			a.failUsenetJob(c, j, notRepairable(short.Damaged, short.Recovery))
			return
		}
		a.failUsenetJob(c, j, "the par2 repair failed: "+err.Error())
		return
	}
	log.Printf("usenet: %s: %d damaged blocks repaired", j.Name, damaged)
	a.passUsenetCheck(c, j, "it was repaired", setFiles(set, rep))
}

// setFiles maps the name of each file of a checked set to where it is.
func setFiles(set *par2.Set, rep *par2.Report) map[string]string {
	out := map[string]string{}
	for i, f := range set.Files {
		if p := rep.Files[i].Path; p != "" {
			out[collide.SafeName(f.BaseName())] = p
		}
	}
	return out
}

func notRepairable(damaged, recovery int) string {
	return fmt.Sprintf("%d blocks of this release are damaged or missing, and its par2 files hold %d recovery blocks", damaged, recovery)
}

func pathsOf(files []checkFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out
}

func indexOfPath(files []checkFile, p string) int {
	for i, f := range files {
		if samePath(f.path, p) {
			return i
		}
	}
	return -1
}

// isPar2File reports whether the file at path is a par2 file by its first
// bytes, which finds one posted under a random name too.
func isPar2File(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	return par2.IsPar2(head)
}

// pickVolumes chooses held volumes holding at least need blocks between them,
// with little to spare: the smallest that covers what is left, or else the
// largest and on from there.
func pickVolumes(spare []spareVolume, need int) []spareVolume {
	left := slices.Clone(spare)
	sort.Slice(left, func(i, k int) bool { return left[i].blocks < left[k].blocks })
	var out []spareVolume
	for need > 0 && len(left) > 0 {
		i := sort.Search(len(left), func(i int) bool { return left[i].blocks >= need })
		if i == len(left) {
			i = len(left) - 1
		}
		out = append(out, left[i])
		need -= left[i].blocks
		left = slices.Delete(left, i, i+1)
	}
	return out
}

// fetchVolumes switches held recovery volumes on and starts them. The check
// goes on once they are here, as every finished file asks it to.
func (a *App) fetchVolumes(c *usenetCheck, j usenet.Job, pick []spareVolume, p *core.RepairProgress) {
	ids := make([]string, len(pick))
	for i, v := range pick {
		ids[i] = v.id
	}
	a.showRepair(c, j.TaskIDs, p)
	a.SetEnabled(ids, true)
	a.StartTasks(ids)
}

// renameToSet gives every file the set recognised under another name the
// name the set has for it, and returns the paths as they are afterwards. Each
// goes through a name of its own first, so two files posted under each
// other's names swap. A file whose name is taken by a file outside the
// release keeps its own.
func (a *App) renameToSet(set *par2.Set, found []string, data []checkFile) []string {
	out := slices.Clone(found)
	type move struct {
		i        int
		id       string
		from, to string
		tmp      string
	}
	var moves []move
	for i, p := range found {
		if p == "" {
			continue
		}
		want := filepath.Join(filepath.Dir(p), collide.SafeName(set.Files[i].BaseName()))
		if samePath(p, want) {
			continue
		}
		k := indexOfPath(data, p)
		if k < 0 {
			continue
		}
		moves = append(moves, move{i: i, id: data[k].id, from: p, to: want, tmp: p + ".klrename"})
	}
	if len(moves) == 0 {
		return out
	}
	leaving := map[string]bool{}
	for _, m := range moves {
		leaving[filepath.Clean(m.from)] = true
	}
	a.mu.Lock()
	var copies []taskCopy
	var started []move
	for _, m := range moves {
		if _, err := os.Lstat(m.to); err == nil && !leaving[filepath.Clean(m.to)] {
			log.Printf("usenet: %s stays as it is, %s is taken", m.from, filepath.Base(m.to))
			continue
		}
		if err := os.Rename(m.from, m.tmp); err != nil {
			log.Printf("usenet: %s could not be renamed: %v", m.from, err)
			continue
		}
		started = append(started, m)
	}
	for _, m := range started {
		final := m.to
		if err := os.Rename(m.tmp, m.to); err != nil {
			log.Printf("usenet: %s could not be renamed: %v", m.from, err)
			if os.Rename(m.tmp, m.from) != nil {
				final = m.tmp
			} else {
				final = m.from
			}
		}
		out[m.i] = final
		data[indexOfPath(data, m.from)].path = final
		t := a.tasks[m.id]
		if t == nil {
			continue
		}
		log.Printf("usenet: %s is %s by its par2 set", t.Name, filepath.Base(final))
		t.Name = filepath.Base(final)
		if t.File != "" {
			t.File = final
		}
		a.noteMovedLocked(t.ID)
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(copies)
	return out
}

// showRepair puts p on the finished files among ids, as a new value each
// time since the copies already sent share the old one. A file that finishes
// later takes it up from c.
func (a *App) showRepair(c *usenetCheck, ids []string, p *core.RepairProgress) {
	a.mu.Lock()
	c.shown = p
	var copies []taskCopy
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusDone {
			continue
		}
		t.Repair = p
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	for i := range copies {
		a.show(&copies[i])
	}
}

// repairMeter turns a check's progress into broadcasts, a few a second.
func (a *App) repairMeter(c *usenetCheck, ids []string, stage core.RepairStage, damaged, recovery int) func(done, total int64) {
	var last time.Time
	return func(done, total int64) {
		if total <= 0 || done < total && time.Since(last) < checkProgressEvery {
			return
		}
		last = time.Now()
		a.showRepair(c, ids, &core.RepairProgress{
			Stage: stage, Progress: float64(done) / float64(total), Damaged: damaged, Recovery: recovery,
		})
	}
}

// passUsenetCheck records a release as whole and lets it go on: what is due
// to be unpacked is, and the rest leaves the working folder. files maps the
// names of the par2 set's files to where they are, so a download that failed
// for good but whose file the repair rebuilt counts as done.
func (a *App) passUsenetCheck(c *usenetCheck, j usenet.Job, why string, files map[string]string) {
	st := a.usenetStateFor()
	st.jobs.SetCheck(j.ID, usenet.CheckPassed)
	log.Printf("usenet: %s passed its par2 check: %s", j.Name, why)
	a.mu.Lock()
	c.outcome = usenet.CheckPassed
	c.report, c.paths, c.shown = nil, nil, nil
	clear(c.damaged)
	a.mu.Unlock()
	a.adoptRebuilt(j.TaskIDs, files)
	a.releaseUsenetFiles(j.TaskIDs)
}

// adoptRebuilt marks done the failed downloads among ids whose files the set
// names and the repair rebuilt, and deletes what the failed attempt left.
func (a *App) adoptRebuilt(ids []string, files map[string]string) {
	var copies []taskCopy
	var parts []string
	a.mu.Lock()
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusError || !t.NextTry.IsZero() {
			continue
		}
		path, ok := files[collide.SafeName(local.LinkName(t.URL))]
		if !ok {
			path, ok = files[t.Name]
		}
		fi, err := os.Stat(path)
		if !ok || err != nil {
			continue
		}
		parts = append(parts, local.PartFile(a.dirFor(t), t.URL, t.ID))
		log.Printf("usenet: %s failed to download, and its par2 set rebuilt it", t.Name)
		t.Status = core.StatusDone
		t.Name, t.File = filepath.Base(path), path
		t.Size, t.Loaded = fi.Size(), fi.Size()
		t.ClearFailure()
		t.Retries, t.MaxTries, t.GaveUp = 0, 0, false
		t.Online = core.AvailOnline
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(copies)
	for _, p := range parts {
		local.RemovePart(p)
	}
}

// releaseUsenetFiles lets the files among ids go on once nothing holds them
// for a check any more: what is due to be unpacked is, and the rest leaves
// the working folder.
func (a *App) releaseUsenetFiles(ids []string) {
	cfg := a.Settings.Get()
	a.mu.Lock()
	touched := map[string]*core.Task{}
	var deliver []string
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if t.Repair != nil {
			t.Repair = nil
			touched[id] = t
		}
		if t.Status != core.StatusDone {
			continue
		}
		deliver = append(deliver, id)
		if target := a.extractNowLocked(t, cfg); target != nil {
			touched[target.ID] = target
		}
	}
	copies := make([]taskCopy, 0, len(touched))
	for _, t := range touched {
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(copies)
	for _, id := range deliver {
		a.spawn(func() {
			a.delivering.Add(1)
			defer a.delivering.Add(-1)
			a.deliverDownload(id)
		})
	}
}

// failUsenetJob hands a release that cannot be repaired to the next account,
// or fails it: its damaged files, the downloads of it still under way, and
// its par2 index when no file of its own is to blame, so the list and Sonarr
// both see why.
func (a *App) failUsenetJob(c *usenetCheck, j usenet.Job, reason string) {
	st := a.usenetStateFor()
	if ids, ok := st.jobs.Fallback(local.ResolverID, c.remote, reason); ok {
		a.mu.Lock()
		delete(st.checks, c.remote)
		a.mu.Unlock()
		a.RemoveTasks(ids, true)
		a.releaseUsenetFiles(j.TaskIDs)
		return
	}
	st.jobs.SetCheck(j.ID, usenet.CheckFailed)
	log.Printf("usenet: %s cannot be repaired: %s", j.Name, reason)

	var under []string
	a.mu.Lock()
	for _, id := range j.TaskIDs {
		if t := a.tasks[id]; t != nil && !HeldSpare(t) && (t.Status == core.StatusRunning || t.Status == core.StatusQueued) {
			under = append(under, id)
		}
	}
	a.mu.Unlock()
	a.PauseTasks(under)

	a.mu.Lock()
	c.outcome = usenet.CheckFailed
	c.report, c.paths, c.shown = nil, nil, nil
	blamed := map[string]bool{}
	for _, id := range under {
		blamed[id] = true
	}
	for id := range c.damaged {
		blamed[id] = true
	}
	if len(c.damaged) == 0 {
		for _, id := range j.TaskIDs {
			if t := a.tasks[id]; t != nil && t.Status == core.StatusDone && strings.EqualFold(filepath.Ext(t.Name), ".par2") && par2.VolumeBlocks(t.Name) == 0 {
				blamed[id] = true
			}
		}
	}
	var copies []taskCopy
	for _, id := range j.TaskIDs {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		t.Repair = nil
		if blamed[id] && (t.Status == core.StatusDone || t.Status == core.StatusPaused) {
			t.Status = core.StatusError
			t.Reason = core.ReasonGone
			t.SetError(reason, "", nil)
			t.GaveUp = true
			t.NextTry = time.Time{}
		}
		copies = append(copies, a.copyLocked(t))
	}
	a.mu.Unlock()
	a.publishTasks(copies)
}

// usenetBeyondRepair is the early verdict on a release whose files are still
// coming. With its par2 index here, the blocks its damaged files lack so far
// can be counted, at least one for each, and once they are more than every
// recovery volume of the release holds, no repair can work and the rest need
// not be fetched. It returns why, or "" while a repair may still work.
func (a *App) usenetBeyondRepair(c *usenetCheck) string {
	j, ok := a.usenetStateFor().jobs.Lookup(local.ResolverID, c.remote)
	if !ok {
		return ""
	}
	var index []string
	volumes := 0
	a.mu.Lock()
	lost := make([]int64, 0, len(c.lost))
	for _, l := range c.lost {
		lost = append(lost, l)
	}
	for _, id := range j.TaskIDs {
		t := a.tasks[id]
		if t == nil || !strings.EqualFold(filepath.Ext(t.Name), ".par2") {
			continue
		}
		if t.Status == core.StatusDone && !c.damaged[id] {
			index = append(index, a.fileOfLocked(t))
		} else {
			volumes += par2.VolumeBlocks(t.Name)
		}
	}
	a.mu.Unlock()
	if len(index) == 0 {
		return ""
	}
	set, err := par2.Load(index...)
	if err != nil {
		return ""
	}
	need := 0
	for _, l := range lost {
		need += max(1, int((l+set.SliceSize-1)/set.SliceSize))
	}
	if have := len(set.Recovery) + volumes; need > have {
		return fmt.Sprintf("at least %d blocks of this release are missing, and its par2 files hold %d recovery blocks", need, have)
	}
	return ""
}

// releaseHasPar2 reports whether the .nzb of a job the own servers took lists
// a par2 file, without which a file with gaps cannot be repaired.
func (a *App) releaseHasPar2(job string) bool {
	files, err := a.usenetStateFor().own.Files(job)
	if err != nil {
		return false
	}
	for _, f := range files {
		if strings.EqualFold(filepath.Ext(f.Name), ".par2") {
			return true
		}
	}
	return false
}
