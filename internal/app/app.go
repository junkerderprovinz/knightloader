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
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
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

// probeTimeout bounds one collector HEAD. It is short on purpose: the user is
// waiting at the paste box, and a host that accepts the connection and then
// stops talking must not decide how long staging takes.
const probeTimeout = 10 * time.Second

// ytdlpProbeTimeout bounds one yt-dlp title probe (see ytdlp.Backend.ProbeTitle
// and probeYtdlpTitle in app_tasks.go, which applies this). Deliberately
// longer than probeTimeout above rather than reusing it: a plain HEAD is one
// TCP round trip, but yt-dlp's --print %(title)s still has to launch a real
// process and, for a good share of the sites it handles, fetch and parse the
// same page a full extraction would before it can answer at all - closer to
// a slow page load than a bare HEAD. Twenty seconds is a conservative
// judgement call for that shape of work rather than a number measured
// against real hosts as part of this change; it is the one constant to
// revisit first if staging a media link routinely times out its probe in
// practice, or if it turns out to be tying up probe goroutines needlessly
// long against sites that fail fast.
const ytdlpProbeTimeout = 20 * time.Second

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
	// APITokens are named, individually revocable credentials that satisfy
	// the same session guard a password does (see api.authenticated) without
	// sharing its one secret. See internal/apitoken's own package comment
	// for why that has to be a second store rather than a second password.
	APITokens *apitoken.Store
	// Scripts hosts the goja VM that runs a user's own automation snippets
	// on a task finishing, failing, the queue going idle, or on demand - see
	// internal/script's own package doc comment for the sandbox it enforces
	// and app_script.go for the Actions adapter and the Fire call sites this
	// field's own triggers are wired at (app_dispatch.go's onUpdate for the
	// two task triggers, watchQueueIdleForScripts for queue.idle).
	Scripts *script.Host
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

	// DataDir is the directory New was given. Kept verbatim rather than only
	// as the derived paths (dlDir, Store's own path, Settings' own path)
	// because backup and restore need the directory itself, not one file in
	// it — see internal/backup, which stages a validated restore beside
	// whatever New already opened rather than inside it.
	DataDir string

	// RequestExit, when set by whatever embeds this App, is how the API
	// layer's quit/restart/restore routes ask the process to actually stop.
	// App owns no *http.Server and no signal loop of its own to act on a
	// request like that — only the state Close already knows how to drain —
	// so it cannot honour this itself.
	//
	// Nil in every test and in any embedding that never sets it, which the
	// routes read as "not supported here" rather than doing nothing
	// silently: today that is the desktop build, whose window chrome and
	// tray already have their own graceful path to a.Close() and have no
	// need of this one.
	//
	// restart distinguishes only the caller's own log line and the sentence
	// the API hands back — the shutdown sequence a true return triggers is
	// identical either way, deliberately: see cmd/knightloader/main.go's own
	// comment on why quit and restart cannot be told apart from outside a
	// supervised deployment, and therefore are not told apart in here either.
	// The return value reports whether the request was accepted; false means
	// a shutdown is already under way and this one changes nothing.
	RequestExit func(restart bool) bool

	// RequestUpdateInstall, when set (desktop only, wired in
	// desktop/main.go), downloads and applies a newer release, spawns it as
	// a new process, then exits this one through the same graceful path the
	// tray's own Quit menu item uses. Nil on the container build, where
	// self-replacing the running binary makes no sense (see
	// internal/update's own package doc on why the container side of
	// "update available" only ever points at how the deployment itself
	// updates - docker pull, Unraid CA, ...), and nil in every test, read
	// the same "not supported here" way the API layer already reads
	// RequestExit==nil. The actual download/verify/swap mechanics live in
	// internal/update (deployment-agnostic, independently testable); this
	// field is only how the API layer reaches whatever embeds this App to
	// carry that out and then relaunch, the same "App owns no process
	// lifecycle of its own" reasoning as RequestExit just above.
	RequestUpdateInstall func(ctx context.Context) error

	// CnLPort and CnLToggle are set by cmd/knightloader/main.go, the only
	// embedding that starts a Click'n'Load listener today (see main.go's own
	// comment on why desktop does not). Same shape and reasoning as
	// RequestExit just above: App owns no net.Listener of its own to start
	// or stop, only whatever embeds it does, so this is a callback pair
	// rather than a field App could act on directly. Nil wherever nothing
	// wired it (every test, the desktop build) - routes_features.go's own
	// "cnl" switch case reads that as "not supported here", the same
	// convention RequestExit already established.
	//
	// CnLPort reports the actual bound port when the listener is up, 0 when
	// it is not - a real read of live state, not a guess from the
	// environment it started with, so the module row can say exactly what
	// is listening right now instead of what KL_CNL asked for at boot.
	//
	// Deliberately NOT persisted to settings.json: KL_CNL is the real,
	// deployment-level decision (should this container even try to bind the
	// port at all), and this toggle is a lighter, in-process pause/resume on
	// top of it - flipping it back off after a restart if the environment
	// still says off is the expected behaviour, not a bug to route around
	// with a second, competing on/off flag in the settings document.
	CnLPort   func() int
	CnLToggle func(on bool) error

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

	// idleAction watches the wait queue and carries out the configured
	// end-of-queue action after its cancellable countdown - see app_idle.go
	// and internal/idleaction. Owns one goroutine, started and stopped the
	// same way sched just above is; the two are independent of each other.
	idleAction *idleaction.Controller

	// wg counts the goroutines this package starts and keeps for the life of the
	// app - the housekeeping loop, and nothing else so far. Close waits on it,
	// which is the only reason it exists: every one of them writes to the store,
	// and the store is closed on the way out.
	//
	// It is deliberately not a counter for the goroutines that carry a download.
	// Those are abandoned, Close says so, and boot is what puts the list right
	// afterwards.
	wg sync.WaitGroup

	// closeMu guards closing and nothing else, and is deliberately none of the
	// four locks further down. Each of those already has a subject of its own -
	// the task list, the watcher, the backends, the compiled rule sets - and
	// mu in particular is held by callers that then reach spawn on the way out
	// (dispatchLocked publishing settled tasks, unpackLocked starting the
	// extraction worker), so registering work under mu would deadlock the
	// moment a task settled, Close or no Close. closing is what makes "is this
	// app still accepting work?" and "count me in" one atomic step; see track.
	closeMu sync.Mutex
	closing bool

	jd     backend            // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp  backend            // yt-dlp media backend, nil unless the yt-dlp binary is present
	torbox backend            // TorBox debrid backend, nil unless a TorBox key is present
	debrid map[string]backend // one-shot debrid backends by resolver id (alldebrid, realdebrid)

	dlDir string           // where engine + yt-dlp downloads land (extraction source)
	proxy *netproxy.Server // loopback proxy the engine downloads through

	// wmu guards watcher, which is replaced whenever the watched folder changes.
	wmu     sync.Mutex
	watcher *watch.Watcher

	// smu guards selfServe: this instance's own fully-wired HTTP handler
	// (auth guard and all), the same one a browser or an API token reaches.
	// Set once by internal/api.Handler as its very last step - so any earlier
	// reader sees "not ready yet" rather than a half-built stack - and read
	// by the relay client's inbound proxy handler (routes_relay.go) to
	// answer a sibling's call exactly the way this instance would answer its
	// own UI. A relay reconnect is what needs "has this changed" rather than
	// a plain field: applyRelay runs from Handler's own last step too, so a
	// second app.New/api.Handler pair in the same test binary must not read
	// back a Handler neither of them built.
	smu       sync.RWMutex
	selfServe http.Handler
	// discovery is the multicast announce/listen service, nil unless a main
	// package enabled it (buildinfo.DiscoveryEnabled).
	discovery io.Closer

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
	// The hoster-icon cache, embedded so its two fields stay in the file that
	// owns them (app_hostericons.go) instead of being two more lines here that
	// nothing in this file touches. Its map is built on first use, same as
	// unpack above, so it needs nothing in New.
	iconCache
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
	// Unconditional, like Direct and HTTPFallback above and unlike every
	// resolver in app_accounts.go: a magnet link or an uploaded .torrent needs
	// no account and no credential, so there is nothing to wait for a settings
	// change to (re)register - see torrent.Resolver's own doc comment ("it
	// carries no configuration"). Without this line Match/Resolve are correct
	// but unreachable: Registry.For walks only what was Register'd, and a
	// magnet pasted into the existing collector box would fail with "no
	// backend handles this link" despite torrent.Resolver.Match already
	// recognising it.
	a.Registry.Register(torrent.Resolver{})

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
	a.applyTorrentConfig(s.Torrent)
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
	a.idleAction, err = idleaction.NewController(idleaction.Options{
		Config:   func() idleaction.Config { return a.Settings.Get().IdleAction },
		Idle:     a.queueIdleForAction,
		Fire:     a.fireIdleAction,
		OnChange: func() { a.Hub.Broadcast("idleAction", a.IdleActionState()) },
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

	tokens, err := apitoken.Open(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.APITokens = tokens

	// Actions and Hub are the whole of what internal/script needs from this
	// package - see scriptActions' own doc comment for why that adapter
	// exists rather than *App satisfying script.Actions on its own, and
	// *hub.Hub already satisfies script.Broadcaster with no changes.
	scripts, err := script.NewHost(script.Options{DataDir: dataDir, Actions: scriptActions{a}, Hub: a.Hub})
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Scripts = scripts

	a.rewireBackends()
	a.applyWatchFolders(cfg.Get())

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
	// The queue comes up STOPPED when the resume policy says nothing should
	// start by itself, rather than coming up live over a queue nobody may run.
	// manualHalt as well as halted, and that is not a detail: the schedule
	// runner's first pass reads manualHalt as "what the user wants when no
	// window applies", so a halt written only to `halted` would be lifted again
	// a second later by a timetable that knows nothing about the boot.
	if len(requeue) > 0 && holdOnBoot(resume, queueWasLive) {
		a.halted = true
		a.manualHalt = true
	}
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
	// Same reason: idleAction reads the task list through Counters, and
	// starting it before requeue above is applied would let it see an empty
	// queue and arm a countdown for a "nothing to do" that is only true
	// because the boot has not finished putting the list back together yet.
	a.idleAction.Start()
	// Last, so nothing can sweep a list that is still being assembled. Close
	// waits for this goroutine, because everything it does writes to the store.
	//
	// Registered through track like every other a.wg.Add in this package, even
	// though nothing can turn it away here: New has not handed this *App to
	// anybody yet, so there is no Close to race. One entry point with no
	// exceptions in it is what stops the next a.wg.Add from being written the
	// unsafe way - see track. Not a.spawn, because upkeep carries its own
	// defer a.wg.Done() and spawn's wrapper would be a second one.
	if a.track() {
		go a.upkeep()
	}
	// Same ordering reason as sched.Start/idleAction.Start just above:
	// a.tasks is already whole by this point, so there is no boot-time
	// window where this could read a half-assembled queue as idle - see
	// watchQueueIdleForScripts' own doc comment.
	a.spawn(a.watchQueueIdleForScripts)
	// Same "the list is whole by now" ordering as the three above. It reads
	// a.tasks once, then spends its time in yt-dlp calls, so it is spawned
	// rather than run here: a boot must not wait on somebody else's network.
	a.spawn(a.backfillYtdlpProbes)
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
// A goroutine that arrives after Close has committed to shutting down does not
// start at all. There is no useful work left for it: everything it would write
// goes to a store that is closing, and the alternative - letting it run and
// discarding the error - is how a shutdown grows a tail nobody can measure.
// Which side of that line a given call falls on is track's decision, taken as
// one atomic step - not a context check Close can overtake between the check
// and the register.
func (a *App) spawn(f func()) {
	if !a.track() {
		return
	}
	go func() {
		defer a.wg.Done()
		f()
	}()
}

// track counts the caller in as work Close has to wait for, or reports false if
// Close has already committed to shutting down - in which case the caller must
// not touch a.wg at all. Every a.wg.Add(1) in this package goes through here;
// the matching Done stays with whoever called it.
//
// The check and the Add are one step under one lock on purpose. Written the
// obvious way instead - `if a.ctx != nil && a.ctx.Err() != nil { return }` and
// then a bare a.wg.Add(1), which is what spawn used to do - the two halves are
// a check-then-act with a gap Close can land in: the caller finds the context
// still live, Close cancels and reaches a.wg.Wait() with the counter already at
// zero so Wait returns at once, and only then does the caller's Add(1) run.
// sync.WaitGroup names that case as misuse in so many words ("calls with a
// positive delta that occur when the counter is zero must happen before a
// Wait"), and it costs one of two things: a Close that returned with work it
// was supposed to wait for still ahead of it - the availability probe, the
// checksum pass, a watch-folder job, the settled-task publish, every one of
// them writing into the store this same Close is about to shut, which is the
// exact tail spawn exists to prevent - or an outright "sync: WaitGroup misuse:
// Add called concurrently with Wait" panic taking the process down. Holding
// closeMu across both halves closes the gap: a caller that gets in before
// Close's flip has its Add(1) done before Wait can be reached, and one that
// arrives after it is turned away instead of racing for the register.
//
// Nothing is called while closeMu is held, and Close lets it go again before it
// cancels and waits. Both of those matter, because spawn's callers reach it
// holding locks of their own - see closeMu's own comment on the struct.
func (a *App) track() bool {
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	if a.closing {
		return false
	}
	a.wg.Add(1)
	return true
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
// The order below is the contract. Refuse new work and cancel first so nothing
// new begins, then the goroutines this package owns, then the subsystems, and
// the store last because every one of them writes to it.
//
// Calling it twice is harmless: the flag and cancel are both idempotent, a
// second Wait on a drained WaitGroup returns immediately, and every subsystem
// below either has its own closeOnce or takes a second Close without
// complaining.
// SetSelfServeHandler stores this instance's own fully-wired HTTP handler,
// so the relay client's inbound proxy handler (routes_relay.go) can answer a
// sibling's call exactly the way this instance would answer a browser's or
// an API token's - same auth guard, same routes, no second surface to keep
// in sync with the first. Called once, by internal/api.Handler as its very
// last step; nil until then, which SelfServeHandler's own callers read as
// "not ready yet" rather than nothing happening silently.
func (a *App) SetSelfServeHandler(h http.Handler) {
	a.smu.Lock()
	a.selfServe = h
	a.smu.Unlock()
}

// SetDiscovery stores the network-discovery service so Close can stop it,
// the same arrangement the relay has via SetRelay(nil) below - a shutting
// down instance must stop announcing that it is there.
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

func (a *App) Close() error {
	// Flipped first, under the same lock track takes, so that from here on no
	// spawn can still slip a wg.Add(1) past the Wait below - see track's own
	// comment for what that used to cost. cancel() stays after it: the flag is
	// what refuses new work, cancel is what stops work already under way.
	a.closeMu.Lock()
	a.closing = true
	a.closeMu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	// Stops and closes whatever relay connection is currently open, the same
	// as any other SetRelay(nil) call - a shutting-down instance has no
	// business staying registered on a relay it is about to stop answering
	// for.
	if a.Federation != nil {
		a.Federation.SetRelay(nil)
	}
	// Same reasoning one line up: stop telling the network this instance is
	// available while it is shutting down.
	a.smu.Lock()
	disc := a.discovery
	a.discovery = nil
	a.smu.Unlock()
	if disc != nil {
		_ = disc.Close()
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
	// Same promise as sched just above: Close blocks on an in-flight tick, so
	// a Fire (SetHalted) already under way finishes before anything it might
	// touch is torn down further down this function.
	if a.idleAction != nil {
		_ = a.idleAction.Close()
	}
	// Same promise as sched/idleAction just above: Close waits for an
	// in-flight script (Fire's worker pool, or a RunNow call) to finish
	// before anything it might call back into (Pause/Resume/RestartTasks/
	// the Store) is torn down further down this function - see
	// internal/script's own package doc comment, "every goroutine this
	// package starts is tracked".
	if a.Scripts != nil {
		_ = a.Scripts.Close()
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

// titleProber is implemented by a backend that can look up a link's real name
// (and, since this round's probe upgrade, its real available formats) without
// downloading it - the yt-dlp counterpart to the collector's own HEAD probe
// (analyze, app_tasks.go) for a plain file link. Optional for the same reason
// speedLimiter above is: most backends already report a real name off their
// own progress stream once a download starts (a backend's Download reaching
// onUpdate with Update.Name set), and forcing every one of them to grow a
// method that would just return a zero value is the wrong trade for what
// only one of them can actually answer ahead of time. See
// ytdlp.Backend.ProbeTitle and probeYtdlpTitle in app_tasks.go, the two
// halves of the one caller this exists for.
//
// Returns ytdlp.ProbeResult directly rather than a locally-decoupled shape:
// app_ytdlp_variants.go already imports the ytdlp package concretely for
// Variant/HosterPreset, so this interface staying "decoupled" bought nothing
// real - the package already depends on ytdlp's own vocabulary elsewhere.
type titleProber interface {
	ProbeTitle(ctx context.Context, url string) (ytdlp.ProbeResult, error)
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
	a.afterSettingsChange(applied)
	return applied, nil
}

// PatchSettings is ApplySettings for a partial update: patch names only the
// top-level fields it means to change, and settings.Store.SetPartial reads
// every other field from whatever is stored right now, under the same lock
// that then writes the merged result back. See that method's own comment
// for why that has to be one critical section rather than a Get here
// followed by a Set. Everything after the save runs exactly as it does for a
// full PUT: a live-effect subsystem re-reading Settings.Get() would not be
// able to tell the two apart, and it must not have to.
func (a *App) PatchSettings(patch map[string]json.RawMessage) (settings.Settings, error) {
	applied, err := a.Settings.SetPartial(patch)
	if err != nil {
		return applied, err
	}
	a.afterSettingsChange(applied)
	return applied, nil
}

// afterSettingsChange is what ApplySettings and PatchSettings share: every
// runtime effect a saved settings document can have, applied against
// whichever one of them just landed on disk. Kept as one function precisely
// so the two ways of saving cannot drift into applying a different subset of
// this list, where a field patch could reach the store but not the scheduler.
func (a *App) afterSettingsChange(applied settings.Settings) {
	a.applyRuleSets(applied)
	// The speed limit goes through the timetable and never straight to the
	// limiter. Writing it here as well would let a saved settings page lift a
	// nightly cap that is still in force, until whichever boundary came next.
	// Set recompiles and re-evaluates at once against the new base, so the runner
	// is the one that hands the limiter its answer.
	a.sched.Set(applied.Schedule)
	// Re-evaluated now rather than at idleAction's own next poll, so turning
	// the action on (or off) while the queue happens to already be idle takes
	// effect immediately instead of up to a couple of seconds later.
	a.idleAction.Refresh()
	a.applyWatchFolders(applied)
	a.applyConnections(applied.Connections)
	a.applyTorrentConfig(applied.Torrent)
	a.mu.Lock()
	if p := dedupe.ParsePolicy(applied.MirrorPolicy); p != a.dupes.Policy() {
		// The policy is baked in at New, so a change needs a new set, re-seeded
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

// ytdlpTitleProber returns the yt-dlp backend as a titleProber, and whether
// it actually is one - the same pattern pushJDSpeedLimit above uses for its
// own optional interface. False when no yt-dlp backend is wired at all
// (a.ytdlp nil, no binary present at boot) exactly as much as when one is
// wired that does not implement it; either way probeYtdlpTitle's only caller
// has nothing to do.
func (a *App) ytdlpTitleProber() (titleProber, bool) {
	a.bmu.RLock()
	b := a.ytdlp
	a.bmu.RUnlock()
	tp, ok := b.(titleProber)
	return tp, ok
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

// applyTorrentConfig pushes the current seed-ratio/seed-duration/port policy
// into the engine - the one live effect settings.Torrent currently has on a
// running torrent. See Engine.SetTorrentConfig's own doc comment for exactly
// what "reaches" means for each of the three: seed-ratio and seed-duration
// take for every torrent added from here on, port only takes if no torrent
// has started yet this process. Logged and swallowed rather than propagated,
// the same non-fatal risk posture already established for engine.go's own
// UseProxy call at boot: a failed push here is a setting not yet in effect,
// not a reason to fail the save or the boot that called this.
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
