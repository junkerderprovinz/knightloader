package app

// Free space on the target folders as a readout. The guard in app_diskguard.go
// asks the same question but only acts on it, so without this page nobody can
// see which folder is tight or how much the queue still wants to write.
//
// Each folder has three possible answers: measured; unknown, because the
// platform cannot be asked (the byte counts then mean nothing); or measured at
// the nearest existing parent because the folder does not exist yet. The last
// is normal for a fresh download folder but also what a mount that did not come
// up looks like, so both paths are reported.
//
// Nothing here writes. settings.Validate drops a probe into each folder, so
// only os.Stat is used.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/diskspace"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// diskUsage is a variable so tests can simulate full disks and platforms that
// cannot measure. Only tests write it.
var diskUsage = diskspace.Usage

// diskReportTTL is how long one reading is shared. Every open tab polls the
// route, and the guard itself only looks every fifteen seconds.
const diskReportTTL = 5 * time.Second

// maxQueueVolumes caps how many folders outside the configured ones the queue
// may add to one report. Configured folders are always reported; the rows kept
// are those owed the most, and Truncated records the cut.
const maxQueueVolumes = 16

// Roles a folder can appear in the list for. They are stable ids that the
// client translates.
const (
	roleDownloads = "downloads"
	roleCategory  = "category"
	roleWork      = "work"
	roleTask      = "task"
)

// VolumeReport is one target folder as the server measured it.
type VolumeReport struct {
	// Dir is the folder the app would write into, possibly not yet existing.
	// For a path template it is the template's fixed head (see
	// settings.FixedPrefix).
	Dir string `json:"dir"`
	// Measured is the folder the figures describe: Dir when it exists, else
	// its nearest existing parent. When a mount failed this is the volume root
	// of a different disk, so clients must show the difference.
	Measured string `json:"measured"`
	// Exists reports whether Dir itself is currently a directory.
	Exists bool `json:"exists"`
	// Known reports whether the platform could be asked. When false, Free,
	// Used and Total are 0 and mean nothing.
	Known bool `json:"known"`
	// Free is what this process may still write here, excluding root's reserve
	// on unix and honouring quotas on Windows.
	Free uint64 `json:"free"`
	// Used is what files occupy.
	Used uint64 `json:"used"`
	// Total is the volume size. Free plus Used can be less, by the root
	// reserve or a quota (see diskspace.Space).
	Total uint64 `json:"total"`
	// Queued is what the owed downloads would still add here: announced size
	// minus bytes received, per task. It is a floor, since tasks of unknown
	// size add nothing. It must not be subtracted from Free, because the engine
	// pre-allocates running downloads and their room is already gone from Free.
	Queued int64 `json:"queued"`
	// Tasks counts every enabled download still owed here, including those of
	// unknown size, so a queue of unchecked links does not read as empty.
	Tasks int `json:"tasks"`
	// Role is why this folder is listed, as a role id.
	Role string `json:"role"`
}

// DiskReport is every target folder at one moment.
type DiskReport struct {
	// Volumes has one row per folder, never nil. Several folders often share a
	// disk and there is no volume identity to detect it, so rows must never be
	// added up.
	Volumes []VolumeReport `json:"volumes"`
	// Truncated says the queue named more outside folders than maxQueueVolumes
	// and the smallest were left out.
	Truncated bool `json:"truncated"`
	// SampledAt is when the reading was taken. Readings are shared for a few
	// seconds, so clients should not present it as a live gauge.
	SampledAt time.Time `json:"sampledAt"`
}

// DiskReport returns each target folder's free space and what the queue still
// wants to write there. A reading is shared for diskReportTTL, and callers
// arriving during a new measurement get the previous one: statting a dead mount
// can take as long as its timeout, and neither piling goroutines into it nor
// blocking every tab behind it is acceptable. Only the very first caller waits.
func (a *App) DiskReport() DiskReport {
	st := a.diskReportStateFor()
	st.mu.Lock()
	fresh := !st.at.IsZero() && time.Since(st.at) < diskReportTTL
	if fresh {
		rep := st.report
		st.mu.Unlock()
		return rep
	}
	if ch := st.inflight; ch != nil {
		if !st.at.IsZero() {
			rep := st.report
			st.mu.Unlock()
			return rep
		}
		st.mu.Unlock()
		<-ch
		st.mu.Lock()
		rep := st.report
		st.mu.Unlock()
		return rep
	}
	ch := make(chan struct{})
	st.inflight = ch
	st.mu.Unlock()
	// Deferred so a panicking sample cannot leave later callers waiting.
	defer func() {
		st.mu.Lock()
		st.inflight = nil
		st.mu.Unlock()
		close(ch)
	}()

	rep := a.sampleDiskReport()
	st.mu.Lock()
	// Aged from when the walk started, so a slow walk does not look fresh.
	st.report, st.at = rep, rep.SampledAt
	st.mu.Unlock()
	return rep
}

// diskReportState is one App's cached reading, kept in a package-level map
// keyed by *App.
type diskReportState struct {
	mu sync.Mutex
	// report is the last reading and at when it was taken; a zero at means
	// nothing has been measured yet.
	report DiskReport
	at     time.Time
	// inflight is non-nil while a sample is being taken and is closed when it
	// lands, which keeps sampling single-flight.
	inflight chan struct{}
}

var (
	diskReportMu  sync.Mutex
	diskReportReg = map[*App]*diskReportState{}
)

// diskReportStateFor returns this App's cached reading, building it on first
// use.
func (a *App) diskReportStateFor() *diskReportState {
	diskReportMu.Lock()
	defer diskReportMu.Unlock()
	st, ok := diskReportReg[a]
	if !ok {
		// An empty list rather than nil, which would encode as JSON null.
		st = &diskReportState{report: DiskReport{Volumes: []VolumeReport{}}}
		diskReportReg[a] = st
	}
	return st
}

// folderDemand is one folder's share of the queue.
type folderDemand struct {
	bytes int64
	tasks int
}

// sampleDiskReport takes one reading. Only DiskReport calls it, one at a time.
func (a *App) sampleDiskReport() DiskReport {
	cfg := a.Settings.Get()
	rep := DiskReport{Volumes: []VolumeReport{}, SampledAt: time.Now()}

	// Configured folders are reported even with an empty queue; they are what
	// the disk thresholds are set against.
	var rows []*VolumeReport
	seen := map[string]bool{}
	add := func(dir, role string) {
		// Only the fixed head of a template exists on disk.
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) {
			// A relative folder would resolve against the working directory.
			return
		}
		if seen[dir] {
			return // the first role that named the folder wins
		}
		seen[dir] = true
		rows = append(rows, &VolumeReport{Dir: dir, Role: role})
	}
	add(a.defaultDir(), roleDownloads)
	add(cfg.WorkDir, roleWork)
	for _, c := range cfg.Categories {
		add(c.Dir, roleCategory)
	}

	// Demand inside a configured folder counts toward that folder, so
	// per-package subfolders do not each get a row and a syscall. Only
	// destinations outside every configured folder get rows of their own. A
	// subfolder that is a separate mount is therefore reported with its
	// parent's figures.
	loose := map[string]*VolumeReport{}
	for dir, d := range a.queueDemand() {
		if v := containingRow(rows, dir); v != nil {
			v.Queued += d.bytes
			v.Tasks += d.tasks
			continue
		}
		clean := filepath.Clean(dir)
		v := loose[clean]
		if v == nil {
			v = &VolumeReport{Dir: clean, Role: roleTask}
			loose[clean] = v
		}
		v.Queued += d.bytes
		v.Tasks += d.tasks
	}
	extra := make([]*VolumeReport, 0, len(loose))
	for _, v := range loose {
		extra = append(extra, v)
	}
	// Most owed first, then by path, so the order is stable.
	sort.Slice(extra, func(i, j int) bool {
		if extra[i].Queued != extra[j].Queued {
			return extra[i].Queued > extra[j].Queued
		}
		return extra[i].Dir < extra[j].Dir
	})
	if len(extra) > maxQueueVolumes {
		extra = extra[:maxQueueVolumes]
		rep.Truncated = true
	}
	rows = append(rows, extra...)

	rep.Volumes = make([]VolumeReport, 0, len(rows))
	for _, v := range rows {
		measure(v)
		rep.Volumes = append(rep.Volumes, *v)
	}
	return rep
}

// queueDemand returns what the queue still owes each destination, keyed by
// a.dirFor(t) exactly as the disk guard groups it, so the readout and the guard
// agree. It releases a.mu before anything is statted. Like the guard, it never
// attributes bytes to the working folder.
func (a *App) queueDemand() map[string]folderDemand {
	out := map[string]folderDemand{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		switch t.Status {
		case core.StatusDone, core.StatusError, core.StatusCollected:
			// The same exclusions as Counters, so the two figures agree.
			continue
		}
		if !t.Enabled {
			continue
		}
		dir := a.dirFor(t)
		d := out[dir]
		d.bytes += remainingBytes(t)
		d.tasks++
		out[dir] = d
	}
	return out
}

// containingRow returns the configured folder dir lies in, or nil. The deepest
// match wins, so a category inside the download folder keeps its own demand.
func containingRow(rows []*VolumeReport, dir string) *VolumeReport {
	var best *VolumeReport
	for _, v := range rows {
		// withinDir: case-insensitive on Windows, and "/downloads-old" is not
		// inside "/downloads".
		if !withinDir(v.Dir, dir) {
			continue
		}
		if best == nil || len(v.Dir) > len(best.Dir) {
			best = v
		}
	}
	return best
}

// measure fills in one row's byte counts and the flags that qualify them.
func measure(v *VolumeReport) {
	v.Measured = deepestExistingDir(v.Dir)
	v.Exists = v.Measured == v.Dir
	sp, ok := diskUsage(v.Measured)
	if !ok {
		// Known stays false, which marks the zeros as meaningless.
		return
	}
	v.Known = true
	v.Free, v.Used, v.Total = sp.Free, sp.Used, sp.Total
}

// deepestExistingDir returns dir when it is a directory, else its nearest
// existing parent. The walk happens here rather than inside diskspace.Usage so
// the substituted folder can be reported.
func deepestExistingDir(dir string) string {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
