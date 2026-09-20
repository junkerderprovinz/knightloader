package app

// The subsystem health report: every part of this instance that can fail on
// its own with its state, the queue counted by why it waits and why it failed,
// room on the target folders, and uptime.
//
// This is unrelated to the account health in app_health.go, which is why
// everything here is named sysHealth* or Subsystem*; accounts.HealthState is
// already in use in this package.
//
// It is separate from /api/health, which must keep answering a plain "ok": the
// phone app's discovery, the container HEALTHCHECK and the Click'n'Load bridge
// depend on it, and reporting a dead JD sidecar there would restart the app in
// a loop for a fault in another container.
//
// Every row is derived on read; the cache only keeps probes from running once
// per caller. States and remedies are ids that the client translates.

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

// started is when the process came up. It is package level so that tests,
// which build many Apps in one process, do not each report a fresh start.
var started = time.Now()

// SubsystemState is what one part of this instance is doing, as a stable id.
type SubsystemState string

const (
	// StateOK is working, with nothing to report.
	StateOK SubsystemState = "ok"
	// StateDegraded is working while something is held back: a retrying relay,
	// a folder under its floor, a challenge waiting for a human.
	StateDegraded SubsystemState = "degraded"
	// StateFailed is set up on this instance and not working.
	StateFailed SubsystemState = "failed"
	// StateUnused is not set up here. It is not a fault; an instance without a
	// relay or JD sidecar is healthy.
	StateUnused SubsystemState = "unused"
	// StateUnknown is a part that cannot be asked on this build or platform.
	StateUnknown SubsystemState = "unknown"
)

// Subsystem ids. The client looks up labels by them (health.part.<id>) and
// monitoring groups on them.
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

// subsystemOrder is the fixed order of the rows, and every row is always
// present: a row that disappears cannot be found, and an alert on a series that
// stops being written keeps firing after the fault clears.
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

// Remedy ids, looked up by the client as health.remedy.<id>. A row nothing can
// be done about carries none.
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
	// ID is one of the subsystem ids.
	ID string `json:"id"`

	State SubsystemState `json:"state"`

	// Detail is the failing service's own words, shown beside the translated
	// state, never instead of it.
	Detail string `json:"detail,omitempty"`

	// Remedy is a remedy id, or empty.
	Remedy string `json:"remedy,omitempty"`

	// Since is when the row last changed state, kept across probes. It is
	// absent until a change has been observed, since the first reading cannot
	// know when its state began. omitzero keeps a zero time out of the JSON.
	Since time.Time `json:"since,omitzero"`
}

// TaskCounts is the download list, counted.
type TaskCounts struct {
	Running int `json:"running"`
	// Waiting is queued and not running, for any reason; WaitingBy says which.
	Waiting    int `json:"waiting"`
	Paused     int `json:"paused"`
	Extracting int `json:"extracting"`
	// Collected is staged in the link collector and not yet queued.
	Collected int `json:"collected"`
	// Disabled counts switched-off rows across the statuses above, so the
	// status buckets still add up to the list.
	Disabled int `json:"disabled"`

	// Failed is how many rows currently carry an error. It is a gauge, not a
	// tally: it drops when rows are cleared or trimmed. Lifetime figures come
	// from the history (/api/stats/volume).
	Failed int `json:"failed"`

	// WaitingBy maps core.Waiting to a count and FailedBy core.Reason to a
	// count, without zero entries and never nil. An unclassified failure is
	// filed as "unknown", since an empty label is useless in a UI and in
	// Prometheus.
	WaitingBy map[string]int `json:"waitingBy"`
	FailedBy  map[string]int `json:"failedBy"`
}

// HealthReport is the whole readout.
type HealthReport struct {
	// Status is the worst row. Unused and unknown rows never make it worse
	// than StateOK.
	Status SubsystemState `json:"status"`

	Version    string `json:"version"`
	Deployment string `json:"deployment"`

	StartedAt time.Time `json:"startedAt"`
	// UptimeSeconds is computed on the server, so a client clock that is off
	// cannot distort it.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	// Subsystems is every row in subsystemOrder, never nil.
	Subsystems []Subsystem `json:"subsystems"`

	Tasks TaskCounts `json:"tasks"`

	// Volumes is DiskReport's rows verbatim, so this page and the Downloads
	// page agree. Never nil; see VolumeReport.Known before drawing the bytes.
	Volumes []VolumeReport `json:"volumes"`

	Halted bool `json:"halted"`
	Quiet  bool `json:"quiet"`

	// SampledAt is when the probed rows were taken; they are shared for
	// sysHealthTTL. Task counts and the queue row are always current.
	SampledAt time.Time `json:"sampledAt"`
}

// sysHealthTTL is how long one probe is shared. JDStatus can take up to thirty
// seconds against a dead sidecar, and a monitor scraping faster than that
// against an uncached probe would pile up goroutines.
const sysHealthTTL = 30 * time.Second

// storePingTimeout bounds the database ping. The store's single connection can
// be held by a long VACUUM, and not answering within two seconds is then the
// right thing to report.
const storePingTimeout = 2 * time.Second

// sysProbe is the costly part of the report: the database ping, the JD status
// and the disk walk.
type sysProbe struct {
	// rows maps id to row for everything except the queue, which is built per
	// call so its halt flag matches the live task counts.
	rows      map[string]Subsystem
	volumes   []VolumeReport
	sampledAt time.Time
}

// sysHealthState is one App's cached probe, kept in a package-level map keyed
// by *App.
type sysHealthState struct {
	mu sync.Mutex
	// probe is the last reading and at when it was taken; zero at means none
	// yet.
	probe sysProbe
	at    time.Time
	// inflight is non-nil while a probe runs and is closed when it lands,
	// which keeps probing single-flight.
	inflight chan struct{}

	// seen and since back Subsystem.Since. They live outside the probe so the
	// queue row gets one too and a new probe does not reset the clock.
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
			// Empty rather than nil, which would encode as JSON null.
			probe: sysProbe{rows: map[string]Subsystem{}, volumes: []VolumeReport{}},
			seen:  map[string]SubsystemState{},
			since: map[string]time.Time{},
		}
		sysHealthReg[a] = st
	}
	return st
}

// HealthReport returns the full health readout. Probed rows are shared for
// sysHealthTTL and served stale while a new probe runs, as DiskReport does;
// everything cheap is recomputed on every call.
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
			// A missing row would read as a part that does not exist.
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

// sysHealthProbe returns the shared probe, running one if the last is stale,
// with the same single-flight scheme as DiskReport.
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
	// Deferred so a panicking probe cannot leave later callers waiting.
	defer func() {
		st.mu.Lock()
		st.inflight = nil
		st.mu.Unlock()
		close(ch)
	}()

	p := a.sampleSysHealth()
	st.mu.Lock()
	// Aged from when the probe started, so a slow probe does not look fresh.
	st.probe, st.at = p, p.sampledAt
	st.mu.Unlock()
	return p
}

// sampleSysHealth takes one probe. Only sysHealthProbe calls it, one at a time.
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

// storeSubsystem pings the database. A failure stops the bookkeeping, not the
// running transfers, which write to disk without the store.
func (a *App) storeSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemStore}
	if a.Store == nil {
		// Only a test harness builds an App without a store.
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

// queueSubsystem reports the master switch. A stopped queue is a choice or a
// schedule window, so it is degraded, never failed.
func queueSubsystem(q QueueState) Subsystem {
	row := Subsystem{ID: SubsystemQueue, State: StateOK}
	if q.Halted || q.Quiet {
		row.State = StateDegraded
	}
	return row
}

// diskSubsystem reads the same report the Downloads page draws. Below the
// pause floor is a failure, since running downloads were put back; below the
// start floor is degraded; a folder measured at a parent is a caveat, reported
// only when nothing worse applies. Unmeasured rows are skipped, and when no
// row could be measured the state is unknown.
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

// jdSubsystem asks the JD sidecar itself. The feature registry only checks
// that a.jd is set, which stays true after JD dies and stays false when JD
// comes back, because rewireBackends only runs at boot and on account changes.
// It runs inside the probe cache, since JDStatus can take thirty seconds.
func (a *App) jdSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemJD}
	if strings.TrimSpace(os.Getenv("KL_JD")) == "" {
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

// ytdlpSubsystem reads the live routing table; rewireBackends only registers
// yt-dlp once the binary has run. A missing yt-dlp is unused, not failed.
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
// routing table. internal/api has the same helper, but importing it here would
// create a cycle.
func (a *App) resolverRegistered(id string) bool {
	for _, rid := range a.Registry.IDs() {
		if rid == id {
			return true
		}
	}
	return false
}

// accountsSubsystem checks the configured accounts against the health
// tracker. Both reads are cached. A refused credential needs a person and is a
// failure; a benched one retries by itself and is only degraded.
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
			// Switched off by hand, so it claims no links.
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

// feedsSubsystem joins the configured subscriptions onto the live poller, like
// GET /api/feeds, so a saved subscription that is not polled at all shows up.
// A failing feed affects nothing else, so this is degraded at worst.
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
			// Configured but not polled: the poller refused it, or the runner
			// is not up.
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

// relaySubsystem reports whether the instance is reachable through its relay.
// A down socket is degraded: the client retries and everything local works.
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

// captchaSubsystem reports whether anything is waiting for a human. It reads
// the cache only. A waiting challenge is degraded: nothing is broken, but what
// is behind it has stopped.
func (a *App) captchaSubsystem() Subsystem {
	row := Subsystem{ID: SubsystemCaptcha}
	if strings.TrimSpace(os.Getenv("KL_JD")) == "" {
		// The JD sidecar is the only captcha source.
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

// healthTaskCounts counts the task list in one walk, so the figures cannot
// straddle a change, and holds a.mu only for the walk.
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
				// A queued task without a waiting reason is simply next.
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

// reasonKey returns a failure reason as a label, "unknown" for the empty
// core.ReasonUnknown.
func reasonKey(r core.Reason) string {
	if r == core.ReasonUnknown {
		return "unknown"
	}
	return string(r)
}

// stampSince fills in Since on every row. The first time a state is seen it
// gets no timestamp, since the process cannot know when that state began.
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

// worstState returns the instance's overall state. Unused and unknown rows
// carry no weight, or a fresh install would always look impaired.
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
