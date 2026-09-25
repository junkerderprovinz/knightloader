// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; a download backend (the Gopeed engine or
// headless JD) reports changes, the app persists them and broadcasts them.
//
// The package is split by subject: this file holds the App and its lifecycle,
// app_links.go the way a link becomes a task, app_queue.go the wait queue,
// app_dispatch.go the handover to a backend and what it reports back,
// app_tasks.go per-task edits and persistence, app_extract.go unpacking,
// app_bulk.go operations on a selection, app_boot.go restart recovery and
// housekeeping, app_mirror.go second copies of a file, and app_accounts.go the
// credentials and the backend routing they decide.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/script"
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

// probeTimeout bounds one collector HEAD. The user is waiting at the paste box,
// and a host that accepts the connection and then stalls must not decide how
// long staging takes.
const probeTimeout = 10 * time.Second

// ytdlpProbeTimeout bounds one yt-dlp title probe (probeYtdlpTitle). It is
// longer than probeTimeout because yt-dlp starts a process and often fetches
// and parses the whole page before it can answer. The figure is an estimate,
// not a measurement.
const ytdlpProbeTimeout = 20 * time.Second

// doer is the part of an HTTP client the probe uses, so a test can answer
// without leaving the machine.
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
	// APITokens are named, individually revocable credentials that satisfy
	// the same session guard a password does (see api.authenticated) without
	// sharing its secret.
	APITokens *apitoken.Store
	// Scripts runs the user's automation snippets in a goja sandbox (see
	// internal/script). It subscribes to Events like any other consumer.
	Scripts *script.Host
	// Events is the app's event bus; app_script.go is where the app publishes.
	Events *script.Bus
	// EventTargets turns a firing into an HTTP request to a configured
	// address. It owns one goroutine per enabled target and must be closed
	// (see app_notify.go).
	EventTargets *notify.Dispatcher
	// Throttle is the shared bandwidth allowance for everything downloading
	// through the loopback proxy.
	Throttle *throttle.Limiter
	// Crawler turns a pasted page into the files it links to.
	Crawler crawler.Crawler
	// Reconnector asks the router for a new public address, which is the only
	// thing that lifts a hoster limit keyed to the one this box has.
	Reconnector *reconnect.Reconnector

	// MediaHooks calls the address a category points at once a package filed
	// there has finished and its files are in place (see app_mediahook.go).
	MediaHooks *mediahook.Runner

	// Probe is the client the collector's HEAD requests use to learn a staged
	// link's size and whether it still exists. It is a field so tests that
	// stage links do not race a real DNS lookup.
	Probe doer

	// DataDir is the directory New was given. Backup and restore need the
	// directory itself, since internal/backup stages a restore beside it.
	DataDir string

	// RequestExit is how the quit, restart and restore routes ask whatever
	// embeds this App to stop the process; App owns no server or signal loop.
	// Nil means not supported here, which is the case in tests and on the
	// desktop build, whose tray has its own path to Close.
	//
	// restart only changes the log line and the API's answer; the shutdown is
	// the same, since quit and restart cannot be told apart outside a
	// supervised deployment (see cmd/knightloader/main.go). It returns false
	// when a shutdown is already under way.
	RequestExit func(restart bool) bool

	// RequestUpdateInstall, set on the desktop build only, downloads and
	// applies a newer release, starts it, and exits through the tray's
	// graceful path. A container updates through its deployment instead (see
	// internal/update). Nil means not supported here.
	RequestUpdateInstall func(ctx context.Context) error

	// RequestSuspend, set on the desktop build only, puts the machine to sleep
	// for the end-of-queue "suspend" action. The per-OS calls live in
	// desktop/power_*.go.
	//
	// It is separate from RequestExit because sleeping is not quitting, and a
	// container that can exit cannot sleep its host. Which actions are offered
	// follows from which of these fields are wired
	// (internal/idleaction.Capabilities).
	//
	// The error carries the operating system's words verbatim: on Linux a
	// policy refusal reads "Interactive authentication required", which says
	// what to fix.
	RequestSuspend func() error

	// CnL is the Click'n'Load listener the embedding started, nil where it
	// started none. The server and the desktop build both set it. Its switch is
	// not persisted: KL_CNL is the deployment's decision, and switching the
	// listener off is an in-process pause on top of it.
	CnL *cnl.Listener

	// ctx is cancelled by Close. It bounds work that outlives its caller: a
	// reconnect can hold the line for its whole timeout, and a shutdown must
	// neither wait for a router nor fire a reboot command on its way out.
	ctx    context.Context
	cancel context.CancelFunc

	// sched applies the user's timetable to the queue. It owns one goroutine and
	// is the only writer of the speed limit, so a saved settings page cannot lift
	// a nightly cap that is still in force.
	sched *schedule.Runner

	// idleAction carries out the configured end-of-queue action after its
	// cancellable countdown (see app_idle.go). It owns one goroutine.
	idleAction *idleaction.Controller

	// idleRuns records what the last end-of-queue action did and holds the
	// runner the command action uses (see app_idle_command.go). Its zero value
	// is ready to use. It has its own lock rather than a.mu because it is
	// taken inside a spawned goroutine.
	idleRuns idleRunLog

	// wg counts the long-lived goroutines this package starts. Close waits on
	// it because they write to the store, which closes on the way out.
	// Download goroutines are not counted; boot repairs what they leave.
	wg sync.WaitGroup

	// closeMu guards closing only. mu is held by callers that reach spawn on
	// the way out (dispatchLocked, unpackLocked), so registering work under mu
	// would deadlock. closing makes "still accepting work?" and "count me in"
	// one step; see track.
	closeMu sync.Mutex
	closing bool

	jd    backend // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp backend // yt-dlp media backend, nil unless the yt-dlp binary is present
	// torbox is the default TorBox account's backend, nil unless configured.
	// Every TorBox account is also in debrid under its slot id, which
	// backendFor reads first, so this only answers for a task recorded as the
	// bare "torbox".
	torbox backend
	// debrid holds one backend per configured debrid account, keyed by its
	// resolver slot id: "alldebrid" for the default account, "alldebrid#work"
	// for a second login (see resolver.SlotID).
	debrid map[string]backend
	// remotefs fetches ftp, ftps and sftp links and hands WebDAV ones to the
	// engine. It is never nil after rewireBackends, since an anonymous FTP
	// archive needs no credential or binary.
	remotefs backend

	dlDir string           // where engine + yt-dlp downloads land (extraction source)
	proxy *netproxy.Server // loopback proxy the engine downloads through

	// wmu guards watcher, which is replaced whenever the watched folder changes.
	wmu     sync.Mutex
	watcher *watch.Watcher

	// fmu guards feeds, the RSS/Atom subscriptions. It is separate from wmu
	// because applyWatchFolders probes shares and applyFeeds reads the store,
	// and neither should wait on the other.
	fmu   sync.Mutex
	feeds *feed.Runner

	// smu guards selfServe, this instance's fully wired HTTP handler. It is set
	// by api.Handler as its last step, so earlier readers see "not ready", and
	// the relay's inbound proxy (routes_relay.go) uses it to answer a sibling
	// exactly as this instance answers its own UI.
	smu       sync.RWMutex
	selfServe http.Handler
	// discovery is the multicast announce/listen service, nil unless a main
	// package enabled it (buildinfo.DiscoveryEnabled).
	discovery io.Closer

	// bmu guards the backend fields above. It is separate from mu because
	// re-wiring makes network calls, and task state must not wait for those.
	bmu sync.RWMutex

	// claims keeps the direct download, the HTTP fallback and yt-dlp off the
	// hosts they would fetch a page from (see app_claims.go).
	claims hostClaims

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
	// budget is the speed limit shared out between the backends (see
	// app_budget.go).
	budget budget
	// limitInForce is the limit the timetable last put in force; a window can
	// carry its own. Negative means no window has spoken and settings decide,
	// since zero means unlimited.
	limitInForce int64
	// scheduleBaseHalt is the manualHalt scheduleBase last handed the schedule
	// runner, so applySchedule can tell an answer computed before a hard stop
	// from one computed after it.
	scheduleBaseHalt bool
	// quiet is the second set of limits and its switch (see app_quiet.go).
	quiet quietState
	// dupes answers "is this link already in the list". It is not safe for
	// concurrent use, so every call to it happens under mu.
	dupes *dedupe.Set
	// picker chooses which configured connection carries a download. It is
	// rebuilt on every settings save, which also settles the bans against the
	// new rows (see proxycfg.NewPicker). Nil means this machine's own address.
	// Read under mu.
	picker *proxycfg.Picker
	// bans outlives every picker, so a connection refused by a host does not get
	// a clean slate every time the user saves an unrelated setting.
	bans *proxycfg.Bans
	// skipped is the trace of links that never became tasks, newest last.
	skipped []SkippedLink
	// stopMark is the task whose completion halts the queue: "finish this,
	// then stop".
	stopMark string
	tasks    map[string]*core.Task
	queue    []string        // task IDs waiting for a slot, FIFO with per-host skip-ahead
	active   map[string]bool // dispatched and not yet terminal/paused
	started  map[string]bool // ever handed to a backend (Resume vs fresh Download)
	// stamps hands out the CreatedAt of every link that enters the list. It
	// has its own lock.
	stamps stagedAt
	// unpack is the extraction worker: the jobs, their order and the goroutine
	// that runs them. It is built on first use (see unpackLocked).
	unpack *unpackState
	// probed is the last yt-dlp format list per link URL, so a quality picked
	// after the probe gets its own extension and size without asking the host
	// again. Read and written under mu, built on first use.
	probed map[string][]ytdlp.FormatEntry
	// reprobing holds the links reprobeYtdlp is asking about, under mu.
	reprobing map[string]bool
	// iconCache is the hoster-icon cache (app_hostericons.go), embedded so its
	// fields stay in that file. It is built on first use.
	iconCache
	// mediaToolsState describes yt-dlp and ffmpeg on this machine
	// (app_mediatools.go). It is built on first use.
	mediaToolsState
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
		DataDir:    dataDir,
		dlDir:      filepath.Join(dataDir, "downloads"),
		Throttle:   throttle.New(),
		tasks:      map[string]*core.Task{},
		active:     map[string]bool{},
		started:    map[string]bool{},
		debrid:     map[string]backend{},
	}
	// Every outbound client comes from internal/httpx, so proxy, user agent,
	// redirect rule and pooling are one policy. Each subsystem gets its own
	// client so a router holding connections open cannot starve a crawl.
	a.Crawler = crawler.HTML{Client: httpx.New(httpx.Options{})}
	a.Probe = httpx.New(httpx.Options{Timeout: probeTimeout})
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.Registry.Register(resolver.Direct{Leave: a.claims.pageOnly})
	a.Registry.Register(resolver.HTTPFallback{Leave: a.claims.pageOnly})
	// Torrents need no account, so unlike the resolvers in app_accounts.go this
	// one is registered unconditionally.
	a.Registry.Register(torrent.Resolver{})

	eng, err := engine.New(filepath.Join(dataDir, "downloads"), a.onUpdate)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Engine = eng
	// Seeded here so the configured limit applies before the schedule's first
	// pass takes over.
	a.limitInForce = -1
	a.Throttle.Set(cfg.Get().SpeedLimit)

	s := cfg.Get()
	a.applyRuleSets(s)
	// At boot as well as on every save, or a restart would send downloads out
	// by the machine's own address until the next save.
	a.applyConnections(s)
	a.applyTorrentConfig(s.Torrent)
	a.dupes = dedupe.New(dedupe.ParsePolicy(s.MirrorPolicy))
	// Read through a closure so a reconnect uses the router password as last
	// saved.
	rc, err := reconnect.New(reconnect.Options{
		Config: func() reconnect.Config { return a.Settings.Get().Reconnect },
		// The shared redirect rule keeps a LiveHeader script that redirects off
		// the router from carrying the router password along.
		HTTP: httpx.New(httpx.Options{}),
	})
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Reconnector = rc
	a.sched, err = schedule.NewRunner(schedule.Options{
		Entries:    s.Schedule,
		Base:       a.scheduleBase,
		Apply:      a.applySchedule,
		Suspension: a.storedSuspension(time.Now()),
	})
	if err != nil {
		st.Close()
		return nil, err
	}
	a.idleAction, err = idleaction.NewController(idleaction.Options{
		Config:   func() idleaction.Config { return a.Settings.Get().IdleAction },
		Idle:     a.queueIdleForAction,
		Fire:     a.fireIdleAction,
		OnChange: func() { a.Hub.Broadcast("idleAction", a.IdleActionState()) },
		// Zero and nil outside tests, which keeps idleaction's defaults (see
		// idleActionPoll).
		Poll:  idleActionPoll,
		Clock: idleActionClock,
	})
	if err != nil {
		st.Close()
		return nil, err
	}

	// All engine traffic goes through a loopback proxy, the only place the
	// download library lets bytes be metered. Without it downloads still work,
	// just unthrottled.
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

	tokens, err := apitoken.Open(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.APITokens = tokens

	// The bus belongs to the app rather than the script host, so other
	// subscribers do not depend on the host and closing it leaves the bus
	// alone.
	a.Events = script.NewBus()
	// The instance name is read at delivery time, since it can be renamed while
	// running.
	a.EventTargets = notify.New(notify.Options{InstanceName: func() string { return cfg.Get().InstanceName }})
	a.Events.Subscribe("eventtargets", a.EventTargets.On)
	scripts, err := script.NewHost(script.Options{DataDir: dataDir, Actions: scriptActions{a}, Hub: a.Hub, Bus: a.Events})
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Scripts = scripts
	a.applyModuleSwitches(cfg.Get())

	// After the bus and the credential store, both of which it needs.
	a.startMediaHooks()

	a.rewireBackends()
	a.applyWatchFolders(cfg.Get())
	// At boot as well as on save, so an enabled log file is written from the
	// start. logring.OpenFile replays the lines logged before this point.
	a.applyLogFile(cfg.Get().LogFile)

	// Every stored row belonged to a process that is gone, so reviveOnBoot
	// decides what each task comes back as.
	existing, err := st.All()
	if err != nil {
		return nil, err
	}
	// Checked before any row is rewritten: whether the last process was
	// downloading decides the resume policy.
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
		// Only live tasks are filed: pasting a finished or failed download again
		// is a second attempt, not a duplicate.
		if t.Status != core.StatusDone && t.Status != core.StatusError {
			a.dupes.Add(linkEntry(t))
		}
	}
	// Written back so the store and the retention sweep agree. Nothing is
	// broadcast; no client can be connected yet.
	for i := range revived {
		c := revived[i]
		if err := st.Save(&c); err != nil {
			log.Printf("could not write back the boot state of %s: %v", c.ID, err)
		}
	}
	// Housekeeping runs once now so the list is trimmed when first opened. It
	// runs before the queue is filled and the scheduler starts, since removing
	// a task dispatches and could ignore a pause window.
	a.sweep()
	// Under the lock because the watcher started above is already running.
	a.mu.Lock()
	a.queue = append(a.queue, requeue...)
	// The queue comes up stopped when the resume policy says nothing should
	// start by itself. manualHalt is set too, since the schedule's first pass
	// recomputes halted from it.
	if len(requeue) > 0 && holdOnBoot(resume, queueWasLive) {
		a.halted = true
		a.manualHalt = true
	}
	a.mu.Unlock()
	// Everything below starts only now that the task list is whole.
	//
	// The schedule's first pass halts or throttles at once, and it is also what
	// dispatches the requeued tasks, so a restart cannot slip past a pause
	// window.
	a.sched.Start()
	// idleAction would otherwise see an empty queue and arm its countdown.
	a.idleAction.Start()
	// Feeds come up after a.dupes is seeded, unlike drop folders: a poller
	// hands links over the moment it starts, and an unseeded set would let a
	// listed link in twice.
	a.applyFeeds(cfg.Get())
	// Targets may fire on queue.idle, which is reported within two seconds.
	a.applyEventTargets(cfg.Get())
	// upkeep and budgetLoop call a.wg.Done themselves, so they use track and a
	// bare go rather than a.spawn. Nothing can race Close here, but every
	// a.wg.Add goes through track.
	if a.track() {
		go a.upkeep()
	}
	if a.track() {
		go a.budgetLoop()
	}
	a.spawn(a.watchQueueIdleForScripts)
	// Its first pass records which packages are already complete, so it must
	// not see a half-loaded list.
	a.spawn(a.watchPackagesForScripts)
	// The speed record starts at boot so the first visitor already sees a
	// curve (see app_speedhistory.go).
	a.spawn(a.sampleSpeedLoop)
	// Rows staged under a preset that differs from the saved one are sorted
	// before anybody opens the collector.
	a.applyVariantPresets()
	// Last, with the list whole and the presets applied, so a batch that fell
	// due while the app was down confirms what the collector shows after the
	// restart.
	a.rearmAutoConfirm()
	// Spawned so the boot does not wait on yt-dlp calls.
	a.spawn(a.backfillYtdlpProbes)
	return a, nil
}

// applyRuleSets compiles both rule lists and keeps what Compile could not use.
// A broken rule costs only that rule, and its problem is kept for the settings
// form, where it can be fixed.
func (a *App) applyRuleSets(s settings.Settings) {
	pkg, pkgProb := rules.Compile(s.Packagizer)
	filt, filtProb := rules.Compile(s.LinkFilter)
	a.rmu.Lock()
	a.pkgRules, a.pkgProb = pkg, pkgProb
	a.filtRules, a.filtProb = filt, filtProb
	a.rmu.Unlock()
}

// matchers returns the two compiled rule sets. They are replaced wholesale, so
// a caller that reads them once has a consistent pair even if a save lands
// mid-paste.
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

// RuleProblems reports the rules that could not be compiled, so the settings
// response can show a filter rule that is silently not blocking anything.
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

// TaskFolder is taskDir for callers outside this package, so nobody has to
// reproduce the folder rules. An empty id answers the default folder, which
// the SABnzbd bridge reports as complete_dir
// (internal/api/routes_downloadclient.go).
func (a *App) TaskFolder(id string) string { return a.taskDir(id) }

// spawn runs f on its own goroutine and makes Close wait for it, since many of
// these goroutines write to the store Close is about to shut. After Close has
// begun, f does not start at all (see track).
func (a *App) spawn(f func()) {
	if !a.track() {
		return
	}
	go func() {
		defer a.wg.Done()
		f()
	}()
}

// track counts the caller in as work Close has to wait for, or reports false
// once Close has begun, in which case the caller must not touch a.wg. Every
// a.wg.Add in this package goes through here; the caller keeps the Done.
//
// The check and the Add happen under one lock. A separate check would leave a
// gap in which Close reaches Wait at zero before the Add runs, which
// sync.WaitGroup calls misuse: Close returns with work still running, or the
// process panics.
//
// Nothing is called while closeMu is held, since spawn's callers hold locks of
// their own.
func (a *App) track() bool {
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	if a.closing {
		return false
	}
	a.wg.Add(1)
	return true
}

// SetSelfServeHandler stores this instance's fully wired HTTP handler, so the
// relay's inbound proxy (routes_relay.go) answers a sibling through the same
// auth guard and routes as a browser. api.Handler calls it as its last step;
// until then SelfServeHandler returns nil, read as "not ready".
func (a *App) SetSelfServeHandler(h http.Handler) {
	a.smu.Lock()
	a.selfServe = h
	a.smu.Unlock()
}

// SetDiscovery stores the network-discovery service so Close can stop it; a
// shutting-down instance must stop announcing itself.
func (a *App) SetDiscovery(c io.Closer) {
	a.smu.Lock()
	a.discovery = c
	a.smu.Unlock()
}

// SelfServeHandler returns whatever SetSelfServeHandler last stored, or nil
// before that has ever run.
func (a *App) SelfServeHandler() http.Handler {
	a.smu.RLock()
	defer a.smu.RUnlock()
	return a.selfServe
}

// Close shuts the app down. It waits for the goroutines this package owns, the
// intake watcher, the feed runner and the schedule runner, whose Close blocks
// on an in-flight Apply.
//
// Running transfers are abandoned without a drain: waiting on a large download
// would keep a container from restarting, and stopping keeps the bytes already
// written. A backend reporting after the store is closed has its write
// discarded, which is why boot reconciles the list.
//
// The order is the contract: refuse new work and cancel, wait for owned
// goroutines, close the subsystems, and the store last since they all write to
// it. Calling it twice is harmless.
func (a *App) Close() error {
	// Flipped under track's lock so no spawn can Add past the Wait below.
	// The flag refuses new work; cancel stops work under way.
	a.closeMu.Lock()
	a.closing = true
	a.closeMu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	// Leave the relay and stop announcing on the network.
	if a.Federation != nil {
		a.Federation.SetRelay(nil)
	}
	a.smu.Lock()
	disc := a.discovery
	a.discovery = nil
	a.smu.Unlock()
	if disc != nil {
		_ = disc.Close()
	}
	// Before the engine and the store, since a sweep in flight removes tasks
	// from both.
	a.wg.Wait()
	a.wmu.Lock()
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	a.wmu.Unlock()
	// Waits for a poll in flight so no entry is still being added. cancel has
	// already aborted any fetch, so this is bounded by the handover.
	a.fmu.Lock()
	if a.feeds != nil {
		_ = a.feeds.Close()
		a.feeds = nil
	}
	a.fmu.Unlock()
	// Each of the following Close calls waits for work in flight (an Apply, a
	// tick, a script, a request) before what it calls into is torn down.
	if a.sched != nil {
		_ = a.sched.Close()
	}
	if a.idleAction != nil {
		_ = a.idleAction.Close()
	}
	if a.Scripts != nil {
		_ = a.Scripts.Close()
	}
	if a.EventTargets != nil {
		_ = a.EventTargets.Close()
	}
	// Pending media hook calls are dropped rather than flushed: their files
	// were never moved into place.
	a.stopMediaHooks()
	if a.proxy != nil {
		_ = a.proxy.Close()
	}
	if a.Engine != nil {
		a.Engine.Close()
	}
	return a.Store.Close()
}

// dirFor decides where a task's file goes: the task's own folder if set, else
// its category's folder, else the configured download folder (or the built-in
// default), optionally with a per-package subfolder. A task's own folder is
// used as is; appending to it would nest duplicates.
func (a *App) dirFor(t *core.Task) string {
	if t == nil {
		return a.defaultDir()
	}
	if t.Dir != "" {
		return t.Dir
	}
	cfg := a.Settings.Get()
	vars := pathvars.Vars{
		Package: t.Package,
		Host:    hostOf(t.URL),
		Name:    t.Name,
		Date:    t.CreatedAt,
	}
	// The category sits between the task's folder and the instance's. A
	// Packagizer rule writes its folder into t.Dir, so a rule, which looked at
	// this link, beats a category, which labels a batch.
	//
	// A templated category folder gets no per-package level appended, like a
	// templated DownloadDir below.
	if raw := cfg.CategoryFor(t.Category).Dir; raw != "" {
		if d := cfg.CategoryDir(t.Category, vars); d != "" {
			if pathvars.HasVars(raw) {
				return d
			}
			return a.withPackageSubfolder(cfg, t, d)
		}
	}
	// A configured folder may be a template, expanded here where the task's
	// package and hoster are known.
	if pathvars.HasVars(cfg.DownloadDir) {
		if expanded := pathvars.Expand(cfg.DownloadDir, vars); filepath.IsAbs(expanded) {
			return expanded
		}
	}
	return a.withPackageSubfolder(cfg, t, a.defaultDir())
}

// withPackageSubfolder appends the per-package level when the setting asks for
// one, for both the category folder and the instance folder.
func (a *App) withPackageSubfolder(cfg settings.Settings, t *core.Task, dir string) string {
	if cfg.SubfolderByPackage && strings.TrimSpace(t.Package) != "" {
		return filepath.Join(dir, sanitizeSegment(t.Package))
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

// titleProber is implemented by a backend that can look up a link's name and
// available formats without downloading it, the yt-dlp counterpart to the
// collector's HEAD probe (analyze). It is optional because other backends learn
// the name from their progress stream once a download starts.
type titleProber interface {
	ProbeTitle(ctx context.Context, url string) (ytdlp.ProbeResult, error)
}

// ApplySettings persists new settings and applies what can change at runtime:
// raised limits dispatch waiting tasks immediately, the JD limit is pushed
// live, and yt-dlp picks the limit up on its next spawn. The embedded engine
// has no rate-limit API (Gopeed v1.9.x), so it is metered through the
// loopback proxy instead.
func (a *App) ApplySettings(s settings.Settings) (settings.Settings, error) {
	applied, err := a.Settings.Set(s)
	if err != nil {
		return applied, err
	}
	a.afterSettingsChange(applied)
	return applied, nil
}

// PatchSettings is ApplySettings for a partial update: patch names only the
// top-level fields to change, and settings.Store.SetPartial merges them under
// the lock that writes the result. Everything after the save runs as for a
// full update.
func (a *App) PatchSettings(patch map[string]json.RawMessage) (settings.Settings, error) {
	applied, err := a.Settings.SetPartial(patch)
	if err != nil {
		return applied, err
	}
	a.afterSettingsChange(applied)
	return applied, nil
}

// afterSettingsChange applies every runtime effect of a saved settings
// document. ApplySettings and PatchSettings share it so they cannot drift
// apart.
func (a *App) afterSettingsChange(applied settings.Settings) {
	a.applyRuleSets(applied)
	// The speed limit goes through the timetable, never straight to the
	// limiter, so a save cannot lift a nightly cap still in force. Set
	// re-evaluates at once.
	a.sched.Set(applied.Schedule)
	// Now rather than at the next poll, so toggling the action on an idle
	// queue takes effect at once.
	a.idleAction.Refresh()
	a.applyWatchFolders(applied)
	a.applyFeeds(applied)
	a.applyEventTargets(applied)
	a.applyLogFile(applied.LogFile)
	a.applyConnections(applied)
	a.applyTorrentConfig(applied.Torrent)
	a.applyModuleSwitches(applied)
	a.mu.Lock()
	if p := dedupe.ParsePolicy(applied.MirrorPolicy); p != a.dupes.Policy() {
		// The policy is fixed at construction, so a change needs a new set,
		// re-seeded from the list.
		a.dupes = dedupe.New(p)
		for _, t := range a.tasks {
			if t.Status != core.StatusDone && t.Status != core.StatusError {
				a.dupes.Add(linkEntry(t))
			}
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	// Both preset editors save through here, so the collector follows either
	// of them at once.
	a.applyVariantPresets()
	// An auto-confirm countdown under way follows a new delay at once, and
	// ends when auto-confirm was switched off. After the presets, so a
	// countdown the save makes due leaves out the rows it set aside.
	a.wakeAutoConfirm()
	// Other open tabs reload their settings pages and the modules list.
	a.Hub.Broadcast("settings", nil)
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

// ytdlpTitleProber returns the yt-dlp backend as a titleProber, and false when
// yt-dlp is switched off, no yt-dlp backend is wired or it does not implement
// one.
func (a *App) ytdlpTitleProber() (titleProber, bool) {
	if a.resolverOff("ytdlp") {
		return nil, false
	}
	a.bmu.RLock()
	b := a.ytdlp
	a.bmu.RUnlock()
	tp, ok := b.(titleProber)
	return tp, ok
}

// applyConnections rebuilds the connection picker from the saved rows. Building
// it also settles the bans against the new rows; the Bans instance itself is
// kept so refusals survive unrelated saves. An empty list, or the module
// switched off, leaves the picker nil: use this machine's own address.
func (a *App) applyConnections(s settings.Settings) {
	rows := s.Connections
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(rows) == 0 || s.ModuleOff("connections") {
		a.picker = nil
		return
	}
	if a.bans == nil {
		a.bans = proxycfg.NewBans()
	}
	a.picker = proxycfg.NewPicker(rows, proxycfg.Options{Bans: a.bans})
}

// applyTorrentConfig pushes the seed ratio, seed duration and port into the
// engine. The seed settings apply to torrents added from now on; the port only
// if no torrent has started in this process (see Engine.SetTorrentConfig). A
// failure is logged rather than failing the save or the boot.
func (a *App) applyTorrentConfig(t settings.Torrent) {
	if err := a.Engine.SetTorrentConfig(t.Port, t.SeedRatioTarget, t.SeedDurationSeconds); err != nil {
		log.Printf("torrent config not applied (%v); torrents seed at the engine's own defaults", err)
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// freshIDLocked returns an ID no live task holds. A collision is very unlikely,
// but it would silently replace a task in the map and orphan its download.
// Caller holds a.mu.
func (a *App) freshIDLocked() string {
	for {
		id := newID()
		if _, taken := a.tasks[id]; !taken {
			return id
		}
	}
}
