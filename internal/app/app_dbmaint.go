package app

// Database maintenance: the integrity check, the compaction and the statistics
// refresh, run on demand from a button or on a long interval, plus the record
// of what the last one found.
//
// EVERYTHING HERE IS ASYNCHRONOUS AND NONE OF IT IS OPTIONAL. A VACUUM on a
// multi-gigabyte store outlives any browser timeout and any reverse proxy's,
// and it holds the store's single connection (internal/store's Open explains
// why there is only ever one) for the whole rewrite. So a run is a goroutine
// with a context, registered through App.track so that Close cancels it and
// then waits for SQLite to roll the half-finished rewrite back; the route that
// starts one answers 202 and the page polls. A synchronous handler would give
// the operator a dead browser tab and no way to find out whether the database
// was being rewritten behind it.
//
// Kept at package level and keyed by the owning *App rather than as fields on
// App (app.go) - the same trade activityReg (app_activity.go), captchaState
// (app_captcha.go) and hosterAuth (app_hosterauth.go) already document, for the
// identical reason: app.go's struct is not this feature's file to grow, and a
// package-level map gives the same per-instance guarantee without touching it.
// Production runs exactly one App for the life of the process.

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/diskspace"
)

// MaintenanceKind is the three things that can be done to the database. Typed
// rather than free text, and parsed at the edge by ParseMaintenanceKind, so a
// route cannot hand this package a verb it will silently do nothing about.
type MaintenanceKind string

const (
	// MaintenanceCheck reads every page and reports what SQLite finds. It
	// writes nothing.
	MaintenanceCheck MaintenanceKind = "check"
	// MaintenanceCompact rewrites the whole file at its real size.
	MaintenanceCompact MaintenanceKind = "compact"
	// MaintenanceAnalyze re-measures the tables for the query planner.
	MaintenanceAnalyze MaintenanceKind = "analyze"
)

// SkippedDownloadsRunning is the one value MaintenanceRun.Skipped ever takes
// today. It is a machine-readable reason rather than a sentence, because the
// sentence has to be translated into forty-two languages and a string built
// here could not be.
const SkippedDownloadsRunning = "downloads-running"

// ErrMaintenanceBusy is what a second start gets while one run is in flight.
// One at a time is not an implementation limit but the point: two VACUUMs
// queued on one connection is one VACUUM followed by a second, pointless one,
// with the store frozen for the sum of both.
var ErrMaintenanceBusy = errors.New("a database maintenance run is already going")

// ErrMaintenanceClosing is what a start gets once Close has committed to
// shutting down. Reported rather than swallowed: a button that answers "started"
// and then never reports a result is worse than one that says the server is on
// its way out.
var ErrMaintenanceClosing = errors.New("the server is shutting down")

// ParseMaintenanceKind turns what a caller named into one of the three and
// refuses anything else - the same shape, and for the same reason, as
// KnownActivityKind (app_activity.go) and KnownOrigin (app_links.go): a route
// that acted on free text would answer 202 to a typo and do nothing at all.
func ParseMaintenanceKind(s string) (MaintenanceKind, bool) {
	switch k := MaintenanceKind(s); k {
	case MaintenanceCheck, MaintenanceCompact, MaintenanceAnalyze:
		return k, true
	}
	return "", false
}

// StorageInfo is what the two files this instance keeps are costing, and where
// the compaction's scratch copy would go.
//
// THE PATHS ARE IN HERE AND DELIBERATELY NOT IN THE DIAGNOSTICS BUNDLE.
// internal/api/routes_diagnostics.go spends eight lines arguing that the bundle
// is a file meant to be attached to a PUBLIC bug report; a desktop data
// directory is C:\Users\<a person's real name>\AppData\..., which has no
// business in one. This struct goes out on the session-guarded maintenance
// route only, where it is answering somebody who is already looking at their
// own settings pages, and where the path is the single most useful thing on the
// screen the moment a check comes back damaged.
type StorageInfo struct {
	StorePath             string `json:"storePath"`
	StoreBytes            int64  `json:"storeBytes"`            // the .db plus any -journal/-wal/-shm beside it
	StoreReclaimableBytes int64  `json:"storeReclaimableBytes"` // freelist_count * page_size, a FLOOR
	SettingsPath          string `json:"settingsPath"`
	SettingsBytes         int64  `json:"settingsBytes"`
	SettingsPresent       bool   `json:"settingsPresent"` // false on an install that has never saved
	// TempDir is where SQLite writes the full second copy a VACUUM needs, and
	// it is reported because it is almost never where anybody looks. The
	// container image sets KL_DATA=/data and VOLUME ["/data"] and no TMPDIR at
	// all, so the scratch copy of a 6 GB store lands in the container's own
	// writable layer - on Unraid, inside docker.img, a fixed-size image whose
	// filling takes every container on the box down with it. Without this
	// field the failure reads "database or disk is full" and sends the
	// operator to a data volume with terabytes free.
	TempDir string `json:"tempDir"`
	// TempFreeBytes is 0 when this build cannot ask the platform (see
	// internal/diskspace), which is a different thing from 0 bytes free. The
	// interface renders the pair together and never the number alone.
	TempFreeBytes int64 `json:"tempFreeBytes"`
}

// MaintenanceRun is one completed pass, exactly as it is written to the record
// file and exactly as it goes out on the wire.
type MaintenanceRun struct {
	Kind       MaintenanceKind `json:"kind"`
	At         time.Time       `json:"at"`
	DurationMs int64           `json:"durationMs"`
	// OK is whether the pass did what it set out to do. A check that found
	// damage is not OK; a check that could not be run at all is also not OK,
	// and Error tells the two apart.
	OK bool `json:"ok"`
	// Problems is what an integrity check reported, verbatim and in SQLite's
	// own order, never containing the word "ok" on its own - internal/store's
	// IntegrityCheck folds that away so that len == 0 is the whole test.
	// Always an empty slice, never nil: a JSON null here would have every
	// caller on the other side write the same `?? []` guard.
	Problems []string `json:"problems"`
	// BytesBefore and BytesAfter are the file's size either side of a
	// compaction, and 0 for the two kinds that do not change it. Measured
	// rather than predicted: the free-list figure the page shows beforehand is
	// a floor (see store.Sizes), so the only honest "you got this much back"
	// is the difference between two stats.
	BytesBefore int64  `json:"bytesBefore"`
	BytesAfter  int64  `json:"bytesAfter"`
	Error       string `json:"error"`
	// Skipped is set on a SCHEDULED pass that did not run, and empty on every
	// pass that did. A schedule that quietly does nothing is worse than one
	// that is switched off, because nobody can tell the two apart from the
	// outside; this is what lets the page say which it is.
	Skipped string `json:"skipped"`
}

// MaintenanceState is the whole answer to GET /api/system/maintenance.
type MaintenanceState struct {
	Storage StorageInfo `json:"storage"`
	// Running is the kind in flight, or empty when nothing is. The page polls
	// while it is non-empty and stops when it clears, which is the only way to
	// follow work that outlives its own request.
	Running           MaintenanceKind `json:"running"`
	IntervalDays      int             `json:"intervalDays"`
	CompactOnSchedule bool            `json:"compactOnSchedule"`
	// NextRunAt and Last are POINTERS on purpose. "No schedule" and "the next
	// run is at the epoch" are different answers, and so are "nothing has ever
	// run here" and "something ran and found nothing wrong" - a zero-valued
	// struct would render the second when the truth is the first, which on
	// this page is a clean bill of health nobody earned. Same reasoning
	// core.Task.AutoExtract already carries.
	NextRunAt *time.Time      `json:"nextRunAt"`
	Last      *MaintenanceRun `json:"last"`
}

// maintenanceFile is the record, beside the database rather than in it.
//
// NOT IN THE STORE, because the one moment anybody needs to read "the check
// failed on the 3rd" is the moment the store will not open - a row in it is
// unreachable exactly when it matters. NOT IN settings.json either: that file
// is user configuration, its own size is one of the numbers being reported
// here, and setLocked rewrites the whole document on every touch, so runtime
// state living in it would be rewritten by every unrelated save.
const maintenanceFile = "maintenance.json"

// maintenanceRecord is what that file holds.
type maintenanceRecord struct {
	// ArmedAt is when the interval clock started, and it is the whole reason
	// this file exists rather than a variable. A scheduler whose "last run"
	// lives only in memory is either useless or hostile on a box that restarts
	// nightly: it never reaches the interval, or it fires on every boot. It is
	// set the first time the app sees a non-zero interval and cleared when the
	// interval goes back to zero, so switching the schedule on at 15:00 puts
	// the first run a whole interval away rather than thirty seconds away.
	ArmedAt time.Time `json:"armedAt"`
	// DBTag is the identity stamp the last run wrote into the database's own
	// header (store.SetTag). When the live database does not carry it, this
	// record is about a file that is no longer there - a restore has been
	// applied under it - and Last is withheld. See store.Tag for why this and
	// not the file's size and modification time.
	DBTag int64 `json:"dbTag"`
	// DBSizeAtRun and DBModTimeAtRun are kept for the log and for whoever is
	// reading this file by hand during an incident. They are deliberately NOT
	// what invalidates the record: both move every time a download saves a
	// row, so a check against them would blank the page's own answer seconds
	// after somebody read it.
	DBSizeAtRun    int64           `json:"dbSizeAtRun"`
	DBModTimeAtRun time.Time       `json:"dbModTimeAtRun"`
	Last           *MaintenanceRun `json:"last"`
}

// maintenanceState is one App's runner: what is in flight, and the record as it
// stands. Both under one mutex, held only for the bookkeeping - never across a
// VACUUM, or the GET that reports "running" would itself block for the length
// of the rewrite it is trying to describe.
type maintenanceState struct {
	mu      sync.Mutex
	running MaintenanceKind
	rec     *maintenanceRecord
	loaded  bool
	// lastReclaimable and lastTag are the two figures that can only be had by
	// querying the database, remembered from the last time it could be asked.
	//
	// They exist because of what a compaction does to every other reader: it
	// holds the store's one connection for the whole rewrite, so a pragma
	// issued while it runs does not answer until it is over. The page polls
	// this state every two seconds precisely while that is happening, and the
	// diagnostics bundle can be pulled at the same moment. Serving the last
	// known figures for those seconds is a reading a few minutes old; querying
	// for them would be a request that hangs for ten minutes, and a page that
	// can never show the run it started finishing.
	//
	lastReclaimable int64
	lastTag         int64
}

var (
	maintenanceMu  sync.Mutex
	maintenanceReg = map[*App]*maintenanceState{}
)

// maintenanceStateFor returns this App's runner, building it on first use - the
// same lazy-registry shape activityStateFor already uses.
func (a *App) maintenanceStateFor() *maintenanceState {
	maintenanceMu.Lock()
	defer maintenanceMu.Unlock()
	st, ok := maintenanceReg[a]
	if !ok {
		st = &maintenanceState{}
		maintenanceReg[a] = st
	}
	return st
}

// recordLocked is the record as it stands, read off disk on first use. Caller
// holds st.mu.
//
// A missing or unreadable file is an empty record and never an error: the
// ordinary case on every install that has never run maintenance is that there
// is no file, and a build that refused to report the database's size because it
// could not parse a JSON file full of runtime state would be failing the useful
// half of the page over the optional half.
func (st *maintenanceState) recordLocked(dataDir string) *maintenanceRecord {
	if st.loaded {
		return st.rec
	}
	st.loaded = true
	st.rec = &maintenanceRecord{}
	b, err := os.ReadFile(filepath.Join(dataDir, maintenanceFile))
	if err != nil {
		return st.rec
	}
	var rec maintenanceRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		log.Printf("the database maintenance record could not be read and is being started again: %v", err)
		return st.rec
	}
	st.rec = &rec
	return st.rec
}

// saveRecordLocked writes the record through a temporary file and one rename,
// so that a process killed mid-write leaves the previous record intact rather
// than a truncated one that parses as "nothing has ever run". Caller holds
// st.mu.
//
// 0o600 because it names this instance's own paths and the verdict on its
// database. Nothing here is a credential, but nothing here is anybody else's
// business either, and the two files it sits beside are already written that
// way.
func (st *maintenanceState) saveRecordLocked(dataDir string) {
	b, err := json.MarshalIndent(st.rec, "", "  ")
	if err != nil {
		log.Printf("could not write the database maintenance record: %v", err)
		return
	}
	final := filepath.Join(dataDir, maintenanceFile)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("could not write the database maintenance record: %v", err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		log.Printf("could not put the database maintenance record in place: %v", err)
		_ = os.Remove(tmp)
	}
}

// StorageInfo measures the two files this instance keeps, and never blocks on
// the database.
//
// The sizes and the temporary directory come from os.Stat and the platform, so
// they are answerable at any moment, including in the middle of a compaction.
// The free-page figure cannot be: it is a header pragma, and a pragma issued
// while a rewrite holds the one connection waits for the rewrite. So it is
// asked for only when nothing is running, and served from the last answer while
// something is - see maintenanceState.lastReclaimable.
//
// An error from the store is not fatal to the answer. The settings file's size
// and the temporary volume are still true, and a page that showed nothing at
// all because one pragma failed would be hiding the very state somebody came to
// look at.
func (a *App) StorageInfo() StorageInfo {
	out := StorageInfo{
		StorePath:    a.Store.Path(),
		SettingsPath: a.Settings.Path(),
		TempDir:      os.TempDir(),
	}
	if size, err := a.Store.FileSize(); err == nil {
		out.StoreBytes = size
	} else {
		log.Printf("could not measure the database file: %v", err)
	}
	// Not there is an ANSWER, not a failure: settings.Load reads the file and
	// never writes it, so an install nobody has saved a settings page on runs
	// entirely on the built-in defaults and has no settings.json at all. "0
	// bytes" for a file that does not exist is a different claim, and the
	// wrong one.
	if fi, err := os.Stat(out.SettingsPath); err == nil {
		out.SettingsPresent = true
		out.SettingsBytes = fi.Size()
	}
	if free, ok := diskspace.Free(out.TempDir); ok {
		out.TempFreeBytes = int64(free)
	}
	out.StoreReclaimableBytes, _ = a.databaseFigures()
	return out
}

// databaseFigures is the free-page count and the identity stamp, both of which
// need the connection - refreshed when it is free, remembered when it is not.
// It returns the reclaimable bytes and the tag.
func (a *App) databaseFigures() (reclaimable, tag int64) {
	st := a.maintenanceStateFor()
	st.mu.Lock()
	busy := st.running != ""
	reclaimable, tag = st.lastReclaimable, st.lastTag
	st.mu.Unlock()
	if busy {
		return reclaimable, tag
	}

	// Off the lock. Both of these are queries against the store, and holding
	// the runner's mutex across them would make the one thing this whole
	// arrangement exists to keep answerable - "is it still running" - wait on
	// the database after all.
	sizes, sizeErr := a.Store.Sizes()
	liveTag, tagErr := a.Store.Tag()
	if sizeErr != nil {
		log.Printf("could not read the database page counts: %v", sizeErr)
	}
	if tagErr != nil {
		log.Printf("could not read the database identity stamp: %v", tagErr)
	}
	if sizeErr != nil || tagErr != nil {
		// Whatever was last known, rather than a fresh pair of zeros: a
		// database that will not answer a header pragma has not just had all
		// its free space returned to it.
		return reclaimable, tag
	}

	st.mu.Lock()
	st.lastReclaimable, st.lastTag = sizes.ReclaimableBytes, liveTag
	st.mu.Unlock()
	return sizes.ReclaimableBytes, liveTag
}

// MaintenanceState is everything the page needs in one read: the sizes, whether
// something is running, the schedule, and what the last pass found.
func (a *App) MaintenanceState() MaintenanceState {
	cfg := a.Settings.Get()
	st := a.maintenanceStateFor()

	// Built first, and deliberately so: StorageInfo refreshes the identity
	// stamp below through databaseFigures, and reading a stamp AFTER deciding
	// whether the record matches it would compare the record against whatever
	// the previous poll happened to see.
	out := MaintenanceState{
		Storage:           a.StorageInfo(),
		IntervalDays:      cfg.MaintenanceIntervalDays,
		CompactOnSchedule: cfg.MaintenanceCompactOnSchedule,
	}
	_, liveTag := a.databaseFigures()

	st.mu.Lock()
	defer st.mu.Unlock()
	rec := st.recordLocked(a.DataDir)
	out.Running = st.running
	// A record that describes the DATABASE is only shown while the database is
	// still the one it describes - see maintenanceRecord.DBTag. A record that
	// describes the SCHEDULE ("the run was skipped, downloads were going") is
	// shown regardless: it is a statement about this instance's timer, it is
	// true whatever file is underneath, and withholding it would recreate the
	// silent-schedule failure the skip record exists to end.
	if rec.Last != nil && (rec.Last.Skipped != "" || (rec.DBTag != 0 && rec.DBTag == liveTag)) {
		last := *rec.Last
		// Copied out, and the slice with it. The record is shared state held
		// under this mutex for the life of the process; handing the caller the
		// same backing array would let a JSON encoder read it while the next
		// run is replacing it.
		last.Problems = append([]string{}, rec.Last.Problems...)
		out.Last = &last
	}
	if cfg.MaintenanceIntervalDays > 0 && !rec.ArmedAt.IsZero() {
		next := rec.ArmedAt.AddDate(0, 0, cfg.MaintenanceIntervalDays)
		out.NextRunAt = &next
	}
	return out
}

// StartMaintenance begins one pass in the background and returns at once.
//
// The manual path, and it runs whatever the queue is doing. That is the
// difference from the scheduled one and it is deliberate: a person pressing
// this at three in the morning has decided, the confirm dialog in front of the
// compact button named the file size before they did, and a button that refused
// because one download was in flight would be a button that never works on the
// machines this feature is for. See runDBMaintenanceIfDue for the other rule.
func (a *App) StartMaintenance(kind MaintenanceKind) error {
	st := a.maintenanceStateFor()
	st.mu.Lock()
	if st.running != "" {
		st.mu.Unlock()
		return ErrMaintenanceBusy
	}
	// Claimed before track, so that two requests arriving together cannot both
	// get past this point. If track then refuses, the claim is given back
	// below - the alternative order would leave a window in which the state
	// says nothing is running while a goroutine is starting one.
	st.running = kind
	st.mu.Unlock()

	if !a.track() {
		st.mu.Lock()
		st.running = ""
		st.mu.Unlock()
		return ErrMaintenanceClosing
	}
	go func() {
		defer a.wg.Done()
		run := a.runMaintenance(kind)
		a.finishMaintenance(st, run)
	}()
	return nil
}

// finishMaintenance records a completed pass and releases the one-at-a-time
// claim, in that order and under one lock. Two critical sections would leave a
// moment in which nothing is running and the new result is not yet visible,
// which on a page polling every two seconds renders as the answer flickering
// back to the previous run.
func (a *App) finishMaintenance(st *maintenanceState, run MaintenanceRun) {
	// The stamp goes on the file that was just examined, and it is written
	// AFTER the work rather than before: a stamp written first would be
	// relying on VACUUM preserving application_id instead of proving it, and
	// it would describe a file the rewrite had not yet produced. A failure
	// here costs the record's ability to notice a later restore, which is
	// worth a log line and nothing more - the verdict itself is still true.
	tag := newMaintenanceTag()
	if err := a.Store.SetTag(tag); err != nil {
		log.Printf("could not stamp the database after maintenance: %v", err)
		tag = 0
	}
	var size int64
	var mod time.Time
	if fi, err := os.Stat(a.Store.Path()); err == nil {
		size, mod = fi.Size(), fi.ModTime()
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	rec := st.recordLocked(a.DataDir)
	rec.Last = &run
	rec.DBTag = tag
	rec.DBSizeAtRun = size
	rec.DBModTimeAtRun = mod
	st.saveRecordLocked(a.DataDir)
	st.running = ""
}

// runMaintenance does the work. It runs on its own goroutine, holds no lock of
// this package's, and is cancelled by a.ctx - which is what a Close reaches when
// the process is being shut down mid-rewrite.
func (a *App) runMaintenance(kind MaintenanceKind) MaintenanceRun {
	started := time.Now()
	run := MaintenanceRun{Kind: kind, At: started, Problems: []string{}}
	var err error
	switch kind {
	case MaintenanceCheck:
		var problems []string
		problems, err = a.Store.IntegrityCheck(a.ctx, 0)
		if err == nil {
			run.Problems = problems
			run.OK = len(problems) == 0
			if !run.OK {
				log.Printf("the database failed its integrity check with %d problem(s); the first is: %s",
					len(problems), problems[0])
			}
		}
	case MaintenanceCompact:
		// Measured either side rather than reported from the free-list figure,
		// which is a floor and would undershoot every time. A stat that fails
		// leaves the number at 0, and the interface reads a zero "before" as
		// "no figure" rather than as "the file was empty".
		run.BytesBefore = a.storeFileBytes()
		err = a.Store.Vacuum(a.ctx)
		run.BytesAfter = a.storeFileBytes()
		run.OK = err == nil
		if run.OK {
			log.Printf("database compacted: %d bytes before, %d after", run.BytesBefore, run.BytesAfter)
		}
	case MaintenanceAnalyze:
		err = a.Store.Analyze(a.ctx)
		run.OK = err == nil
	default:
		// Unreachable through the route, which parses the kind first. Recorded
		// rather than ignored, because a caller inside this package adding a
		// fourth kind and forgetting a case here should see it on the page and
		// not in silence.
		err = fmt.Errorf("unknown maintenance kind %q", kind)
	}
	if err != nil {
		run.Error = err.Error()
		run.OK = false
	}
	run.DurationMs = time.Since(started).Milliseconds()
	return run
}

// storeFileBytes is the database file's own size, journal included, or 0 when
// it cannot be measured. Used either side of a compaction, which is why it is
// FileSize and not Sizes: the "after" reading is taken the instant the rewrite
// finishes, and a pragma there would be one more query queued behind whatever
// the release of the connection has just let through.
func (a *App) storeFileBytes() int64 {
	size, err := a.Store.FileSize()
	if err != nil {
		return 0
	}
	return size
}

// newMaintenanceTag is a fresh identity stamp for the database header. Positive
// and never zero, because zero is what an unstamped database answers and is the
// value the record uses to mean "no stamp"; the range is kept inside a signed
// 32-bit integer, which is the width SQLite's application_id field has.
func newMaintenanceTag() int64 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any platform this runs on, and if it
		// did, a fixed stamp is still better than none: the record would stop
		// noticing restores rather than stop working.
		return 1
	}
	tag := int64(binary.BigEndian.Uint32(b[:]) & 0x7fffffff)
	if tag == 0 {
		tag = 1
	}
	return tag
}

// runDBMaintenanceIfDue is the scheduled half, called once a minute from
// sweep().
//
// IT NEVER DOES THE WORK ON THE CALLER'S GOROUTINE. sweep() runs on the upkeep
// loop, and Close waits for that loop before it closes the store (app.go). A
// VACUUM run inline here would therefore make a `docker stop` wait for the
// whole rewrite, hit the runtime's ten-second SIGKILL, and take the process
// down in the middle of writing the database - the exact failure this feature
// exists to prevent. So the pass is started through StartMaintenance's own
// tracked goroutine and this function returns immediately.
//
// Cheap on every ordinary tick: one settings read and, when a schedule is set,
// one comparison against a stamp already in memory.
func (a *App) runDBMaintenanceIfDue() {
	cfg := a.Settings.Get()
	st := a.maintenanceStateFor()

	st.mu.Lock()
	rec := st.recordLocked(a.DataDir)

	if cfg.MaintenanceIntervalDays <= 0 {
		// Disarmed, so that switching the schedule back on later starts the
		// clock from that moment rather than from whenever it was last on. A
		// stale armed-at kept through a year of the setting being off would
		// fire a compaction within a minute of somebody switching it on, which
		// is precisely what arming a whole interval out exists to prevent.
		if !rec.ArmedAt.IsZero() {
			rec.ArmedAt = time.Time{}
			st.saveRecordLocked(a.DataDir)
		}
		st.mu.Unlock()
		return
	}
	if rec.ArmedAt.IsZero() {
		rec.ArmedAt = time.Now()
		st.saveRecordLocked(a.DataDir)
		st.mu.Unlock()
		return
	}
	if time.Now().Before(rec.ArmedAt.AddDate(0, 0, cfg.MaintenanceIntervalDays)) {
		st.mu.Unlock()
		return
	}
	if st.running != "" {
		// A manual run is in flight and this tick is simply late. Not recorded
		// as a skip: nothing was refused, and the armed-at stamp is left alone
		// so the schedule fires on the next tick after that run finishes.
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()

	// The one rule the manual button does not share. A scheduled pass that
	// froze every write for ten minutes in the middle of somebody's downloads
	// would be the app sabotaging itself on a timer, so it stands down - and it
	// says so, because a schedule that silently never runs is worse than one
	// that is switched off. Running downloads only: a paused or held row is
	// something a person parked, and letting it disable the schedule for ever
	// would be the silent-never-runs failure wearing a different hat. A task
	// that starts during the pass simply queues on the connection, exactly as
	// it does for the manual button.
	if a.Counters().Running > 0 {
		a.recordSkip(st, SkippedDownloadsRunning)
		return
	}

	// The armed-at stamp moves BEFORE the work rather than after it. The pass
	// is minutes long and the tick is one minute, so re-arming afterwards would
	// let a second tick find the old stamp still due and queue a second pass
	// behind the first.
	st.mu.Lock()
	rec = st.recordLocked(a.DataDir)
	rec.ArmedAt = time.Now()
	st.saveRecordLocked(a.DataDir)
	st.mu.Unlock()

	if err := a.StartMaintenance(MaintenanceCheck); err != nil {
		// Busy or closing. Either way there is nothing useful to record: the
		// first is another pass already doing this work, the second is a
		// process on its way out.
		return
	}
	if !cfg.MaintenanceCompactOnSchedule {
		return
	}
	// The compaction waits for the check and only happens if the check passed.
	// Compacting a damaged database is how something recoverable becomes
	// something gone: VACUUM reads every page and rewrites the file, so a
	// rewrite driven by a corrupt B-tree can turn a database that still had
	// most of its rows readable into one that has none. The check's own verdict
	// stays as the last record in that case, which is the answer somebody needs
	// to see.
	a.spawn(func() { a.compactAfterCheck(st) })
}

// compactAfterCheck waits for the scheduled integrity check to finish and
// compacts only if it came back clean.
//
// It polls rather than chaining off the check's own goroutine, because the two
// passes are separate records and separate entries in the one-at-a-time claim -
// chaining them would make a single run that reports one verdict for two
// different pieces of work. The poll is a lock and a compare every two seconds
// against a run that takes minutes; the ceiling is what stops it outliving the
// thing it is waiting for if that pass never clears the claim at all.
func (a *App) compactAfterCheck(st *maintenanceState) {
	const (
		poll     = 2 * time.Second
		giveUpAt = 6 * time.Hour
	)
	deadline := time.Now().Add(giveUpAt)
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(poll):
		}
		if time.Now().After(deadline) {
			log.Print("the scheduled compaction gave up waiting for its integrity check")
			return
		}
		st.mu.Lock()
		running := st.running
		rec := st.recordLocked(a.DataDir)
		last := rec.Last
		st.mu.Unlock()
		if running != "" {
			continue
		}
		if last == nil || last.Kind != MaintenanceCheck {
			return
		}
		if !last.OK {
			log.Print("the scheduled compaction was not run: the integrity check before it reported problems")
			return
		}
		if err := a.StartMaintenance(MaintenanceCompact); err != nil {
			log.Printf("the scheduled compaction did not start: %v", err)
		}
		return
	}
}

// recordSkip writes down that a scheduled pass stood down, and does it at most
// once per reason.
//
// Once, because sweep() runs every minute and a download can run for a day: a
// record rewritten sixty times an hour would be a file written for no new
// information and a "last run" timestamp that crept forward while nothing
// happened. The armed-at stamp is deliberately untouched, so the pass fires on
// the first tick after the downloads stop rather than waiting another whole
// interval.
func (a *App) recordSkip(st *maintenanceState, reason string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	rec := st.recordLocked(a.DataDir)
	if rec.Last != nil && rec.Last.Skipped == reason {
		return
	}
	rec.Last = &MaintenanceRun{
		Kind:     MaintenanceCheck,
		At:       time.Now(),
		Problems: []string{},
		Skipped:  reason,
	}
	// The stamp and the sizes are left exactly as they were. This record is
	// about a pass that did NOT touch the database, so re-stamping the file
	// here would throw away the record's ability to tell a restore from an
	// ordinary write for no gain at all.
	st.saveRecordLocked(a.DataDir)
}
