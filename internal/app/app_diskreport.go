package app

// Room on the target folders, as a readout rather than as a guard.
//
// The guard next door (app_diskguard.go) asks the same syscall the same
// question and answers it to nobody: it decides whether one task may start,
// logs a volume crossing the pause mark, and throws the reading away. So on an
// install where nothing starts, every row says "waiting for disk space" and
// there is no way at all to see how much room there is, which of the configured
// folders is the tight one, or how much the queue would still want to write
// there if it did start.
//
// THREE ANSWERS PER FOLDER, NOT TWO. It measured; or this platform cannot be
// asked at all, in which case the three byte counts mean nothing whatsoever and
// must not be drawn (internal/diskspace's whole design is that "I do not know"
// is a different answer from zero); or the folder is not there yet and what was
// measured is the nearest existing folder above it. The third is the normal
// case rather than the odd one - a download folder is created by whoever writes
// the first file into it - and it is also the dangerous one: a folder whose
// mount did not come up walks all the way to the volume root, and the figures
// then describe the container's own filesystem under the name of the folder
// somebody was asking about. Both paths travel, so the substitution can be
// seen instead of trusted.
//
// AND NOTHING HERE WRITES ANYTHING. Not a folder, not a probe file, not a
// setting. settings.Validate is the obvious-looking way to find out whether a
// folder is usable, and it MkdirAll's the path and drops a
// .knightloader-write-test into it - so wiring it in here would mean that
// opening a dashboard creates every configured category folder on disk and
// litters each one. os.Stat and nothing else.

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

// diskUsage is diskspace.Usage behind a package variable, for the reason
// freeSpace next door carries: a reading nothing can replace is a reading no
// test can control, and the alternative is a test that only says anything on a
// machine that happens to be nearly full - or, for the half that matters most
// here, only on a platform that cannot measure at all. Written by tests only.
var diskUsage = diskspace.Usage

// diskReportTTL is how long one reading is handed to everybody who asks.
//
// Five seconds, against a route every open browser tab polls. The figures move
// at disk speed and the guard that acts on them looks every fifteen (see
// diskCheckInterval), so measuring more often buys a reader nothing and costs a
// syscall per folder per tab. It is short enough that the number in front of
// somebody is never visibly behind the rows underneath it.
const diskReportTTL = 5 * time.Second

// maxQueueVolumes caps how many folders the QUEUE alone may add to one report.
//
// The configured folders are always all reported: there are as many of those as
// the person made, and each is a folder they chose. What is capped is the tail
// of destinations that sit outside every one of them - a per-task override, a
// rule pointing somewhere else - because that list is as long as the queue is,
// and each entry costs a syscall on a route every tab polls. The rows kept are
// the ones owed the most, and Truncated says the cut happened; the same shape
// internal/api's folder chooser already uses for a directory listing it had to
// cut.
const maxQueueVolumes = 16

// The roles a folder can be in the list for. They travel as stable ids and are
// never English prose: the server has no idea which of the forty-two locales is
// reading, which is the same reason routes_features.go sends a Feature.ID.
const (
	roleDownloads = "downloads"
	roleCategory  = "category"
	roleWork      = "work"
	roleTask      = "task"
)

// VolumeReport is one target folder as the server measured it.
type VolumeReport struct {
	// Dir is the folder the app would write into. It may be a folder that does
	// not exist yet, and for a configured path template it is the fixed head of
	// that template rather than any folder a single download lands in - see
	// settings.FixedPrefix.
	Dir string `json:"dir"`
	// Measured is the folder the figures actually describe: Dir itself when it
	// is there, otherwise the nearest existing folder above it. The two differ
	// harmlessly for a download folder nobody has written into yet, and they
	// differ ALARMINGLY when a mount did not come up - this is then the volume
	// root and the numbers describe a completely different disk. Whatever reads
	// this has to show the difference; hiding it is how a confident wrong number
	// gets in front of somebody.
	Measured string `json:"measured"`
	// Exists reports whether Dir itself is a directory today.
	Exists bool `json:"exists"`
	// Known reports whether this platform could be asked at all. False is a real
	// third answer and not a zero: internal/diskspace has no call on some
	// kernels, and every guard in the app then holds nothing back. When it is
	// false, Free, Used and Total are all 0 and mean NOTHING - not an empty
	// disk, not a full one.
	Known bool `json:"known"`
	// Free is what this process may still write here. On unix that excludes the
	// blocks reserved for root; on Windows it honours a per-user quota. Zero
	// with Known true is a genuinely exhausted volume, which is the one state
	// this whole readout exists to make visible.
	Free uint64 `json:"free"`
	// Used is what somebody's files occupy.
	Used uint64 `json:"used"`
	// Total is the volume's size. Free plus Used can be LESS than this, by the
	// root reserve or by a quota, and none of the three may be derived from the
	// other two - see diskspace.Space, which is where that is dealt with once.
	Total uint64 `json:"total"`
	// Queued is what the downloads still owed would add to this folder:
	// announced size minus what has already arrived, per task, clamped at zero.
	//
	// IT IS A FLOOR AND NOT A FORECAST. A task whose size nobody knows carries
	// Size 0 and contributes nothing, which is most of a fresh queue.
	//
	// AND IT MAY NOT BE SUBTRACTED FROM Free. A transfer that is already running
	// has usually had its room taken out of the volume the moment it started -
	// this build's engine creates the destination file at its full final length
	// before the first byte arrives, which spaceCheck.promised documents from
	// the other side - so its bytes are already missing from Free rather than
	// sitting on top of it. Subtracting one from the other counts those twice
	// and can paint a healthy volume as overcommitted. The two figures stand
	// side by side.
	Queued int64 `json:"queued"`
	// Tasks is how many downloads are aimed at this folder: every one that is
	// still owed and switched on, INCLUDING the ones whose size nobody knows and
	// which therefore add nothing to Queued. That is deliberate. A queue of two
	// hundred unchecked links would otherwise read "0 B, 0 downloads", which is
	// the one thing this row must not say about two hundred files that are about
	// to be written.
	Tasks int `json:"tasks"`
	// Role is why this folder is in the list. See the role constants: it is a
	// stable id, and whatever draws it looks the label up in its own language.
	Role string `json:"role"`
}

// DiskReport is every target folder at one moment.
type DiskReport struct {
	// Volumes is one row per FOLDER, and never nil.
	//
	// Per folder and NOT per disk, which is the caveat whoever draws this has to
	// carry: the ordinary install has the download folder and the working folder
	// on one volume, and the same free gigabytes then appear in two rows.
	// Nothing in this build can tell that they are the same disk - there is no
	// volume identity to be had, diskspace exposes no f_fsid and
	// GetDiskFreeSpaceEx returns none - so these rows must never be added up.
	Volumes []VolumeReport `json:"volumes"`
	// Truncated says the queue named more folders outside the configured ones
	// than maxQueueVolumes, and the smallest demands were left out.
	Truncated bool `json:"truncated"`
	// SampledAt is when the reading was taken. It is shared for a few seconds on
	// purpose, so this is older than "now" and whatever draws it should say so
	// rather than imply a live gauge - between a transfer being stopped and the
	// next sample, this can show a volume comfortably above the mark while the
	// rows underneath read "waiting for disk space".
	SampledAt time.Time `json:"sampledAt"`
}

// DiskReport is what each target folder has room for, and what the queue still
// wants to write into it.
//
// The reading is shared for diskReportTTL, and a caller that arrives while
// somebody else is measuring is handed the PREVIOUS one rather than made to
// wait for the new one. Both halves of that are the same decision: a folder on
// a mount that has gone away takes as long to stat as that mount takes to time
// out, so a route that let every tab start its own walk would pile up
// goroutines inside one syscall, and one that queued them all behind a single
// walk would go silent for as long as it lasted. The first caller of all does
// wait, because there is nothing yet to hand it.
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
	// Cleared and closed even if the sample panics, so one bad walk cannot leave
	// every later caller waiting on a reading that will never land.
	defer func() {
		st.mu.Lock()
		st.inflight = nil
		st.mu.Unlock()
		close(ch)
	}()

	rep := a.sampleDiskReport()
	st.mu.Lock()
	// Aged from when the walk STARTED rather than from when it finished: the
	// figures describe that moment, and a slow walk over a tired mount must not
	// be allowed to hand itself a fresh timestamp.
	st.report, st.at = rep, rep.SampledAt
	st.mu.Unlock()
	return rep
}

// diskReportState is one App's cached reading.
//
// Package level keyed by *App rather than a field on App, the same trade
// app_diskguard.go's diskState and app_accounts.go's refresh timer already
// document: app.go's struct is not this file's to grow.
type diskReportState struct {
	mu sync.Mutex
	// report is the last reading and at is when it was taken. A zero at means
	// nothing has been measured yet, which is the only case anybody waits for.
	report DiskReport
	at     time.Time
	// inflight is non-nil while a sample is being taken and is closed when that
	// sample lands. It is what makes this single-flight rather than a bare "is
	// the timestamp old enough" check, and the difference shows up exactly when
	// it hurts: five tabs arriving in one cold window, on a mount that has
	// stopped answering.
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
		// Even the reading that has never been taken carries an empty list
		// rather than a nil one: a nil slice encodes as JSON null, and the page
		// that walks over it throws instead of drawing nothing. The neighbouring
		// StopCost initialises its own slice for exactly this.
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

// sampleDiskReport takes one reading. Only DiskReport calls it, and only ever
// one at a time.
func (a *App) sampleDiskReport() DiskReport {
	cfg := a.Settings.Get()
	rep := DiskReport{Volumes: []VolumeReport{}, SampledAt: time.Now()}

	// The configured folders first, and reported even with an empty queue: "how
	// much room is left where downloads go" is worth an answer before anything
	// has been added, and it is the question the three thresholds on the
	// downloads settings page are set against.
	var rows []*VolumeReport
	seen := map[string]bool{}
	add := func(dir, role string) {
		// Cut back to the part of a template that is a real path before anything
		// stats it. "/downloads/<jd:date>/<jd:packagename>" never exists, so
		// measuring it as written walks up past the download folder and reports
		// whatever it lands on. settings.FixedPrefix is the app's own rule for
		// where a template stops being a path, exported rather than copied.
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) {
			// A relative folder cannot be measured from here: it resolves against
			// whatever the process's working directory happens to be, which is
			// the same reason sanitizePaths refuses to store one.
			return
		}
		if seen[dir] {
			return // one folder, one row: the first role that named it wins
		}
		seen[dir] = true
		rows = append(rows, &VolumeReport{Dir: dir, Role: role})
	}
	add(a.defaultDir(), roleDownloads)
	add(cfg.WorkDir, roleWork)
	for _, c := range cfg.Categories {
		add(c.Dir, roleCategory)
	}

	// What the queue owes, put against the row it belongs to. A destination
	// inside a configured folder is counted against that configured folder
	// instead of getting a row of its own: with the per-package subfolder
	// switched on, every package resolves to its own directory, and a row each
	// would be thirty rows and thirty syscalls describing one disk thirty times.
	// A destination OUTSIDE all of them does get its own row, and that is the
	// case worth one - an override or a rule pointing at another mount.
	//
	// The price is said out loud rather than hidden: a subfolder that is itself
	// a separate mount is then reported under its parent's figures. Nothing here
	// can tell that it is one, and the alternative is a syscall per package on a
	// route every open tab polls.
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
	// Most owed first, and by path where two are owed the same, so that two
	// readings of an unchanged queue come out in the same order rather than in
	// whatever order the map felt like.
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

// queueDemand is what the queue still owes each destination folder, keyed by
// a.dirFor(t) byte for byte - the key the disk guard groups by and the
// dispatcher checks against. A second opinion about where a download lands
// would show one folder under two names and let this readout disagree with the
// guard that actually holds downloads back.
//
// THE ONLY PLACE IN THIS FILE THAT TAKES a.mu, and it is released before
// anything stats anything. A destination on an unresponsive network mount
// blocks the call that measures it; the dispatcher already pays that once a
// pass with a.mu in its hand (spaceCheck says so itself), and a GET that every
// open browser tab makes may not join in.
//
// NOTHING IS EVER ATTRIBUTED TO THE WORKING FOLDER, and that is worth knowing
// rather than discovering. With a WorkDir configured the bytes are written
// there first and moved to the destination once the download is finished, so
// the folder this counts against is not the folder that fills up in the
// meantime. The disk guard reads it exactly the same way, and the two agreeing
// is worth more than this half being cleverer on its own: a readout that put
// the demand somewhere the guard never looks would explain nothing about why a
// download is being held back.
func (a *App) queueDemand() map[string]folderDemand {
	out := map[string]folderDemand{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		switch t.Status {
		case core.StatusDone, core.StatusError, core.StatusCollected:
			// The three exclusions Counters makes, so the figure here and the
			// one under the list cannot contradict each other: nothing is owed
			// on a finished or a failed download, and a link still in the
			// collector has not been added to the queue at all.
			continue
		}
		if !t.Enabled {
			// Out of both figures, as it is out of Counters' byte total: a link
			// that is switched off is not going to be written anywhere, and
			// counting it puts bytes in front of somebody that no amount of
			// waiting works off.
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

// containingRow is the configured folder dir belongs to, or nil when it sits
// outside every one of them. The deepest match wins, so a category folder
// inside the download folder keeps the demand of its own tasks rather than
// handing it to the folder above.
func containingRow(rows []*VolumeReport, dir string) *VolumeReport {
	var best *VolumeReport
	for _, v := range rows {
		// withinDir rather than a string prefix, for the reason it carries: a
		// path comparison has to be case-insensitive on Windows, and
		// "/downloads-old" is not inside "/downloads".
		if !withinDir(v.Dir, dir) {
			continue
		}
		if best == nil || len(v.Dir) > len(best.Dir) {
			best = v
		}
	}
	return best
}

// measure fills in one row's byte counts and the two flags that say what they
// are worth.
func measure(v *VolumeReport) {
	v.Measured = deepestExistingDir(v.Dir)
	// No second stat for Exists: the walk answers with the folder itself when
	// that folder is a directory, so the two are one question.
	v.Exists = v.Measured == v.Dir
	sp, ok := diskUsage(v.Measured)
	if !ok {
		// Left at zero, and Known is the field that says those zeroes mean
		// nothing. See internal/diskspace: a platform that cannot be asked is a
		// third answer, not a full disk.
		return
	}
	v.Known = true
	v.Free, v.Used, v.Total = sp.Free, sp.Used, sp.Total
}

// deepestExistingDir is dir itself when it is a directory today, else the
// nearest folder above it that is.
//
// Walked here rather than left to diskspace.Usage's own identical walk, and
// that is the whole difference between a guard and a readout: the walk inside
// that package is invisible from outside it, so a folder whose mount did not
// come up is measured at the volume root and reported, with total confidence,
// as the folder that was asked about. What this answers travels beside the
// folder that was asked about, so the substitution can be seen.
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
