package app

// Database maintenance: integrity check, compaction and statistics refresh,
// run from a button or on a long interval, plus the record of the last run.
//
// Every run is asynchronous. A VACUUM on a large store outlives any browser or
// proxy timeout and holds the store's single connection for the whole rewrite,
// so a run is a goroutine registered through App.track: Close cancels it and
// waits for SQLite to roll back. The route answers 202 and the page polls.
//
// The runner state lives in a package-level map keyed by *App.

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

// MaintenanceKind is one of the three maintenance passes. Routes parse it with
// ParseMaintenanceKind so an unknown verb is refused rather than ignored.
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

// SkippedDownloadsRunning is the reason a scheduled pass stood down. It is a
// code rather than a sentence so the interface can translate it.
const SkippedDownloadsRunning = "downloads-running"

// ErrMaintenanceBusy is returned while another run is in flight. Two VACUUMs
// on one connection would freeze the store for both.
var ErrMaintenanceBusy = errors.New("a database maintenance run is already going")

// ErrMaintenanceClosing is returned once Close has begun, so the button does
// not report a start that will never finish.
var ErrMaintenanceClosing = errors.New("the server is shutting down")

// ParseMaintenanceKind parses a kind a caller named and refuses anything else.
func ParseMaintenanceKind(s string) (MaintenanceKind, bool) {
	switch k := MaintenanceKind(s); k {
	case MaintenanceCheck, MaintenanceCompact, MaintenanceAnalyze:
		return k, true
	}
	return "", false
}

// StorageInfo is what the database and settings files cost, and where a
// compaction's scratch copy would go. It carries local paths, so it is served
// only on the session-guarded maintenance route and never goes into the
// diagnostics bundle.
type StorageInfo struct {
	StorePath             string `json:"storePath"`
	StoreBytes            int64  `json:"storeBytes"`            // the .db plus any -journal/-wal/-shm beside it
	StoreReclaimableBytes int64  `json:"storeReclaimableBytes"` // freelist_count * page_size, a floor
	SettingsPath          string `json:"settingsPath"`
	SettingsBytes         int64  `json:"settingsBytes"`
	SettingsPresent       bool   `json:"settingsPresent"` // false on an install that has never saved
	// TempDir is where SQLite writes the full copy a VACUUM needs. The
	// container sets no TMPDIR, so on Unraid that copy lands inside docker.img,
	// and "database or disk is full" would otherwise point at the wrong volume.
	TempDir string `json:"tempDir"`
	// TempFreeBytes is 0 when the platform cannot be asked (see
	// internal/diskspace), which is not the same as 0 bytes free.
	TempFreeBytes int64 `json:"tempFreeBytes"`
}

// MaintenanceRun is one completed pass, as stored in the record file and sent
// to the client.
type MaintenanceRun struct {
	Kind       MaintenanceKind `json:"kind"`
	At         time.Time       `json:"at"`
	DurationMs int64           `json:"durationMs"`
	// OK is false both for a check that found damage and for one that could
	// not run; Error tells them apart.
	OK bool `json:"ok"`
	// Problems is what an integrity check reported, in SQLite's order and
	// without the lone "ok". It is never nil, so it encodes as [].
	Problems []string `json:"problems"`
	// BytesBefore and BytesAfter are the file size either side of a
	// compaction, measured rather than predicted, and 0 for the other kinds.
	BytesBefore int64  `json:"bytesBefore"`
	BytesAfter  int64  `json:"bytesAfter"`
	Error       string `json:"error"`
	// Skipped is set on a scheduled pass that did not run, so the page can
	// tell a skipped schedule from a disabled one.
	Skipped string `json:"skipped"`
}

// MaintenanceState is the whole answer to GET /api/system/maintenance.
type MaintenanceState struct {
	Storage StorageInfo `json:"storage"`
	// Running is the kind in flight, or empty. The page polls while it is set.
	Running           MaintenanceKind `json:"running"`
	IntervalDays      int             `json:"intervalDays"`
	CompactOnSchedule bool            `json:"compactOnSchedule"`
	// NextRunAt and Last are pointers so that "no schedule" and "never run"
	// stay distinct from a zero time or a clean result.
	NextRunAt *time.Time      `json:"nextRunAt"`
	Last      *MaintenanceRun `json:"last"`
}

// maintenanceFile is the record, kept beside the database. Not in the store,
// because it is needed most when the store will not open, and not in
// settings.json, which is user configuration rewritten on every save.
const maintenanceFile = "maintenance.json"

// maintenanceRecord is what maintenanceFile holds.
type maintenanceRecord struct {
	// ArmedAt is when the interval clock started. It is persisted so a box
	// that restarts nightly neither never reaches the interval nor runs on
	// every boot. It is set when a non-zero interval is first seen and cleared
	// when the interval returns to zero.
	ArmedAt time.Time `json:"armedAt"`
	// DBTag is the stamp the last run wrote into the database header
	// (store.SetTag). When the live database lacks it, a restore replaced the
	// file and Last is withheld.
	DBTag int64 `json:"dbTag"`
	// DBSizeAtRun and DBModTimeAtRun are for people reading the file. They do
	// not invalidate the record, since every saved row changes both.
	DBSizeAtRun    int64           `json:"dbSizeAtRun"`
	DBModTimeAtRun time.Time       `json:"dbModTimeAtRun"`
	Last           *MaintenanceRun `json:"last"`
}

// maintenanceState is one App's runner. The mutex guards bookkeeping only and
// is never held across a VACUUM, so polling for "running" never blocks.
type maintenanceState struct {
	mu      sync.Mutex
	running MaintenanceKind
	rec     *maintenanceRecord
	loaded  bool
	// lastReclaimable and lastTag are the last figures read from the database.
	// A compaction holds the only connection, so while one runs these are
	// served instead of a query that would hang until it finishes.
	lastReclaimable int64
	lastTag         int64
}

var (
	maintenanceMu  sync.Mutex
	maintenanceReg = map[*App]*maintenanceState{}
)

// maintenanceStateFor returns this App's runner, building it on first use.
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

// recordLocked returns the record, reading it from disk on first use. A missing
// or unreadable file is an empty record, never an error. Caller holds st.mu.
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

// saveRecordLocked writes the record through a temporary file and a rename, so
// a crash mid-write keeps the previous record. The mode matches the files
// beside it. Caller holds st.mu.
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

// StorageInfo measures the database and settings files without blocking on
// the database: sizes come from stat, and the free-page figure is served from
// the last reading while a run holds the connection. A failing measurement
// leaves the rest of the answer intact.
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
	// An install that never saved its settings has no settings.json, which is
	// reported as absent rather than as 0 bytes.
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

// databaseFigures returns the reclaimable bytes and the identity stamp. They
// are read from the database unless a run holds the connection; then, or when
// the read fails, the last known figures are returned.
func (a *App) databaseFigures() (reclaimable, tag int64) {
	st := a.maintenanceStateFor()
	st.mu.Lock()
	busy := st.running != ""
	reclaimable, tag = st.lastReclaimable, st.lastTag
	st.mu.Unlock()
	if busy {
		return reclaimable, tag
	}

	// Queried without st.mu, so "is it running" never waits on the database.
	sizes, sizeErr := a.Store.Sizes()
	liveTag, tagErr := a.Store.Tag()
	if sizeErr != nil {
		log.Printf("could not read the database page counts: %v", sizeErr)
	}
	if tagErr != nil {
		log.Printf("could not read the database identity stamp: %v", tagErr)
	}
	if sizeErr != nil || tagErr != nil {
		return reclaimable, tag
	}

	st.mu.Lock()
	st.lastReclaimable, st.lastTag = sizes.ReclaimableBytes, liveTag
	st.mu.Unlock()
	return sizes.ReclaimableBytes, liveTag
}

// MaintenanceState returns the sizes, whether something is running, the
// schedule and what the last pass found.
func (a *App) MaintenanceState() MaintenanceState {
	cfg := a.Settings.Get()
	st := a.maintenanceStateFor()

	// StorageInfo first, so the stamp compared below is fresh.
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
	// A result about the database is shown only while it is still the same
	// database; a skip is about the schedule and is always shown.
	if rec.Last != nil && (rec.Last.Skipped != "" || (rec.DBTag != 0 && rec.DBTag == liveTag)) {
		last := *rec.Last
		// Copy the slice so the encoder never shares it with the next run.
		last.Problems = append([]string{}, rec.Last.Problems...)
		out.Last = &last
	}
	if cfg.MaintenanceIntervalDays > 0 && !rec.ArmedAt.IsZero() {
		next := rec.ArmedAt.AddDate(0, 0, cfg.MaintenanceIntervalDays)
		out.NextRunAt = &next
	}
	return out
}

// StartMaintenance starts one pass in the background and returns at once.
// Unlike the scheduled pass it runs even while downloads are active, since a
// person asked for it.
func (a *App) StartMaintenance(kind MaintenanceKind) error {
	st := a.maintenanceStateFor()
	st.mu.Lock()
	if st.running != "" {
		st.mu.Unlock()
		return ErrMaintenanceBusy
	}
	// Claimed before track so two simultaneous requests cannot both start;
	// released again if track refuses.
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

// finishMaintenance records a completed pass and releases the claim under one
// lock, so a polling page never sees "not running" with the old result.
func (a *App) finishMaintenance(st *maintenanceState, run MaintenanceRun) {
	// Stamped after the work, so the stamp describes the file the run
	// produced. A failed stamp only costs restore detection.
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

// runMaintenance does the work on its own goroutine, holding no lock of this
// package, and is cancelled by a.ctx.
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
		// A failed stat leaves 0, which the interface shows as no figure.
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
		err = fmt.Errorf("unknown maintenance kind %q", kind)
	}
	if err != nil {
		run.Error = err.Error()
		run.OK = false
	}
	run.DurationMs = time.Since(started).Milliseconds()
	return run
}

// storeFileBytes is the database file's size, journal included, or 0 when it
// cannot be measured. It uses FileSize rather than a pragma so it does not
// queue on the connection.
func (a *App) storeFileBytes() int64 {
	size, err := a.Store.FileSize()
	if err != nil {
		return 0
	}
	return size
}

// newMaintenanceTag returns a fresh identity stamp: positive, never zero (zero
// means unstamped), and within SQLite's 32-bit application_id.
func newMaintenanceTag() int64 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A fixed stamp only stops restore detection.
		return 1
	}
	tag := int64(binary.BigEndian.Uint32(b[:]) & 0x7fffffff)
	if tag == 0 {
		tag = 1
	}
	return tag
}

// runDBMaintenanceIfDue is the scheduled pass, called once a minute from
// sweep(). It never does the work itself: Close waits for the upkeep loop, and
// an inline VACUUM would outlast a container stop's grace period and get the
// process killed mid-write.
func (a *App) runDBMaintenanceIfDue() {
	cfg := a.Settings.Get()
	st := a.maintenanceStateFor()

	st.mu.Lock()
	rec := st.recordLocked(a.DataDir)

	if cfg.MaintenanceIntervalDays <= 0 {
		// Disarmed, so re-enabling the schedule starts a fresh interval instead
		// of firing within a minute.
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
		// A manual run is going; the schedule fires on a tick after it ends.
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()

	// A scheduled pass stands down while downloads run, and records that it
	// did. Paused and held tasks do not count, or they could block the
	// schedule for ever.
	if a.Counters().Running > 0 {
		a.recordSkip(st, SkippedDownloadsRunning)
		return
	}

	// Re-armed before the work, or the next tick would find the old stamp due
	// and queue a second pass.
	st.mu.Lock()
	rec = st.recordLocked(a.DataDir)
	rec.ArmedAt = time.Now()
	st.saveRecordLocked(a.DataDir)
	st.mu.Unlock()

	if err := a.StartMaintenance(MaintenanceCheck); err != nil {
		return
	}
	if !cfg.MaintenanceCompactOnSchedule {
		return
	}
	// Compaction follows only a clean check: a VACUUM over a corrupt B-tree
	// can make a partly readable database unreadable.
	a.spawn(func() { a.compactAfterCheck(st) })
}

// compactAfterCheck waits for the scheduled check to finish and compacts only
// if it came back clean. The two stay separate runs with separate records; the
// deadline stops the wait if the check never releases its claim.
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

// recordSkip records that a scheduled pass stood down, once per reason, so a
// day of downloads does not rewrite the file every minute. ArmedAt stays, so
// the pass runs on the first tick after the downloads stop, and the stamp and
// sizes stay because the database was not touched.
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
	st.saveRecordLocked(a.DataDir)
}
