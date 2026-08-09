// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; a download backend (the Gopeed engine or
// headless JD) reports changes, the app persists them and broadcasts them.
//
// The package is split by subject rather than by layer, so that two people
// working on unrelated parts of the download manager are not editing the same
// file: this one holds the App itself and its lifecycle, app_links.go the way a
// link becomes a task, app_queue.go the wait queue, app_dispatch.go the handover
// to a backend and everything a backend reports back, app_tasks.go per-task
// edits and persistence, app_extract.go unpacking, app_bulk.go the operations
// that act on a whole selection, app_boot.go what a restart leaves behind and
// the housekeeping that keeps the list bounded, app_mirror.go the second copy of
// a file the list already has, and app_accounts.go the credentials and the
// backend routing they decide.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/knightloader/internal/throttle"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// backend is a download backend: the embedded Gopeed engine or headless JD.
// Both report progress through the app's onUpdate callback.
type backend interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// probeTimeout bounds one collector HEAD. It is short on purpose: the user is
// waiting at the paste box, and a host that accepts the connection and then
// stops talking must not decide how long staging takes.
const probeTimeout = 10 * time.Second

// doer is the part of an HTTP client this package's probe uses. It is declared
// here rather than in internal/httpx because the consumer owns the interface:
// httpx hands out a *http.Client, and a test hands out whatever answers without
// leaving the machine.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type App struct {
	Store      *store.Store
	Engine     *engine.Engine
	Hub        *hub.Hub
	Registry   *resolver.Registry
	Accounts   *accounts.Store
	Settings   *settings.Store
	Federation *federation.Manager
	Auth       *auth.Guard
	// Throttle is the shared bandwidth allowance for everything downloading
	// through the loopback proxy.
	Throttle *throttle.Limiter
	// Crawler turns a pasted page into the files it links to.
	Crawler crawler.Crawler
	// Reconnector asks the router for a new public address, which is the only
	// thing that lifts a hoster limit keyed to the one this box has.
	Reconnector *reconnect.Reconnector

	// Probe is the client the collector's HEAD requests go out on, to learn a
	// staged link's size and whether it is still there.
	//
	// It is a field rather than a client built inside analyze because a probe
	// that nothing can replace is a probe no test can control: the collector
	// fires it from AddLinks, so any test that stages a link races a real DNS
	// lookup, and the test that proved a late answer cannot erase a refusal was
	// failing on CI for exactly that reason - not on what it was testing, but on
	// which of the two writers happened to finish first.
	Probe doer

	// ctx is cancelled by Close. It bounds work that outlives the call that
	// started it: a reconnect can hold the line for the whole configured timeout,
	// and a shutdown must not wait two minutes for a router to answer — nor fire
	// a reboot command on its way out and drop every download still running.
	ctx    context.Context
	cancel context.CancelFunc

	// sched applies the user's timetable to the queue. It owns one goroutine and
	// is the only writer of the speed limit, so a saved settings page cannot lift
	// a nightly cap that is still in force.
	sched *schedule.Runner

	// wg counts the goroutines this package starts and keeps for the life of the
	// app - the housekeeping loop, and nothing else so far. Close waits on it,
	// which is the only reason it exists: every one of them writes to the store,
	// and the store is closed on the way out.
	//
	// It is deliberately not a counter for the goroutines that carry a download.
	// Those are abandoned, Close says so, and boot is what puts the list right
	// afterwards.
	wg sync.WaitGroup

	jd     backend            // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp  backend            // yt-dlp media backend, nil unless the yt-dlp binary is present
	torbox backend            // TorBox debrid backend, nil unless a TorBox key is present
	debrid map[string]backend // one-shot debrid backends by resolver id (alldebrid, realdebrid)

	dlDir string           // where engine + yt-dlp downloads land (extraction source)
	proxy *netproxy.Server // loopback proxy the engine downloads through

	// wmu guards watcher, which is replaced whenever the watched folder changes.
	wmu     sync.Mutex
	watcher *watch.Watcher

	// bmu guards the backend fields above. It is deliberately separate from mu:
	// re-wiring does network calls, and task state must not wait for those.
	bmu sync.RWMutex

	// rmu guards the compiled rule sets, which are replaced wholesale whenever
	// the settings are saved and never edited in place. It is separate from mu
	// because the filter is consulted while a link is being staged, and that must
	// not queue behind whatever the task list is doing.
	rmu       sync.RWMutex
	pkgRules  *rules.Matcher
	pkgProb   []rules.Problem
	filtRules *rules.Matcher
	filtProb  []rules.Problem

	mu sync.Mutex
	// halted stops the dispatcher from handing anything new to a backend.
	// Running downloads keep running: this is a queue switch, not a kill
	// switch, and a stop that abandons half-written bytes is a different
	// button with a different warning.
	halted bool
	// manualHalt is the halt the user set by hand, kept apart from halted because
	// a schedule window writes that one too. It is the base the timetable is
	// evaluated against, so a stop made at 03:00 is still in force when a window
	// ends at 06:00 instead of being lifted by it.
	manualHalt bool
	// dupes answers "is this link already in the list". It is not safe for
	// concurrent use, so every call to it happens under mu.
	dupes *dedupe.Set
	// picker chooses which configured connection carries a download, and owns the
	// ban list. Rebuilt on every settings save, because building it is also what
	// settles the bans against the new list of rows - see proxycfg.NewPicker.
	//
	// Nil until the first build, and nil means "leave by this machine's own
	// address", which is also what an empty list means. Read under mu.
	picker *proxycfg.Picker
	// bans outlives every picker, so a connection refused by a host does not get
	// a clean slate every time the user saves an unrelated setting.
	bans *proxycfg.Bans
	// skipped is the trace of links that never became tasks, newest last.
	skipped []SkippedLink
	// stopMark is the task whose completion halts the queue. It is how you say
	// "finish this, then stop" without sitting and watching for it.
	stopMark string
	tasks    map[string]*core.Task
	queue    []string        // task IDs waiting for a slot, FIFO with per-host skip-ahead
	active   map[string]bool // dispatched and not yet terminal/paused
	started  map[string]bool // ever handed to a backend (Resume vs fresh Download)
	// unpack is the extraction worker: the jobs, the order they run in, and the
	// one goroutine that runs them. Built on first use rather than in New, so
	// unpacking stays a subject of app_extract.go alone - see unpackLocked.
	unpack *unpackState
}

func New(dataDir string) (*App, error) {
	st, err := store.Open(filepath.Join(dataDir, "knightloader.db"))
	if err != nil {
		return nil, err
	}
	cfg, err := settings.Load(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	fed, err := federation.Load(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a := &App{
		Store:      st,
		Hub:        hub.New(),
		Registry:   resolver.NewRegistry(),
		Settings:   cfg,
		Federation: fed,
		dlDir:      filepath.Join(dataDir, "downloads"),
		Throttle:   throttle.New(),
		tasks:      map[string]*core.Task{},
		active:     map[string]bool{},
		started:    map[string]bool{},
		debrid:     map[string]backend{},
	}
	// Every outbound client is built from internal/httpx, so the proxy, the user
	// agent, the redirect rule and the connection pool are one policy instead of
	// one http.Client literal per subsystem. Each subsystem still gets its own
	// client: a router that holds its connections open must not be able to
	// occupy the pool a crawl needs.
	a.Crawler = crawler.HTML{Client: httpx.New(httpx.Options{})}
	// The collector's probe gets its own client and a short ceiling: it is a
	// HEAD against a link somebody just pasted, so a host that accepts the
	// connection and then says nothing must not hold a staging pass open.
	a.Probe = httpx.New(httpx.Options{Timeout: probeTimeout})
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.Registry.Register(resolver.Direct{})
	a.Registry.Register(resolver.HTTPFallback{})

	eng, err := engine.New(filepath.Join(dataDir, "downloads"), a.onUpdate)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Engine = eng
	// The limiter is seeded here and owned by the schedule from Start onwards.
	// Between the two the timetable has not been consulted yet, and running
	// unthrottled in that window would be a speed limit that does not apply until
	// the first boundary.
	a.Throttle.Set(cfg.Get().SpeedLimit)

	s := cfg.Get()
	a.applyRuleSets(s)
	// At boot as well as on every save. Built only on save, a restart would leave
	// every configured connection unused until somebody happened to open the
	// settings page and press a button - and the symptom, downloads quietly
	// leaving by the machine's own address, looks nothing like its cause.
	a.applyConnections(s.Connections)
	a.dupes = dedupe.New(dedupe.ParsePolicy(s.MirrorPolicy))
	// The configuration is read through a closure rather than captured, so a
	// reconnect fired after the user edited the router password uses the password
	// they just saved and not the one this process booted with.
	rc, err := reconnect.New(reconnect.Options{
		Config: func() reconnect.Config { return a.Settings.Get().Reconnect },
		// Injected rather than left to the package's own fallback client, so
		// router traffic gets the shared policy too. The redirect rule is what
		// earns it: a LiveHeader script whose last step redirects off the router
		// must not carry the router password to wherever it points.
		HTTP: httpx.New(httpx.Options{}),
	})
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Reconnector = rc
	a.sched, err = schedule.NewRunner(schedule.Options{
		Entries: s.Schedule,
		Base:    a.scheduleBase,
		Apply:   a.applySchedule,
	})
	if err != nil {
		st.Close()
		return nil, err
	}

	// All engine traffic goes through a loopback proxy: it is the only place the
	// embedded download library lets us meter bytes. If it cannot start,
	// downloads still work — only the speed limit is lost.
	if px, err := netproxy.Start(a.Throttle); err != nil {
		log.Printf("speed limiter unavailable (%v); downloads run unthrottled", err)
	} else if err := eng.UseProxy(px.Addr()); err != nil {
		log.Printf("speed limiter not applied (%v); downloads run unthrottled", err)
		_ = px.Close()
	} else {
		a.proxy = px
	}

	acc, err := accounts.Open(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Accounts = acc

	guard, err := auth.Open(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Auth = guard

	a.rewireBackends()
	a.applyWatcher(cfg.Get().WatchDir)

	// Reload persisted tasks. What each one comes back as is reviveOnBoot's
	// decision, and it is not a formality: every row in the store belonged to a
	// process that is gone, so a task the database calls "running" has nothing
	// behind it at all.
	existing, err := st.All()
	if err != nil {
		return nil, err
	}
	// Asked before a single row is rewritten, because the first task moved out of
	// "running" destroys the evidence: this is how the app knows whether the last
	// process was downloading or sitting idle, and the resume policy turns on it.
	queueWasLive := false
	for _, t := range existing {
		if t.Status == core.StatusRunning {
			queueWasLive = true
			break
		}
	}
	resume := settings.ParseResumeOnStart(s.ResumeOnStart)
	var revived []core.Task
	var requeue []string
	for _, t := range existing {
		changed, enqueue := a.reviveOnBoot(t, resume, queueWasLive)
		if changed {
			revived = append(revived, *t)
		}
		if enqueue {
			requeue = append(requeue, t.ID)
		}
		a.tasks[t.ID] = t
		// Only live tasks are filed. A finished or failed download must not block
		// its own re-add: pasting one of those again is a deliberate second
		// attempt, which is the rule the raw URL comparison here used to enforce.
		if t.Status != core.StatusDone && t.Status != core.StatusError {
			a.dupes.Add(linkEntry(t))
		}
	}
	// Written back, not only fixed in memory. The store still says "running" for
	// a task nobody is running, and an unpacking that was interrupted has just
	// become a finished download - which has to reach the record and the
	// retention sweep as one. Nothing is broadcast: no client can be connected to
	// a server that has not been started yet.
	for i := range revived {
		c := revived[i]
		if err := st.Save(&c); err != nil {
			log.Printf("could not write back the boot state of %s: %v", c.ID, err)
		}
	}
	// Housekeeping runs once here as well as on its own timer, which is what
	// makes "the list is trimmed" true at the moment somebody opens it rather
	// than a minute later. It runs BEFORE the queue is filled and before the
	// scheduler has had its say: removing a task dispatches, and dispatching a
	// half-built queue against a timetable nothing has read yet is how a nightly
	// pause window gets ignored for the first minute of every boot.
	a.sweep()
	// Under the lock although nothing has been handed this App yet: the watcher
	// started above is already running, and a dropped job file reaches the queue
	// through it.
	a.mu.Lock()
	a.queue = append(a.queue, requeue...)
	a.mu.Unlock()
	// Started only now that the task list is whole. The runner's first pass halts
	// or throttles the queue immediately, and doing that to a queue still being
	// reconstructed would stop downloads nobody paused.
	//
	// It is also what starts whatever the resume policy just put back in the
	// queue, and deliberately so: the first pass dispatches only when the
	// timetable is not holding the queue, so downloads resumed by a restart
	// cannot walk past a pause window by being early.
	a.sched.Start()
	// Last, so nothing can sweep a list that is still being assembled. Close
	// waits for this goroutine, because everything it does writes to the store.
	a.wg.Add(1)
	go a.upkeep()
	return a, nil
}

// applyRuleSets compiles both rule lists and keeps what Compile could not use.
// Compile never fails and never returns nil, so a rule the user got wrong costs
// them that rule and nothing else. The problems are kept rather than logged
// because the settings form is the only place they can be acted on.
func (a *App) applyRuleSets(s settings.Settings) {
	pkg, pkgProb := rules.Compile(s.Packagizer)
	filt, filtProb := rules.Compile(s.LinkFilter)
	a.rmu.Lock()
	a.pkgRules, a.pkgProb = pkg, pkgProb
	a.filtRules, a.filtProb = filt, filtProb
	a.rmu.Unlock()
}

// matchers hands out the two compiled rule sets as they stand right now. They
// are replaced wholesale, so a caller that reads them once and uses them for a
// whole link is using one consistent rule list even if the user saves mid-paste.
func (a *App) matchers() (packagizer, filter *rules.Matcher) {
	a.rmu.RLock()
	defer a.rmu.RUnlock()
	return a.pkgRules, a.filtRules
}

// RuleProblems is what Compile had to leave out of each rule list.
type RuleProblems struct {
	Packagizer []rules.Problem `json:"packagizer"`
	LinkFilter []rules.Problem `json:"linkFilter"`
}

// RuleProblems reports the rules that could not be compiled. It belongs in the
// settings response: a rule dropped for a broken regular expression that nothing
// tells the user about is a rule they go on believing in, and for a filter that
// means links they think are being blocked and are not.
func (a *App) RuleProblems() RuleProblems {
	a.rmu.RLock()
	defer a.rmu.RUnlock()
	return RuleProblems{Packagizer: problemList(a.pkgProb), LinkFilter: problemList(a.filtProb)}
}

// problemList is never nil, so a client reads an empty list rather than null.
func problemList(in []rules.Problem) []rules.Problem {
	if in == nil {
		return []rules.Problem{}
	}
	return in
}

// taskDir answers "where does this task download to" for backends that spawn a
// process per task and need the folder at spawn time.
func (a *App) taskDir(taskID string) string {
	a.mu.Lock()
	var c *core.Task
	if t := a.tasks[taskID]; t != nil {
		x := *t
		c = &x
	}
	a.mu.Unlock()
	return a.dirFor(c)
}

// Close shuts the app down, and what that costs is worth stating plainly rather
// than leaving somebody to find out during an incident.
//
// IT WAITS FOR three things, in this order and for one reason each: the
// housekeeping loop, because it removes tasks and writes to the store; the
// intake watcher, because a job file half read is a paste that half happened;
// and the schedule runner, whose own Close blocks on an in-flight Apply - which
// is the promise that makes it safe to tear down everything Apply talks to next.
//
// IT ABANDONS every transfer still running, and there is no drain, no grace
// period and no attempt at one. A shutdown that waits for a 40 GB download is a
// container that never restarts, and stopping does not destroy the bytes already
// written - only starting again from the beginning does. What it does cost is
// the last update from each of those transfers: a backend reporting in after the
// store is closed has its write discarded, silently, because every caller in the
// app discards Save's error. That is not tidy, and it is precisely why boot
// reconciles the list instead of trusting the last state written to it.
//
// The order below is the contract. Cancel first so nothing new begins, then the
// goroutines this package owns, then the subsystems, and the store last because
// every one of them writes to it.
// spawn runs f on its own goroutine and makes Close wait for it.
//
// The long-lived upkeep loop was counted on a.wg from the start; the short-lived
// ones were not, and several of them write to the store - the availability
// probe, the checksum pass, a watch-folder job, the settled-task publish. So
// Close could cancel, wait for upkeep, close the store, and then one of those
// would land: in production a write to a closed database with its error
// discarded, and on CI a test failing with "TempDir RemoveAll cleanup:
// directory not empty", because SQLite recreated its write-ahead log inside the
// directory the harness was in the middle of deleting.
//
// A goroutine started after the context is already done does not start at all.
// There is no useful work left for it: everything it would write goes to a store
// that is closing, and the alternative - letting it run and discarding the error
// - is how a shutdown grows a tail nobody can measure.
func (a *App) spawn(f func()) {
	if a.ctx != nil && a.ctx.Err() != nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		f()
	}()
}

func (a *App) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	// Before the engine and before the store, because a sweep in flight is
	// removing tasks from both.
	a.wg.Wait()
	a.wmu.Lock()
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	a.wmu.Unlock()
	// Closed before the engine, because Close waits for an in-flight Apply to
	// return: that is exactly the promise that lets everything Apply talks to be
	// torn down next.
	if a.sched != nil {
		_ = a.sched.Close()
	}
	if a.proxy != nil {
		_ = a.proxy.Close()
	}
	if a.Engine != nil {
		a.Engine.Close()
	}
	return a.Store.Close()
}

// dirFor is the single answer to "where does this task's file go": the task's
// own folder if set, else the configured download folder (or the built-in
// default), optionally with a per-package subfolder. A task that already names
// its own folder is taken at its word — combining both would nest duplicates.
func (a *App) dirFor(t *core.Task) string {
	if t == nil {
		return a.defaultDir()
	}
	if t.Dir != "" {
		return t.Dir
	}
	cfg := a.Settings.Get()
	// A configured folder may be a template. Expanding it here means the
	// variables see the task they are being expanded for, which is the only
	// point at which the package and hoster are known.
	if pathvars.HasVars(cfg.DownloadDir) {
		expanded := pathvars.Expand(cfg.DownloadDir, pathvars.Vars{
			Package: t.Package,
			Host:    hostOf(t.URL),
			Name:    t.Name,
			Date:    t.CreatedAt,
		})
		if filepath.IsAbs(expanded) {
			return expanded
		}
	}
	dir := a.defaultDir()
	if cfg.SubfolderByPackage && strings.TrimSpace(t.Package) != "" {
		dir = filepath.Join(dir, sanitizeSegment(t.Package))
	}
	return dir
}

// defaultDir is the configured download folder, or the one inside the data
// directory when none is set.
func (a *App) defaultDir() string {
	if d := strings.TrimSpace(a.Settings.Get().DownloadDir); d != "" {
		return d
	}
	return a.dlDir
}

// sanitizeSegment turns a package name into something safe to use as one path
// segment on any platform.
func sanitizeSegment(s string) string {
	// Anything that cannot appear in one path segment on some platform becomes
	// a dash; control characters become spaces.
	const bad = `/\:*?"<>|`
	out := strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		if strings.ContainsRune(bad, r) {
			return '-'
		}
		return r
	}, s)
	out = strings.Trim(strings.TrimSpace(out), ". ")
	if out == "" {
		return "package"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// hostOf returns the scheduling host bucket for a URL.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// speedLimiter is implemented by backends that can apply a live rate limit.
type speedLimiter interface {
	SetSpeedLimit(bytesPerSec int64) error
}

// ApplySettings persists new settings and applies what can change at runtime:
// raised limits dispatch waiting tasks immediately, the JD limit is pushed
// live, and yt-dlp picks the limit up on its next spawn. The embedded engine
// has no rate-limit API yet (Gopeed v1.9.x) — engine tasks run unthrottled.
func (a *App) ApplySettings(s settings.Settings) (settings.Settings, error) {
	applied, err := a.Settings.Set(s)
	if err != nil {
		return applied, err
	}
	a.applyRuleSets(applied)
	// The speed limit goes through the timetable and never straight to the
	// limiter. Writing it here as well would let a saved settings page lift a
	// nightly cap that is still in force, until whichever boundary came next.
	// Set recompiles and re-evaluates at once against the new base, so the runner
	// is the one that hands the limiter its answer.
	a.sched.Set(applied.Schedule)
	a.applyWatcher(applied.WatchDir)
	a.applyConnections(applied.Connections)
	a.mu.Lock()
	if p := dedupe.ParsePolicy(applied.MirrorPolicy); p != a.dupes.Policy() {
		// The policy is baked in at New, so a change needs a new set — re-seeded
		// from the list it is meant to describe, or the first paste after the
		// change would be checked against nothing.
		a.dupes = dedupe.New(p)
		for _, t := range a.tasks {
			if t.Status != core.StatusDone && t.Status != core.StatusError {
				a.dupes.Add(linkEntry(t))
			}
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	return applied, nil
}

// pushJDSpeedLimit hands the limit in force to the JD backend, which meters its
// own downloads because they never touch our loopback proxy.
func (a *App) pushJDSpeedLimit(limit int64) {
	a.bmu.RLock()
	jdBackend := a.jd
	a.bmu.RUnlock()
	if sl, ok := jdBackend.(speedLimiter); ok {
		if err := sl.SetSpeedLimit(limit); err != nil {
			log.Printf("JD speed limit not applied: %v", err)
		}
	}
}

// applyConnections rebuilds the connection picker from the saved rows.
//
// Rebuilt rather than mutated, because building a picker is also what settles
// the ban list against the new list: a row switched back on loses its refusals
// there, and there is no second call anyone has to remember. The Bans instance
// is kept across rebuilds so the refusals themselves survive a save that had
// nothing to do with them.
//
// An empty list leaves the picker nil, which every caller reads as "leave by
// this machine's own address" - the same answer as a list with nothing usable in
// it, and the right one for the ordinary install that has configured no proxy.
func (a *App) applyConnections(rows []proxycfg.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(rows) == 0 {
		a.picker = nil
		return
	}
	if a.bans == nil {
		a.bans = proxycfg.NewBans()
	}
	a.picker = proxycfg.NewPicker(rows, proxycfg.Options{Bans: a.bans})
}

// applyWatcher starts, restarts or stops the intake watcher to match the
// configured folder. It runs on every settings change, so turning the folder on
// does not need a restart.
func (a *App) applyWatcher(dir string) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	if dir == "" {
		return
	}
	w, err := watch.New(watch.Options{Dir: dir, OnJob: a.onWatchJob})
	if err != nil {
		log.Printf("watch folder %s unusable (%v); intake is off", dir, err)
		return
	}
	w.Start()
	a.watcher = w
	log.Printf("watching %s for dropped links", dir)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// freshID returns an ID no live task holds. A collision is astronomically
// unlikely, but its consequence is not a glitch: the new task would silently
// replace an existing one in the map, and the old download would be orphaned
// with no way to reach it. Caller holds a.mu.
func (a *App) freshIDLocked() string {
	for {
		id := newID()
		if _, taken := a.tasks[id]; !taken {
			return id
		}
	}
}
