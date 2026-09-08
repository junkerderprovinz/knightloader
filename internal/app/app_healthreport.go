package app

// The SUBSYSTEM health report: every part of this instance that can fail on its
// own, with a state of its own; the queue counted by why it is waiting and why
// it failed; room on the target folders; and how long this process has been up.
//
// TWO FILES IN THIS PACKAGE ARE CALLED SOMETHING WITH "HEALTH" IN IT, AND THEY
// ARE UNRELATED. app_health.go is the ACCOUNT health state machine - benching a
// hoster credential that failed, probing it again when the bench expires - and
// its own header spends a paragraph explaining that a different feature shares
// the word. It keeps that separation by spelling every symbol acctHealth* /
// bench* / accountRoutable*. This file is the other feature, and it keeps its
// side of the bargain the same way: everything here reads sysHealth* /
// Subsystem* / SubsystemState, and the words acctHealth and HealthState are
// never spelled in it. The collision is not hypothetical - accounts.HealthState
// is already imported into this package by app_health.go, so a `type
// HealthState` here would not even compile, and a `type Health` would compile
// and be the wrong one at every call site forever after.
//
// WHY IT IS NOT THE EXISTING /api/health. That route answers two fields and a
// literal "ok" and MUST GO ON DOING SO: the phone app's LAN discovery compares
// the string (mobile/src/api/discover.ts), the container's HEALTHCHECK reads
// the exit code (Dockerfile), and the Click'n'Load bridge refuses to start on
// anything but a 200 (internal/bridge/bridge.go). A dead JD sidecar answering
// "degraded" there would mark the container unhealthy and restart the app in a
// loop for a fault in a different container. So the detail is a second,
// separate readout, and this file is where it is computed.
//
// EVERY ROW IS DERIVED ON READ AND NOTHING HERE IS STORED. The one exception is
// the cache below, which exists to keep a probe from being run per caller
// rather than to remember an answer - see sysHealthTTL.
//
// STATES ARE IDS AND NEVER PROSE. This process has no idea which of the
// forty-two locales is reading, and two clients of one instance routinely
// differ; the same reason Feature.ID and VolumeReport.Role are ids.

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// started is when this PROCESS came up, not when this App was built.
//
// Package level on purpose, and it is the difference between an uptime and a
// per-object age. The desktop restart relaunches the binary and the container
// restart replaces it, so process start and app start are the same fact in
// every deployment that ships - while a test suite builds hundreds of Apps in
// one process, and a field on App would report each of them as a fresh install
// half a millisecond old.
var started = time.Now()

// SubsystemState is what one part of this instance is doing, as a stable id.
type SubsystemState string

const (
	// StateOK is working, with nothing to report.
	StateOK SubsystemState = "ok"
	// StateDegraded is working AND something is wrong: a relay that is
	// retrying, a folder under its floor, a challenge waiting for a human.
	// Downloads may still be moving, so it is not a failure - and something
	// is being held back, so it is not fine either.
	StateDegraded SubsystemState = "degraded"
	// StateFailed is set up on this instance and not working.
	StateFailed SubsystemState = "failed"
	// StateUnused is not set up here at all. It is deliberately NOT a fault:
	// an instance with no relay and no JD sidecar is a perfectly healthy
	// instance, and colouring those rows red would train somebody to ignore
	// the two that mean something.
	StateUnused SubsystemState = "unused"
	// StateUnknown is a part that cannot be asked on this build or this
	// platform. A third answer, never a quiet zero - the same distinction
	// VolumeReport.Known draws for a disk nothing can measure.
	StateUnknown SubsystemState = "unknown"
)

// The subsystem ids. Stable, lowercase, and the thing an interface looks a
// label up by (health.part.<id>) and a monitoring system groups on.
const (
	SubsystemStore    = "store"
	SubsystemQueue    = "queue"
	SubsystemDisk     = "disk"
	SubsystemJD       = "jd"
	SubsystemYtdlp    = "ytdlp"
	SubsystemAccounts = "accounts"
	SubsystemFeeds    = "feeds"
	SubsystemRelay    = "relay"
	SubsystemCaptcha  = "captcha"
)

// subsystemOrder is the order the rows always come in, and every one of them is
// always present.
//
// Fixed rather than "whatever fired this time" for one reason: a row that
// disappears when it has nothing to say is a row somebody cannot find when they
// go looking for it, and an alert keyed on a series that stops being written
// fires for the whole staleness window after the fault clears. A part that is
// not set up here says StateUnused in its own row instead.
var subsystemOrder = []string{
	SubsystemStore,
	SubsystemQueue,
	SubsystemDisk,
	SubsystemJD,
	SubsystemYtdlp,
	SubsystemAccounts,
	SubsystemFeeds,
	SubsystemRelay,
	SubsystemCaptcha,
}

// The remedy ids. Each is a sentence an interface looks up
// (health.remedy.<id>), never the sentence itself: the server has no language.
// A row with nothing anybody can do about it carries none.
const (
	remedyStoreFailed     = "store.failed"
	remedyJDUnreachable   = "jd.unreachable"
	remedyJDNotWired      = "jd.notWired"
	remedyYtdlpMissing    = "ytdlp.missing"
	remedyDiskLow         = "disk.low"
	remedyDiskElsewhere   = "disk.elsewhere"
	remedyAccountsBenched = "accounts.benched"
	remedyAccountsInvalid = "accounts.invalid"
	remedyFeedsFailing    = "feeds.failing"
	remedyRelayDown       = "relay.down"
	remedyCaptchaWaiting  = "captcha.waiting"
)

// Subsystem is one part of this instance at one moment.
type Subsystem struct {
	// ID is one of the constants above.
	ID string `json:"id"`

	State SubsystemState `json:"state"`

	// Detail is the failing service's OWN words, in whatever language it
	// speaks, and never a sentence this app wrote for a reader to be shown as
	// prose - the same contract JDStatus.Detail and feedRow.Error already
	// carry. Whatever draws it puts it beside a translated state, not instead
	// of one.
	Detail string `json:"detail,omitempty"`

	// Remedy is a stable id an interface looks a sentence up from. Empty for a
	// row nothing can be done about, which is most of them most of the time.
	Remedy string `json:"remedy,omitempty"`

	// Since is when this row last CHANGED state, and it survives the probe
	// cache: a JD that has been unreachable for two hours says so, rather than
	// resetting every thirty seconds to whenever it was last asked.
	//
	// Absent until a state has been observed twice, which is honest - the first
	// reading knows the state and cannot know when it started.
	//
	// omitzero and not omitempty: a struct is never empty to encoding/json, so
	// omitempty would ship a zero time as the year one and every reader would
	// draw 0001-01-01 as a real date.
	Since time.Time `json:"since,omitzero"`
}

// TaskCounts is the download list as it stands, counted.
type TaskCounts struct {
	Running int `json:"running"`
	// Waiting is queued and not running, for any reason. WaitingBy says which.
	Waiting    int `json:"waiting"`
	Paused     int `json:"paused"`
	Extracting int `json:"extracting"`
	// Collected is staged in the link collector and never added to the queue,
	// so it is owed nothing and holds nothing back.
	Collected int `json:"collected"`
	// Disabled is switched off by its own toggle, counted ACROSS the statuses
	// above rather than instead of them - a disabled row is still queued or
	// still collected, and moving it into a bucket of its own would make the
	// buckets stop adding up to the list somebody is looking at.
	Disabled int `json:"disabled"`

	// Failed is how many rows are sitting in the list with an error on them
	// RIGHT NOW.
	//
	// IT IS A GAUGE AND NOT A TALLY, and the difference is the one thing an
	// operator will get wrong about this whole report. It falls when somebody
	// clears a row and when the retention sweep trims the list after
	// KeepFinishedDays, and it says nothing whatever about how often anything
	// has failed. Alerting on `> 0` is therefore alerting on a number that
	// quietly resets itself.
	//
	// A lifetime counter is deliberately NOT added beside it to "fix" that.
	// Lifetime lives in the history table, which is what /api/stats/volume
	// already reads, and a second lifetime figure computed here would be a
	// second answer to a question that already has one.
	Failed int `json:"failed"`

	// WaitingBy is core.Waiting id -> count, FailedBy is core.Reason id ->
	// count. Zero-valued keys are left out; both maps are never nil, so a
	// reader always gets an object rather than JSON null.
	//
	// An unclassified failure is filed under "unknown" rather than under
	// core.ReasonUnknown's own empty string: an empty label reads as a
	// rendering fault in an interface and is illegal as a Prometheus label
	// value nobody set, and "unknown" is a word both sides already have
	// (task.reason.unknown).
	WaitingBy map[string]int `json:"waitingBy"`
	FailedBy  map[string]int `json:"failedBy"`
}

// HealthReport is the whole readout.
type HealthReport struct {
	// Status is the worst row. StateUnused and StateUnknown never make it worse
	// than StateOK: "no relay configured" and "this kernel cannot measure a
	// disk" are not faults, and letting either colour the whole instance is how
	// a status light stops meaning anything.
	Status SubsystemState `json:"status"`

	Version    string `json:"version"`
	Deployment string `json:"deployment"`

	StartedAt time.Time `json:"startedAt"`
	// UptimeSeconds is StartedAt against the server's own clock, computed here
	// rather than left to the reader: a browser in another timezone with a
	// clock a few minutes out would otherwise print an uptime that is wrong by
	// exactly that much, or negative.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	// Subsystems is subsystemOrder, always all of it, never nil.
	Subsystems []Subsystem `json:"subsystems"`

	Tasks TaskCounts `json:"tasks"`

	// Volumes is DiskReport's own rows verbatim, so this page and the Downloads
	// page can never disagree about how much room a folder has. Never nil. Read
	// VolumeReport's own doc comments before drawing any of it - in particular
	// Known, without which the three byte counts mean nothing at all.
	Volumes []VolumeReport `json:"volumes"`

	Halted bool `json:"halted"`
	Quiet  bool `json:"quiet"`

	// SampledAt is when the PROBED rows were taken. They are shared for
	// sysHealthTTL, so this is older than "now" and whatever draws it should
	// say so rather than imply a live gauge. The task counts and the queue row
	// are always current; the disk rows carry DiskReport's own few-second cache
	// on top of this one.
	SampledAt time.Time `json:"sampledAt"`
}

// sysHealthTTL is how long one probe is handed to everybody who asks.
//
// Thirty seconds, and the number is set by the slowest thing behind it rather
// than by taste. App.JDStatus does a Ping and then a Version against a client
// with a fifteen-second timeout, so a JD sidecar that has gone away costs up to
// THIRTY SECONDS per call - and "the sidecar has gone away" is exactly the
// state this feature exists to report. A monitoring system scraping every
// fifteen seconds against an uncached probe would stack a goroutine per scrape,
// each holding an open connect until its own timeout kills it, and would then
// report the app as down for a reason that is the monitoring itself.
//
// It is long compared with diskReportTTL (five seconds) for the same reason it
// is long in absolute terms: nothing here is a gauge somebody watches move.
const sysHealthTTL = 30 * time.Second

// storePingTimeout bounds the one database question this report asks.
//
// Two seconds. The store holds a single connection shared with every reader and
// writer in the process, and Vacuum keeps it for the whole of a rewrite, so a
// ping with no deadline can sit behind a ten-minute compaction. What is
// reported here is "the database did not answer within two seconds", which on a
// box mid-compaction is both true and the thing worth knowing.
const storePingTimeout = 2 * time.Second

// sysProbe is the part of the report that costs something to obtain: the
// database question, the JD sidecar's own status, and the disk walk.
type sysProbe struct {
	// rows is id -> row for everything EXCEPT the queue, which is assembled per
	// call because it is free and because a stale halt flag beside a live task
	// count would be two answers to one question.
	rows      map[string]Subsystem
	volumes   []VolumeReport
	sampledAt time.Time
}

// sysHealthState is one App's cached probe.
//
// Package level keyed by *App rather than a field on App, exactly as
// diskReportState (app_diskreport.go) and acctHealthReg (app_health.go) already
// are, and for the reason both of them write down: app.go's struct is not this
// file's to grow.
type sysHealthState struct {
	mu sync.Mutex
	// probe is the last reading and at is when it was taken. A zero at means
	// nothing has been probed yet, which is the only case anybody waits for.
	probe sysProbe
	at    time.Time
	// inflight is non-nil while a probe is running and is closed when it lands.
	// It is what makes this single-flight rather than a bare staleness check,
	// and the difference shows up exactly when it hurts: five tabs arriving in
	// one cold window against a JD that has stopped answering.
	inflight chan struct{}

	// seen and since are what makes Subsystem.Since mean anything. They are
	// kept out of the probe on purpose: the queue row is assembled outside it
	// and still needs a Since, and the states have to outlive a probe being
	// replaced.
	seen  map[string]SubsystemState
	since map[string]time.Time
}

var (
	sysHealthMu  sync.Mutex
	sysHealthReg = map[*App]*sysHealthState{}
)

// sysHealthStateFor returns this App's cached probe, building it on first use.
func (a *App) sysHealthStateFor() *sysHealthState {
	sysHealthMu.Lock()
	defer sysHealthMu.Unlock()
	st, ok := sysHealthReg[a]
	if !ok {
		st = &sysHealthState{
			// Empty rather than nil for the reason diskReportStateFor gives:
			// what has never been measured still has to encode as an empty
			// collection, because a reader that walks over null throws instead
			// of drawing nothing.
			probe: sysProbe{rows: map[string]Subsystem{}, volumes: []VolumeReport{}},
			seen:  map[string]SubsystemState{},
			since: map[string]time.Time{},
		}
		sysHealthReg[a] = st
	}
	return st
}

// HealthReport is every part of this instance with its own state, the queue by
// why it is waiting and why it failed, room on the target folders, and how long
// this process has been up.
//
// The probed rows are shared for sysHealthTTL, and a caller arriving while
// somebody else is probing is handed the PREVIOUS reading rather than made to
// wait for the new one - the identical arrangement, and the identical reason,
// as DiskReport next door. The first caller of all does wait, because there is
// nothing yet to hand it.
//
// Everything cheap is recomputed here on every call: the task walk is a map
// walk with no syscall in it, and the queue's own switch is one mutex.
func (a *App) HealthReport() HealthReport {
	probe := a.sysHealthProbe()

	q := a.Queue()
	rows := make([]Subsystem, 0, len(subsystemOrder))
	for _, id := range subsystemOrder {
		if id == SubsystemQueue {
			rows = append(rows, queueSubsystem(q))
			continue
		}
		row, ok := probe.rows[id]
		if !ok {
			// Unreachable in this build: sysProbe fills every id but the queue.
			// It stays rather than being an index into a slice, because the
			// alternative failure is a row silently missing from the list, and
			// a missing row reads as a part that does not exist rather than as
			// a part nothing asked about.
			row = Subsystem{ID: id, State: StateUnknown}
		}
		rows = append(rows, row)
	}
	a.stampSince(rows)

	return HealthReport{
		Status:        worstState(rows),
		Version:       buildinfo.Version,
		Deployment:    buildinfo.Deployment,
		StartedAt:     started,
		UptimeSeconds: int64(time.Since(started) / time.Second),
		Subsystems:    rows,
		Tasks:         a.healthTaskCounts(),
		Volumes:       probe.volumes,
		Halted:        q.Halted,
		Quiet:         q.Quiet,
		SampledAt:     probe.sampledAt,
	}
}

// sysHealthProbe returns the shared probe, running one if the last is stale.
// The single-flight dance is DiskReport's, line for line, because it is the
// same problem: an expensive reading behind a route that several tabs poll.
func (a *App) sysHealthProbe() sysProbe {
	st := a.sysHealthStateFor()
	st.mu.Lock()
	if !st.at.IsZero() && time.Since(st.at) < sysHealthTTL {
		p := st.probe
		st.mu.Unlock()
		return p
	}
	if ch := st.inflight; ch != nil {
		if !st.at.IsZero() {
			p := st.probe
			st.mu.Unlock()
			return p
		}
		st.mu.Unlock()
		<-ch
		st.mu.Lock()
		p := st.probe
		st.mu.Unlock()
		return p
	}
	ch := make(chan struct{})
	st.inflight = ch
	st.mu.Unlock()
	// Cleared and closed even if the probe panics, so one bad reading cannot
	// leave every later caller waiting on one that will never land.
	defer func() {
		st.mu.Lock()
		st.inflight = nil
		st.mu.Unlock()
		close(ch)
	}()

	p := a.sampleSysHealth()
	st.mu.Lock()
	// Aged from when the probe STARTED rather than from when it finished: the
	// reading describes that moment, and a probe that spent thirty seconds
	// waiting for a dead sidecar must not hand itself a fresh timestamp and be
	// served for another thirty.
	st.probe, st.at = p, p.sampledAt
	st.mu.Unlock()
	return p
}

// sampleSysHealth takes one probe. Only sysHealthProbe calls it, and only ever
// one at a time.
func (a *App) sampleSysHealth() sysProbe {
	cfg := a.Settings.Get()
	disk := a.DiskReport()

	p := sysProbe{
		rows:      map[string]Subsystem{},
		volumes:   disk.Volumes,
		sampledAt: time.Now(),
	}
	for _, row := range []Subsystem{
		a.storeSubsystem(),
		diskSubsystem(disk, cfg),
		a.jdSubsystem(),
		a.ytdlpSubsystem(),
		a.accountsSubsystem(),
		a.feedsSubsystem(cfg),
		a.relaySubsystem(cfg),
		a.captchaSubsystem(),
	} {
		p.rows[row.ID] = row
	}
	return p
}

// storeSubsystem asks the database whether it is still there.
//
// A failed ping is deliberately NOT reported as the whole instance being down:
// transfers already running keep writing bytes to disk, because the engine does
// not go through the store to move a file. What stops is the bookkeeping, which
// is why the remedy says a restart would lose everything changed since rather
// than "downloads have stopped".
func (a *App) storeSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemStore}
	if a.Store == nil {
		// Only reachable from a test harness that built an App without one. A
		// third answer rather than a green light: nothing was asked, so nothing
		// is known.
		row.State = StateUnknown
		return row
	}
	ctx, cancel := context.WithTimeout(context.Background(), storePingTimeout)
	defer cancel()
	if err := a.Store.Ping(ctx); err != nil {
		row.State, row.Detail, row.Remedy = StateFailed, err.Error(), remedyStoreFailed
		return row
	}
	row.State = StateOK
	return row
}

// queueSubsystem is the master switch, and it is NEVER StateFailed.
//
// A stopped queue is a choice somebody made, or a timetable window they wrote,
// and reporting it as a fault would page an operator for a working pause. It is
// degraded rather than ok because something IS being held back and the rows
// underneath say so.
func queueSubsystem(q QueueState) Subsystem {
	row := Subsystem{ID: SubsystemQueue, State: StateOK}
	if q.Halted || q.Quiet {
		row.State = StateDegraded
	}
	return row
}

// diskSubsystem reads the same report the Downloads page draws, so the two can
// never disagree about a folder.
//
// The order of the tests below is the order they matter in. A volume under the
// pause floor is a failure whatever else is true of the list, because a
// download that was already running has been put back; a volume under the start
// floor is degraded, because nothing new starts there and what is running is
// untouched. The substitution warning comes last and only when nothing worse
// applies - it is a caveat about the numbers rather than a fault in them.
//
// A row that could not be measured is skipped in every one of those tests. Its
// three byte counts are zero and mean NOTHING (VolumeReport.Known says so at
// length), and reading them as "no space left" would report a full disk that
// does not exist. When no row at all could be measured, the whole part is
// unknown, which is the third answer this build already spells everywhere else.
func diskSubsystem(rep DiskReport, cfg settings.Settings) Subsystem {
	row := Subsystem{ID: SubsystemDisk}
	measured := 0
	low, critical, elsewhere := false, false, false
	for _, v := range rep.Volumes {
		if !v.Exists && v.Measured != v.Dir {
			elsewhere = true
		}
		if !v.Known {
			continue
		}
		measured++
		if cfg.DiskCriticalSpace > 0 && v.Free < uint64(cfg.DiskCriticalSpace) {
			critical = true
		}
		if cfg.DiskLowSpace > 0 && v.Free < uint64(cfg.DiskLowSpace) {
			low = true
		}
	}
	switch {
	case measured == 0:
		// No folder on this box could be asked. Not "there is no disk": on a
		// kernel internal/diskspace has no call for, every guard in the app
		// already holds nothing back, and this row saying so is the only place
		// that becomes visible.
		row.State = StateUnknown
	case critical:
		row.State, row.Remedy = StateFailed, remedyDiskLow
	case low:
		row.State, row.Remedy = StateDegraded, remedyDiskLow
	case elsewhere:
		row.State, row.Remedy = StateDegraded, remedyDiskElsewhere
	default:
		row.State = StateOK
	}
	return row
}

// jdSubsystem is the headless JDownloader sidecar, and it is the one row in
// this file that exists because the module registry's own row gets it wrong.
//
// routes_features.go's jdDetail reports the literal word "reachable" whenever
// ContainerBackendConfigured() is true, and that only reads whether a.jd is
// non-nil. a.jd is set once, in rewireBackends, which runs at boot and on an
// account change - so a JD that died yesterday still reads "reachable" today.
// This row asks the sidecar instead.
//
// THE MIRROR CASE MATTERS AS MUCH and is the reason for the second branch: a JD
// that comes BACK is answering again while a.jd is still nil, because nothing
// calls rewireBackends when a sidecar returns. Without that branch an operator
// reads "JD is fine" here while links go on queueing behind a backend that is
// not registered.
//
// The status itself comes through the shared probe, never from a bare
// App.JDStatus() at the call site: that call is live by design and costs up to
// thirty seconds against a sidecar that has gone away.
func (a *App) jdSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemJD}
	if strings.TrimSpace(os.Getenv("KL_JD")) == "" {
		// No sidecar was ever configured here. Not a fault, and not something
		// to advise about: an instance without JD is an ordinary instance that
		// refuses encrypted containers with a stated reason.
		row.State = StateUnused
		return row
	}
	st := a.JDStatus()
	if !st.Reachable {
		row.State, row.Detail, row.Remedy = StateFailed, st.Detail, remedyJDUnreachable
		return row
	}
	a.bmu.RLock()
	wired := a.jd != nil
	a.bmu.RUnlock()
	if !wired {
		row.State, row.Remedy = StateDegraded, remedyJDNotWired
		return row
	}
	row.State, row.Detail = StateOK, st.Detail
	return row
}

// ytdlpSubsystem reads the live routing table rather than the environment.
//
// rewireBackends only registers the resolver once the binary has actually run
// (Backend.Available), so this is the same "derived from live state, never a
// stored flag" signal every module row already uses. Missing is StateUnused and
// not StateFailed: nothing is broken, this build simply has no yt-dlp, and
// media pages then fail with the site's own error instead of being opened up
// into variants.
func (a *App) ytdlpSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemYtdlp}
	if a.Registry == nil || !a.resolverRegistered("ytdlp") {
		row.State, row.Remedy = StateUnused, remedyYtdlpMissing
		return row
	}
	row.State = StateOK
	return row
}

// resolverRegistered reports whether a resolver with this id is on the live
// routing table right now.
//
// A copy of the identical helper in internal/api/routes_features.go, and
// deliberately not a shared one: that package cannot import this one's
// internals and this one must not import that package at all (the api layer
// depends on app, never the other way round). Six lines duplicated beats an
// import cycle or a third package holding one loop.
func (a *App) resolverRegistered(id string) bool {
	for _, rid := range a.Registry.IDs() {
		if rid == id {
			return true
		}
	}
	return false
}

// accountsSubsystem walks the configured accounts against the health tracker
// app_health.go keeps.
//
// Both reads are cached: AccountStates reports what the account-health ticker
// last found and never makes a call of its own (its own doc comment says so),
// and the tracker is a file read once per App. So this row costs nothing worth
// putting behind the probe cache for its own sake - it is inside it only
// because it is assembled with the two that do.
//
// A refused credential is a failure and a benched one is not, because the
// remedies are opposites: an invalid key needs a person to go and replace it,
// a benched one is retried on its own with the wait doubling each time, and
// telling somebody to act on the second is telling them to do nothing useful.
func (a *App) accountsSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemAccounts}
	states := a.AccountStates()
	if len(states) == 0 {
		row.State = StateUnused
		return row
	}
	tr := a.acctHealthTracker()
	benched, invalid := false, false
	for _, st := range states {
		if !st.Enabled {
			// Switched off by hand is not a fault, exactly as a queue somebody
			// halted is not. It also stops claiming links (rewireBackends), so
			// nothing is waiting on it.
			continue
		}
		switch tr.Get(st.Service, st.Account).State {
		case accounts.HealthTempDisabled:
			benched = true
		case accounts.HealthInvalid, accounts.HealthExpired:
			invalid = true
		}
	}
	switch {
	case invalid:
		row.State, row.Remedy = StateFailed, remedyAccountsInvalid
	case benched:
		row.State, row.Remedy = StateDegraded, remedyAccountsBenched
	default:
		row.State = StateOK
	}
	return row
}

// feedsSubsystem joins the configured subscriptions onto the live poller, the
// same join GET /api/feeds already does and for the same reason: the row that
// matters most is the one MISSING from the runner, a subscription that is saved
// and not being polled at all.
//
// It is degraded and never failed. A feed that has been answering 403 for a
// fortnight adds nothing new, and nothing else about the instance is affected -
// what is already downloading is unaffected, and so is everything anybody adds
// by hand.
func (a *App) feedsSubsystem(cfg settings.Settings) Subsystem {
	row := Subsystem{ID: SubsystemFeeds}
	if len(cfg.Feeds) == 0 {
		row.State = StateUnused
		return row
	}
	live := map[string]string{}
	polled := map[string]bool{}
	for _, h := range a.FeedHealth() {
		live[h.URL] = h.LastError
		polled[h.URL] = true
	}
	failing := ""
	for _, s := range cfg.Feeds {
		url := strings.TrimSpace(s.URL)
		if !polled[url] {
			// Configured and not being polled: the poller refused it, or the
			// runner is not up. Validate is what /api/feeds asks in the same
			// position, and its sentence is the far end's own words the way
			// every Detail in this file is.
			if err := s.Validate(); err != nil {
				failing = err.Error()
			} else if failing == "" {
				failing = "this subscription is not being polled"
			}
			continue
		}
		if e := live[url]; e != "" && failing == "" {
			failing = e
		}
	}
	if failing != "" {
		row.State, row.Detail, row.Remedy = StateDegraded, failing, remedyFeedsFailing
		return row
	}
	row.State = StateOK
	return row
}

// relaySubsystem is whether this instance can be reached from outside through
// its relay.
//
// Degraded and not failed when the socket is down, and that is the whole
// judgement: the client retries by itself, every local thing works exactly as
// it always did, and the only thing that does not work is somebody arriving
// from elsewhere. Calling that a failure would put the instance in the red for
// a fault at the other end of a wire.
func (a *App) relaySubsystem(cfg settings.Settings) Subsystem {
	row := Subsystem{ID: SubsystemRelay}
	if strings.TrimSpace(cfg.RelayURL) == "" {
		row.State = StateUnused
		return row
	}
	if a.Federation != nil && a.Federation.RelayConnected() {
		row.State = StateOK
		return row
	}
	row.State, row.Remedy = StateDegraded, remedyRelayDown
	return row
}

// captchaSubsystem is whether anything has stopped to ask a human.
//
// CaptchaChallenges is a cache read and never a live call to the sidecar (its
// own doc comment), so this costs nothing. A challenge waiting is degraded
// rather than a fault: nothing is broken, and everything behind it is stopped
// until somebody answers, which is precisely the distinction between the two
// words.
func (a *App) captchaSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemCaptcha}
	if strings.TrimSpace(os.Getenv("KL_JD")) == "" {
		// The only source this build relays is the JD sidecar
		// (internal/captcha.JDSource), so with no sidecar there is nothing that
		// could ever ask.
		row.State = StateUnused
		return row
	}
	if len(a.CaptchaChallenges()) > 0 {
		row.State, row.Remedy = StateDegraded, remedyCaptchaWaiting
		return row
	}
	row.State = StateOK
	return row
}

// healthTaskCounts is ONE walk of the task list.
//
// One walk and not one per counter, or two figures on the same card can
// straddle a change and add up to a list nobody has. And a.mu is taken here and
// released before anything else happens, for the reason queueDemand's own
// header spells out: the dispatcher already pays for a hung mount once a pass
// with a.mu in hand, and a GET that every open tab makes may not join in. There
// is no syscall inside this loop at all.
func (a *App) healthTaskCounts() TaskCounts {
	c := TaskCounts{WaitingBy: map[string]int{}, FailedBy: map[string]int{}}
	a.mu.Lock()
	for _, t := range a.tasks {
		if t == nil {
			continue
		}
		switch t.Status {
		case core.StatusRunning:
			c.Running++
		case core.StatusQueued:
			c.Waiting++
			if t.Waiting != core.WaitingNone {
				// Left out rather than filed under an empty label: a queued
				// task with no reason on it is one the dispatcher is about to
				// take, not one being held back by something unnamed. The
				// breakdown is "why are these waiting", and it may not invent
				// an answer for the ones that are simply next.
				c.WaitingBy[string(t.Waiting)]++
			}
		case core.StatusPaused:
			c.Paused++
		case core.StatusExtracting:
			c.Extracting++
		case core.StatusCollected:
			c.Collected++
		case core.StatusError:
			c.Failed++
			c.FailedBy[reasonKey(t.Reason)]++
		}
		if !t.Enabled {
			c.Disabled++
		}
	}
	a.mu.Unlock()
	return c
}

// reasonKey is a failure reason as a label. See TaskCounts.FailedBy for why an
// unclassified failure is "unknown" rather than the empty string core.Reason
// actually carries.
func reasonKey(r core.Reason) string {
	if r == core.ReasonUnknown {
		return "unknown"
	}
	return string(r)
}

// stampSince fills in Since on every row, in place.
//
// It is what makes "JD has been down for two hours" different from "JD was
// down when we last looked", and it has to live outside the probe cache for two
// reasons: the queue row is assembled per call and would otherwise never get
// one, and a probe being replaced must not reset the clock on a state that did
// not change.
//
// The FIRST time a state is seen it gets no timestamp at all. That is honest
// rather than tidy: this process knows what the state is and cannot know when
// it started, and stamping "now" would tell somebody a sidecar failed the
// moment they opened the page.
func (a *App) stampSince(rows []Subsystem) {
	st := a.sysHealthStateFor()
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range rows {
		id := rows[i].ID
		prev, known := st.seen[id]
		switch {
		case !known:
			st.seen[id] = rows[i].State
		case prev != rows[i].State:
			st.seen[id] = rows[i].State
			st.since[id] = now
		}
		rows[i].Since = st.since[id]
	}
}

// worstState is the whole instance's own state.
//
// StateUnused and StateUnknown carry no weight at all, which is the one rule
// worth stating twice: an instance with no relay, no sidecar and no yt-dlp is
// four rows of "not in use here", and letting any of them tint the summary
// would mean a fresh install reports itself as impaired forever.
func worstState(rows []Subsystem) SubsystemState {
	worst := StateOK
	for _, r := range rows {
		switch r.State {
		case StateFailed:
			return StateFailed
		case StateDegraded:
			worst = StateDegraded
		}
	}
	return worst
}
