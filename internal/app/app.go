// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; a download backend (the Gopeed engine or
// headless JD) reports changes, the app persists them and broadcasts them.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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

	mu      sync.Mutex
	tasks   map[string]*core.Task
	queue   []string        // task IDs waiting for a slot, FIFO with per-host skip-ahead
	active  map[string]bool // dispatched and not yet terminal/paused
	started map[string]bool // ever handed to a backend (Resume vs fresh Download)
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
		Crawler:    crawler.HTML{},
		tasks:      map[string]*core.Task{},
		active:     map[string]bool{},
		started:    map[string]bool{},
		debrid:     map[string]backend{},
	}
	a.Registry.Register(resolver.Direct{})
	a.Registry.Register(resolver.HTTPFallback{})

	eng, err := engine.New(filepath.Join(dataDir, "downloads"), a.onUpdate)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Engine = eng
	a.Throttle.Set(cfg.Get().SpeedLimit)

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

	// Reload persisted tasks; anything mid-flight shows as paused on boot, and
	// an interrupted extraction counts as done (the download itself finished).
	existing, err := st.All()
	if err != nil {
		return nil, err
	}
	for _, t := range existing {
		if t.Status == core.StatusRunning || t.Status == core.StatusQueued {
			t.Status = core.StatusPaused
			t.Speed = 0
		}
		if t.Status == core.StatusExtracting {
			t.Status = core.StatusDone
			t.Speed = 0
		}
		a.tasks[t.ID] = t
	}
	return a, nil
}

// rewireBackends rebuilds the resolver routing table and the download backends
// from the credentials currently stored. It runs at startup and again whenever
// an account changes, so adding or removing a debrid key takes effect
// immediately instead of on the next restart. Everything is assembled into
// locals first and swapped in at the end, so a running download never sees a
// half-built table.
func (a *App) rewireBackends() {
	eng, acc := a.Engine, a.Accounts

	// Resolve which hoster backends are configured. Each debrid service brings
	// its own supported-host list; their union tells file hosters (→ debrid/JD)
	// from media pages (→ yt-dlp).
	torboxKey := credential(acc, "torbox", "KL_TORBOX")
	jdBase := os.Getenv("KL_JD")

	var hosterSet map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = fetchTorboxHosters(torboxKey)
	}

	// One-shot debrid services (AllDebrid, Real-Debrid): a single unlock call
	// yields a direct URL the engine downloads.
	type debridSetup struct {
		svc  debrid.Service
		prio int
	}
	var configured []debridSetup
	if k := credential(acc, "alldebrid", "KL_ALLDEBRID"); k != "" {
		configured = append(configured, debridSetup{debrid.NewAllDebrid(k), 34})
	}
	if k := credential(acc, "realdebrid", "KL_REALDEBRID"); k != "" {
		configured = append(configured, debridSetup{debrid.NewRealDebrid(k), 33})
	}
	newDebrid := map[string]backend{}
	for _, d := range configured {
		hosts := fetchDebridHosts(d.svc)
		newDebrid[d.svc.ID()] = debrid.NewBackend(d.svc, eng, a.onUpdate)
		a.Registry.Register(debrid.Resolver{ServiceID: d.svc.ID(), Prio: d.prio, Hosts: hosts})
		for h := range hosts {
			if hosterSet == nil {
				hosterSet = map[string]bool{}
			}
			hosterSet[h] = true
		}
		log.Printf("%s debrid backend enabled (%d supported hosts)", d.svc.Label(), len(hosts))
	}

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-hoster, non-file links) route through it.
	var newYtdlp backend
	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		yb.RateLimit = func() int64 { return a.Settings.Get().SpeedLimit }
		yb.Dir = a.taskDir
		newYtdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: hosterSet})
		log.Printf("yt-dlp backend enabled: %s", ytbin)
	}

	// Optional TorBox debrid backend: when a key is present, supported hoster
	// links are unlocked into a direct CDN URL the engine then downloads.
	var newTorbox backend
	if torboxKey != "" {
		newTorbox = torbox.NewBackend(torbox.NewClient(torboxKey), eng, a.onUpdate)
		a.Registry.Register(torbox.Resolver{Hosts: hosterSet})
		log.Printf("TorBox debrid backend enabled (%d supported hosts)", len(hosterSet))
	}

	// Optional headless-JD backend: the lowest-priority catch-all for hoster
	// links nothing else claims, via JD's crawler and hoster plugins.
	var newJD backend
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			newJD = jb
			a.Registry.Register(jd.Resolver{})
			log.Printf("headless JD backend enabled: %s", jdBase)
		}
	}

	// A credential that is gone must stop claiming links, or those links would
	// route to a service that can no longer unlock them.
	for _, id := range []string{"alldebrid", "realdebrid"} {
		if _, ok := newDebrid[id]; !ok {
			a.Registry.Unregister(id)
		}
	}
	if newTorbox == nil {
		a.Registry.Unregister("torbox")
	}
	if newJD == nil {
		a.Registry.Unregister("jd")
	}
	if newYtdlp == nil {
		a.Registry.Unregister("ytdlp")
	}

	a.bmu.Lock()
	a.debrid, a.ytdlp, a.torbox, a.jd = newDebrid, newYtdlp, newTorbox, newJD
	a.bmu.Unlock()
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

func (a *App) Close() error {
	a.wmu.Lock()
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	a.wmu.Unlock()
	if a.proxy != nil {
		_ = a.proxy.Close()
	}
	if a.Engine != nil {
		a.Engine.Close()
	}
	return a.Store.Close()
}

// Tasks returns a snapshot sorted oldest-first.
func (a *App) Tasks() []*core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*core.Task, 0, len(a.tasks))
	for _, t := range a.tasks {
		c := *t
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// AddLinks resolves each URL and stages it in the link collector (JD-style):
// tasks are created "collected" (analysed but not started). StartTasks moves
// them into the download queue.
func (a *App) AddLinks(urls []string, pkg string) []*core.Task {
	// Skip URLs that are already in flight, so pasting the same list twice
	// doesn't queue a second copy. Tasks that have settled — finished or failed
	// — never block: re-adding one of those is a deliberate second attempt.
	a.mu.Lock()
	known := make(map[string]bool, len(a.tasks))
	for _, t := range a.tasks {
		if t.Status != core.StatusDone && t.Status != core.StatusError {
			known[t.URL] = true
		}
	}
	a.mu.Unlock()

	var created []*core.Task
	seen := map[string]bool{}
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" || known[u] || seen[u] {
			continue
		}
		seen[u] = true
		// A page that points at files becomes those files, not one task for the
		// page. Without this a gallery or an index listing can only ever be a
		// single unusable download.
		if crawled := a.crawl(u); len(crawled) > 0 {
			for _, c := range crawled {
				if c.URL == "" || seen[c.URL] || known[c.URL] {
					continue
				}
				seen[c.URL] = true
				if t := a.stage(c.URL, c.Name, pkg); t != nil {
					created = append(created, t)
				}
			}
			continue
		}
		// A link is never dropped on the floor. If nothing can handle it, or
		// resolving fails, it is still staged — with the reason on it — so the
		// user can see what happened instead of watching links vanish.
		t := &core.Task{
			URL:       u,
			Name:      u,
			Package:   pkg,
			Status:    core.StatusCollected,
			CreatedAt: time.Now(),
		}
		res := a.Registry.For(u)
		if res == nil {
			t.Error = "no backend handles this link"
			t.Online = core.AvailOffline
			a.put(t)
			created = append(created, t)
			continue
		}
		t.Resolver = res.Info().ID
		result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
		if err != nil {
			t.Error = err.Error()
			a.put(t)
			created = append(created, t)
			continue
		}
		if result.Name != "" {
			t.Name = result.Name
		}
		t.Size = result.Size
		a.put(t)
		// Lightweight analysis for plain file links: a HEAD gives size + an
		// online check while the task waits in the collector.
		if res.Info().ID == "direct" {
			go a.analyze(t.ID, result.DirectURL)
		}
		created = append(created, t)
	}
	// A batch pasted without a package name gets one derived from the links
	// themselves. Without this everything lands in "ungrouped", which is where
	// a collector stops being useful the moment there is more than one batch in
	// it.
	if strings.TrimSpace(pkg) == "" {
		if derived := derivePackage(created); derived != "" {
			ids := make([]string, 0, len(created))
			for _, t := range created {
				ids = append(ids, t.ID)
			}
			a.SetPackage(ids, derived)
		}
	}

	// Auto-start hands everything straight to the queue for users who don't
	// want the staging step.
	if len(created) > 0 && a.Settings.Get().AutoStart {
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		a.StartTasks(ids)
	}
	return created
}

// derivePackage guesses a name for a batch that arrived without one: the shared
// stem of the file names if the links look like parts of one thing, else the
// host they came from. It returns "" when neither is worth using, because a bad
// guess is worse than no group at all.
func derivePackage(tasks []*core.Task) string {
	if len(tasks) == 0 {
		return ""
	}
	names := make([]string, 0, len(tasks))
	hosts := map[string]bool{}
	for _, t := range tasks {
		hosts[hostOf(t.URL)] = true
		if n := fileStem(t); n != "" {
			names = append(names, n)
		}
	}
	if stem := commonStem(names); len(stem) >= 3 {
		return sanitizeSegment(stem)
	}
	// One host and nothing else in common: the source is the only honest label.
	if len(hosts) == 1 {
		for h := range hosts {
			if h != "" && !strings.HasPrefix(h, "http") {
				return sanitizeSegment(h)
			}
		}
	}
	return ""
}

// fileStem is the part of a task's file name that identifies the thing rather
// than the part: "film.part03.rar" and "film.r02" both reduce to "film".
func fileStem(t *core.Task) string {
	name := t.Name
	if name == "" || strings.Contains(name, "://") {
		// Nothing resolved yet, so the URL's last segment is the best we have.
		if u, err := url.Parse(t.URL); err == nil {
			name = path.Base(u.Path)
		}
	}
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if key, ok := extract.SetKey(name); ok {
		// SetKey lower-cases its base because it is a grouping key. A package
		// name is read by a person, so take only the LENGTH from it and slice
		// the original, which keeps the capitalisation the release came with.
		if base, _, cut := strings.Cut(key, "|"); cut && len(base) <= len(name) {
			return name[:len(base)]
		}
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

// commonStem is the longest prefix every name shares. When that prefix cuts a
// name short it is trimmed back to a separator, because half a word
// ("Movie.S01E0") is a worse label than the shorter whole one. When every name
// is identical nothing was cut, so nothing is trimmed either.
func commonStem(names []string) string {
	if len(names) == 0 {
		return ""
	}
	stem := names[0]
	truncated := false
	for _, n := range names[1:] {
		i := 0
		for i < len(stem) && i < len(n) && stem[i] == n[i] {
			i++
		}
		if i < len(stem) || i < len(n) {
			truncated = true
		}
		stem = stem[:i]
		if stem == "" {
			return ""
		}
	}
	if truncated {
		if i := strings.LastIndexAny(stem, ".-_ "); i > 0 {
			stem = stem[:i]
		}
	}
	return strings.Trim(stem, ".-_ ")
}

// crawl asks the page crawler what a link points at. It returns nothing when
// crawling is off, when the link is already a file, or when the page yielded
// nothing — in every one of those cases the link is staged as itself.
func (a *App) crawl(u string) []crawler.Result {
	if !a.Settings.Get().Crawl {
		return nil
	}
	// Which backends mean "this might be a page". The HTTP fallback means
	// nobody recognised the link at all. yt-dlp is in the set because it claims
	// by exclusion rather than by knowledge — it takes every http link that is
	// not a known hoster — so gating on the fallback alone meant the crawler
	// never ran at all on any install that has yt-dlp, which is all of them.
	//
	// Everything else stays out: a direct file link is already a download, and
	// a debrid or JD link belongs to a hoster whose page holds nothing we could
	// fetch ourselves.
	if res := a.Registry.For(u); res != nil {
		switch res.Info().ID {
		case "http", "ytdlp":
		default:
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	found, err := a.Crawler.Crawl(ctx, u)
	if err != nil {
		log.Printf("crawl %s: %v", u, err)
		return nil
	}
	// One result that is the page itself is not a crawl, it is the same link
	// back; staging it through the normal path keeps the resolver choice honest.
	if len(found) == 1 && found[0].URL == u {
		return nil
	}
	return found
}

// stage creates one collected task for a URL, resolving it the same way a
// pasted link is resolved. It returns nil only when the URL is unusable.
func (a *App) stage(u, name, pkg string) *core.Task {
	t := &core.Task{
		URL:       u,
		Name:      u,
		Package:   pkg,
		Status:    core.StatusCollected,
		CreatedAt: time.Now(),
	}
	if strings.TrimSpace(name) != "" {
		t.Name = name
	}
	res := a.Registry.For(u)
	if res == nil {
		t.Error = "no backend handles this link"
		t.Online = core.AvailOffline
		a.put(t)
		return t
	}
	t.Resolver = res.Info().ID
	result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
	if err != nil {
		t.Error = err.Error()
		a.put(t)
		return t
	}
	if result.Name != "" {
		t.Name = result.Name
	}
	t.Size = result.Size
	a.put(t)
	if res.Info().ID == "direct" {
		go a.analyze(t.ID, result.DirectURL)
	}
	return t
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

// SetPackage moves tasks into a package (an empty name ungroups them).
func (a *App) SetPackage(ids []string, pkg string) {
	pkg = strings.TrimSpace(pkg)
	a.mu.Lock()
	var copies []core.Task
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			t.Package = pkg
			copies = append(copies, *t)
		}
	}
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// StartTasks moves collected tasks into the download queue and dispatches them.
// An empty id list starts every collected task.
func (a *App) StartTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	a.mu.Lock()
	var toStart []*core.Task
	for id, t := range a.tasks {
		if t.Status == core.StatusCollected && (all || want[id]) {
			toStart = append(toStart, t)
		}
	}
	sort.Slice(toStart, func(i, j int) bool { return toStart[i].CreatedAt.Before(toStart[j].CreatedAt) })
	copies := make([]core.Task, 0, len(toStart))
	for _, t := range toStart {
		t.Status = core.StatusQueued
		t.Error = ""
		t.Speed = 0
		a.queue = append(a.queue, t.ID)
		copies = append(copies, *t)
	}
	a.dispatchLocked()
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// RestartTasks re-runs finished or errored tasks from scratch: their backend
// state is cleared and they re-enter the download queue. Empty ids = every
// errored task.
func (a *App) RestartTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	a.mu.Lock()
	type reset struct {
		id string
		be backend
	}
	var targets []reset
	for id, t := range a.tasks {
		restartable := t.Status == core.StatusError || (t.Status == core.StatusDone && !all)
		if restartable && (all || want[id]) {
			targets = append(targets, reset{id, a.backendFor(t.Resolver)})
			t.Status = core.StatusQueued
			t.Error = ""
			t.Loaded = 0
			t.Speed = 0
			delete(a.active, id)
			delete(a.started, id) // dispatch will hand it to the backend fresh
		}
	}
	a.mu.Unlock()

	// Clear any leftover backend state before re-queuing.
	for _, r := range targets {
		r.be.Remove(r.id, true)
	}

	a.mu.Lock()
	var copies []core.Task
	for _, r := range targets {
		if t := a.tasks[r.id]; t != nil {
			a.queue = append(a.queue, r.id)
			copies = append(copies, *t)
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// resolverForTaskLocked picks the resolver a task should be dispatched through:
// the one recorded on it if that still exists, else the best current match.
// Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	if t.Resolver != "" {
		for _, res := range a.Registry.All(t.URL) {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	return a.Registry.For(t.URL)
}

// nextResolverLocked returns the resolver that should try after the one the
// task just used, or "" when the chain is exhausted. Caller holds a.mu.
func (a *App) nextResolverLocked(t *core.Task) string {
	chain := a.Registry.All(t.URL)
	for i, res := range chain {
		if res.Info().ID == t.Resolver {
			if i+1 < len(chain) {
				return chain[i+1].Info().ID
			}
			return "" // the chain is exhausted
		}
	}
	// The recorded backend is not registered any more — a credential was
	// removed, or a binary went missing. Restart the chain from the top rather
	// than leaving the task stranded on a backend that no longer exists.
	for _, res := range chain {
		if res.Info().ID != t.Resolver {
			return res.Info().ID
		}
	}
	return ""
}

// setAvailability records what a check learned about a link. It is separate
// from Update because availability is a property of the link, not of a download
// attempt: a staged link can be known-dead before anything is started.
func (a *App) setAvailability(id string, avail core.Availability, msg string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Online = avail
	t.Error = msg
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// RecheckTasks re-runs resolution and the availability probe for collected
// tasks, so a link that was dead an hour ago can be tried again without
// re-pasting it. An empty id list rechecks everything in the collector.
func (a *App) RecheckTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var targets []core.Task
	for id, t := range a.tasks {
		if t.Status == core.StatusCollected && (all || want[id]) {
			targets = append(targets, *t)
		}
	}
	a.mu.Unlock()

	for i := range targets {
		t := targets[i]
		res := a.Registry.For(t.URL)
		if res == nil {
			a.setAvailability(t.ID, core.AvailOffline, "no backend handles this link")
			continue
		}
		result, err := res.Resolve(context.Background(), resolver.Request{URL: t.URL})
		if err != nil {
			a.setAvailability(t.ID, core.AvailOffline, err.Error())
			continue
		}
		a.mu.Lock()
		if live := a.tasks[t.ID]; live != nil {
			live.Resolver = res.Info().ID
			if result.Name != "" {
				live.Name = result.Name
			}
		}
		a.mu.Unlock()
		if res.Info().ID == "direct" {
			a.analyze(t.ID, result.DirectURL)
		} else {
			// Other backends cannot probe without starting; clear a stale error
			// and let the attempt decide.
			a.setAvailability(t.ID, core.AvailUnknown, "")
		}
	}
}

// analyze probes a plain file link with a HEAD request to fill in its size and
// flag it offline, updating the collected task in place.
func (a *App) analyze(id, rawurl string) {
	req, err := http.NewRequest(http.MethodHead, rawurl, nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.setAvailability(id, core.AvailOffline, "offline: "+err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		a.setAvailability(id, core.AvailOffline, "offline (HTTP "+strconv.Itoa(resp.StatusCode)+")")
		return
	}
	a.setAvailability(id, core.AvailOnline, "")
	if resp.ContentLength > 0 {
		a.onUpdate(id, core.Update{Size: resp.ContentLength})
	}
}

// dispatchLocked starts queued tasks while slots are free. FIFO with per-host
// skip-ahead: a host at its limit doesn't block other hosts behind it.
// Caller holds a.mu.
func (a *App) dispatchLocked() {
	cfg := a.Settings.Get()
	a.sortQueueLocked()
	perHost := map[string]int{}
	for id := range a.active {
		if t := a.tasks[id]; t != nil {
			perHost[hostOf(t.URL)]++
		}
	}
	var rest []string
	for _, id := range a.queue {
		t := a.tasks[id]
		if t == nil {
			continue // removed while queued
		}
		if len(a.active) >= cfg.MaxConcurrent {
			rest = append(rest, id)
			continue
		}
		h := hostOf(t.URL)
		if perHost[h] >= cfg.MaxPerHost {
			rest = append(rest, id)
			continue
		}
		if a.started[id] {
			a.active[id] = true
			perHost[h]++
			go a.backendFor(t.Resolver).Resume(id)
			continue
		}
		// Honour the resolver already recorded on the task: after a fallback it
		// is deliberately not the highest-priority match any more.
		res := a.resolverForTaskLocked(t)
		if res == nil {
			t.Status = core.StatusError
			t.Error = "no resolver matches"
			continue
		}
		t.Resolver = res.Info().ID
		result, err := res.Resolve(context.Background(), resolver.Request{URL: t.URL})
		if err != nil {
			t.Status = core.StatusError
			t.Error = err.Error()
			continue
		}
		a.active[id] = true
		a.started[id] = true
		perHost[h]++
		conns := result.Connections
		if conns <= 0 {
			conns = 4
		}
		dir := a.dirFor(t)
		if be := a.backendFor(t.Resolver); be == a.Engine {
			go a.Engine.DownloadTo(id, result.DirectURL, result.Headers, conns, dir)
		} else {
			go be.Download(id, result.DirectURL, result.Headers, conns)
		}
	}
	a.queue = rest
}

// sortQueueLocked puts the wait queue in the order the user asked for: higher
// priority first, then the manual position, then oldest first. Caller holds a.mu.
func (a *App) sortQueueLocked() {
	sort.SliceStable(a.queue, func(i, j int) bool {
		x, y := a.tasks[a.queue[i]], a.tasks[a.queue[j]]
		if x == nil || y == nil {
			return y == nil && x != nil
		}
		if x.Priority != y.Priority {
			return x.Priority > y.Priority
		}
		if x.Position != y.Position {
			return x.Position < y.Position
		}
		return x.CreatedAt.Before(y.CreatedAt)
	})
}

// SetPriority lifts or drops tasks in the wait queue. Higher runs first; the
// change takes effect immediately for anything not already downloading.
func (a *App) SetPriority(ids []string, priority int) {
	if priority < -2 {
		priority = -2
	}
	if priority > 2 {
		priority = 2
	}
	a.mu.Lock()
	var copies []core.Task
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			t.Priority = priority
			copies = append(copies, *t)
		}
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
}

// MoveTasks reorders the queue by hand. where is "top" or "bottom"; everything
// keeps its priority, so a moved task still waits behind higher-priority work.
func (a *App) MoveTasks(ids []string, where string) {
	a.mu.Lock()
	min, max := 0, 0
	for _, t := range a.tasks {
		if t.Position < min {
			min = t.Position
		}
		if t.Position > max {
			max = t.Position
		}
	}
	var copies []core.Task
	for i, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if where == "top" {
			t.Position = min - len(ids) + i
		} else {
			t.Position = max + 1 + i
		}
		copies = append(copies, *t)
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
}

// TaskOptions are the per-task overrides the UI can set. A nil field means
// "leave as it is", which keeps a partial edit from wiping the other values.
type TaskOptions struct {
	Dir      *string `json:"dir,omitempty"`
	Password *string `json:"password,omitempty"`
}

// SetTaskOptions applies per-task overrides (destination folder, archive
// password). Changing the folder of a running task only affects a later
// restart — the bytes already on disk stay where they are.
func (a *App) SetTaskOptions(ids []string, o TaskOptions) error {
	if o.Dir != nil && *o.Dir != "" {
		if err := settings.Validate(*o.Dir); err != nil {
			return err
		}
	}
	a.mu.Lock()
	var copies []core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if o.Dir != nil {
			t.Dir = strings.TrimSpace(*o.Dir)
		}
		if o.Password != nil {
			t.Password = strings.TrimSpace(*o.Password)
		}
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return nil
}

// saveAndBroadcast persists task snapshots and pushes them to connected UIs.
func (a *App) saveAndBroadcast(copies []core.Task) {
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// hostOf returns the scheduling host bucket for a URL.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func (a *App) Pause(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	wasActive := a.active[id]
	delete(a.active, id)
	a.dequeueLocked(id)
	if !wasActive {
		// Still waiting in the queue: just mark it paused.
		t.Status = core.StatusPaused
		t.Speed = 0
		c := *t
		a.mu.Unlock()
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
		return
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.backendFor(t.Resolver).Pause(id)
}

func (a *App) Resume(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || a.active[id] {
		a.mu.Unlock()
		return
	}
	t.Status = core.StatusQueued
	t.Speed = 0
	c := *t
	a.dequeueLocked(id)
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// Remove drops a task from the list. deleteFiles additionally erases what was
// downloaded — never the default: tidying the list must not destroy finished
// files, which is also how JDownloader behaves.
func (a *App) Remove(id string, deleteFiles bool) {
	a.mu.Lock()
	t := a.tasks[id]
	delete(a.tasks, id)
	delete(a.active, id)
	delete(a.started, id)
	a.dequeueLocked(id)
	a.dispatchLocked()
	a.mu.Unlock()
	if t != nil {
		a.backendFor(t.Resolver).Remove(id, deleteFiles)
	}
	_ = a.Store.Delete(id)
	a.Hub.Broadcast("removed", map[string]string{"id": id})
}

// dequeueLocked removes id from the wait queue. Caller holds a.mu.
func (a *App) dequeueLocked(id string) {
	for i, q := range a.queue {
		if q == id {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			return
		}
	}
}

func (a *App) backendFor(resolverID string) backend {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	if b, ok := a.debrid[resolverID]; ok && b != nil {
		return b
	}
	switch {
	case resolverID == "jd" && a.jd != nil:
		return a.jd
	case resolverID == "torbox" && a.torbox != nil:
		return a.torbox
	case resolverID == "ytdlp" && a.ytdlp != nil:
		return a.ytdlp
	default:
		return a.Engine
	}
}

// credential reads a service secret from the encrypted store, with the env var
// taking precedence (handy for containers and tests).
func credential(acc *accounts.Store, service, envVar string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	v, _ := acc.Get(service)
	return v
}

// fetchDebridHosts returns a service's supported-host set, or nil when the
// list can't be fetched (routing then degrades to the other backends).
func fetchDebridHosts(svc debrid.Service) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hosts, err := svc.Hosts(ctx)
	if err != nil {
		log.Printf("%s host list unavailable (%v); its routing is disabled", svc.Label(), err)
		return nil
	}
	return hosts
}

// SetAccount stores (or, with an empty secret, clears) a credential for a
// service such as "torbox", and re-wires the backends so it takes effect right
// away — a saved key that only works after a restart is a key that looks broken.
func (a *App) SetAccount(service, secret string) error {
	if err := a.Accounts.Set(service, secret); err != nil {
		return err
	}
	a.rewireBackends()
	return nil
}

// AccountState is what the settings page shows per service: whether a
// credential is stored, whether it currently works, and what the service said.
type AccountState struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
	FromEnv    bool   `json:"fromEnv"` // supplied by the container, not editable here
	OK         bool   `json:"ok"`
	Detail     string `json:"detail"`
	Hosts      int    `json:"hosts"` // supported hosters the service reports
}

// knownServices is the fixed set of credential slots the UI offers, with the
// environment variable that can supply each one instead.
var knownServices = []struct{ id, label, env string }{
	{"torbox", "TorBox", "KL_TORBOX"},
	{"alldebrid", "AllDebrid", "KL_ALLDEBRID"},
	{"realdebrid", "Real-Debrid", "KL_REALDEBRID"},
}

// AccountStates reports every credential slot without contacting anyone.
func (a *App) AccountStates() []AccountState {
	out := make([]AccountState, 0, len(knownServices))
	for _, svc := range knownServices {
		st := AccountState{ID: svc.id, Label: svc.label}
		if v := os.Getenv(svc.env); v != "" {
			st.Configured, st.FromEnv = true, true
		} else if v, _ := a.Accounts.Get(svc.id); v != "" {
			st.Configured = true
		}
		out = append(out, st)
	}
	return out
}

// TestAccount checks a stored credential against the service and reports what
// came back, so a typo in a key is visible here instead of on the first download.
func (a *App) TestAccount(service string) AccountState {
	st := AccountState{ID: service, Label: service}
	for _, svc := range knownServices {
		if svc.id == service {
			st.Label = svc.label
			st.FromEnv = os.Getenv(svc.env) != ""
		}
	}
	key := ""
	for _, svc := range knownServices {
		if svc.id == service {
			key = credential(a.Accounts, svc.id, svc.env)
		}
	}
	if key == "" {
		st.Detail = "no credential stored"
		return st
	}
	st.Configured = true

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var hosts map[string]bool
	var err error
	switch service {
	case "torbox":
		var list []torbox.Hoster
		if list, err = torbox.NewClient(key).Hosters(ctx); err == nil {
			hosts = map[string]bool{}
			for _, h := range list {
				for _, d := range h.Domains {
					hosts[d] = true
				}
			}
		}
	case "alldebrid":
		hosts, err = debrid.NewAllDebrid(key).Hosts(ctx)
	case "realdebrid":
		hosts, err = debrid.NewRealDebrid(key).Hosts(ctx)
	default:
		st.Detail = "unknown service"
		return st
	}
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	st.OK, st.Hosts = true, len(hosts)
	st.Detail = "credential accepted"
	return st
}

// AddLinksCnL satisfies the Click'n'Load listener's Adder interface. A CnL
// submission can carry the archive passwords for what it is sending, which is
// exactly the moment we can learn them without asking the user.
func (a *App) AddLinksCnL(urls []string, pkg string, passwords []string) {
	a.AddLinksWithPasswords(urls, pkg, passwords)
}

// AddLinksWithPasswords stages links that arrived together with the archive
// passwords for them. The first password rides on the tasks themselves, because
// it was supplied for exactly these files; the rest join the global list, where
// a later archive from the same source can still reach them.
func (a *App) AddLinksWithPasswords(urls []string, pkg string, passwords []string) []*core.Task {
	created := a.AddLinks(urls, pkg)
	var first string
	for _, pw := range passwords {
		if pw = strings.TrimSpace(pw); pw != "" {
			first = pw
			break
		}
	}
	if first == "" || len(created) == 0 {
		return created
	}
	ids := make([]string, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}
	if err := a.SetTaskOptions(ids, TaskOptions{Password: &first}); err != nil {
		log.Printf("could not apply the supplied archive password: %v", err)
	}
	if len(passwords) > 1 {
		a.rememberPasswords(passwords)
	}
	return created
}

// rememberPasswords folds passwords a submission brought along into the global
// list, so a later archive from the same source can still be opened.
func (a *App) rememberPasswords(passwords []string) {
	cfg := a.Settings.Get()
	known := map[string]bool{}
	for _, p := range cfg.ArchivePasswords {
		known[p] = true
	}
	added := false
	for _, p := range passwords {
		if p = strings.TrimSpace(p); p != "" && !known[p] {
			cfg.ArchivePasswords = append(cfg.ArchivePasswords, p)
			known[p] = true
			added = true
		}
	}
	if added {
		if _, err := a.Settings.Set(cfg); err != nil {
			log.Printf("could not store the passwords a submission brought along: %v", err)
		}
	}
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
	// The limiter meters every byte that comes through the loopback proxy, so a
	// changed limit takes effect on running downloads, not just on the next one.
	a.Throttle.Set(applied.SpeedLimit)
	a.applyWatcher(applied.WatchDir)
	a.bmu.RLock()
	jdBackend := a.jd
	a.bmu.RUnlock()
	if sl, ok := jdBackend.(speedLimiter); ok {
		if err := sl.SetSpeedLimit(applied.SpeedLimit); err != nil {
			log.Printf("JD speed limit not applied: %v", err)
		}
	}
	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()
	return applied, nil
}

// fetchTorboxHosters returns the set of TorBox-supported hoster domains, or nil
// if the list can't be fetched (routing then degrades gracefully).
func fetchTorboxHosters(key string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs, err := torbox.NewClient(key).Hosters(ctx)
	if err != nil {
		log.Printf("TorBox hoster list unavailable (%v); hoster routing degraded", err)
		return nil
	}
	set := map[string]bool{}
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "www.")))
		if d != "" {
			set[d] = true
		}
	}
	for _, h := range hs {
		add(h.Domain)
		for _, d := range h.Domains {
			add(d)
		}
	}
	return set
}

func (a *App) put(t *core.Task) {
	a.mu.Lock()
	if t.ID == "" {
		t.ID = a.freshIDLocked()
	}
	a.tasks[t.ID] = t
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

func (a *App) onUpdate(id string, u core.Update) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	if u.Name != "" {
		t.Name = u.Name
	}
	if u.Size > 0 {
		t.Size = u.Size
	}
	if u.Status != "" {
		t.Status = u.Status
	}
	if u.Loaded > 0 {
		t.Loaded = u.Loaded
	}
	t.Speed = u.Speed
	if u.Err != "" {
		t.Error = u.Err
	}
	// Terminal states free the scheduling slot for the next queued task.
	if u.Status == core.StatusDone || u.Status == core.StatusError {
		delete(a.active, id)
		a.dispatchLocked()
	}
	if u.Status == core.StatusDone {
		t.Online = core.AvailOnline
		t.Retries = 0
		t.NextTry = time.Time{}
		if a.Settings.Get().VerifyChecksums {
			go a.verifyTask(id, filepath.Join(a.dirFor(t), t.Name))
		}
	}
	// A backend that says the link is not its business hands the task to the
	// next one in the chain instead of failing it. This is deliberately not a
	// guess about the error text: only an explicit signal advances the chain,
	// so a hoster link that genuinely failed is never re-downloaded as a plain
	// web page. The chain only moves downwards, so it terminates.
	var fallbackTo backend
	if u.Status == core.StatusError && u.Unsupported {
		if next := a.nextResolverLocked(t); next != "" {
			fallbackTo = a.backendFor(t.Resolver)
			log.Printf("task %s: %s could not fetch the link, trying %s", id, t.Resolver, next)
			t.Resolver = next
			t.Status = core.StatusQueued
			t.Error = ""
			t.Loaded = 0
			t.Speed = 0
			delete(a.started, id)
			a.queue = append(a.queue, id)
			a.dispatchLocked()
		}
	}

	// A failure is not automatically the end: hosters throttle, connections
	// drop. Retry a bounded number of times with a growing delay before the
	// task is left for the user to deal with.
	var retryIn time.Duration
	if u.Status == core.StatusError && fallbackTo == nil {
		if cfg := a.Settings.Get(); t.Retries < cfg.MaxRetries {
			t.Retries++
			retryIn = u.Retry
			if retryIn <= 0 {
				retryIn = retryDelay(t.Retries)
			}
			t.NextTry = time.Now().Add(retryIn)
		} else {
			t.NextTry = time.Time{}
		}
	}
	// A finished download that completes an archive continues as an extraction
	// (local backends only; JD downloads live on the JD box). For a multi-volume
	// set this only fires once the last part has arrived.
	var extractCopy *core.Task
	if u.Status == core.StatusDone && t.Resolver != "jd" && a.Settings.Get().Extract {
		if target, path := a.extractCandidateLocked(t); target != nil {
			target.Status = core.StatusExtracting
			go a.extractTask(target.ID, path, a.passwordsFor(target))
			if target != t {
				c := *target
				extractCopy = &c
			}
		}
	}
	c := *t
	a.mu.Unlock()
	if fallbackTo != nil {
		// Clear the old backend's state so a later restart does not resume a
		// download that belongs to a backend the task no longer uses.
		fallbackTo.Remove(id, true)
	}
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
	if extractCopy != nil {
		_ = a.Store.Save(extractCopy)
		a.Hub.Broadcast("task", extractCopy)
	}
	if retryIn > 0 {
		a.retryAfter(id, retryIn)
	}
}

// retryDelay grows with each attempt so a hoster cool-down has time to pass,
// without making the last attempt feel abandoned.
func retryDelay(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * 15 * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// retryAfter re-runs a failed task once the delay has passed, unless the user
// touched it in the meantime.
func (a *App) retryAfter(id string, d time.Duration) {
	time.AfterFunc(d, func() {
		a.mu.Lock()
		t := a.tasks[id]
		due := t != nil && t.Status == core.StatusError && !t.NextTry.IsZero()
		a.mu.Unlock()
		if due {
			a.RestartTasks([]string{id})
		}
	})
}

// extractCandidateLocked decides whether a just-finished download completes an
// archive that can now be unpacked, and returns the task to unpack. For a
// multi-volume set that is the moment the LAST part arrives — and what gets
// unpacked is the first volume, not necessarily the part that finished last.
// Caller holds a.mu.
func (a *App) extractCandidateLocked(done *core.Task) (*core.Task, string) {
	key, isVolume := extract.SetKey(done.Name)
	if !isVolume {
		if extract.Supported(done.Name) {
			return done, filepath.Join(a.dirFor(done), done.Name)
		}
		return nil, ""
	}
	dir := a.dirFor(done)
	var first *core.Task
	for _, t := range a.tasks {
		k, ok := extract.SetKey(t.Name)
		if !ok || k != key || a.dirFor(t) != dir {
			continue
		}
		if t.Status != core.StatusDone {
			// A part is still missing (or already extracting, which means
			// another part got here first). Whoever finishes last triggers it.
			return nil, ""
		}
		if extract.Supported(t.Name) && (first == nil || t.Name < first.Name) {
			first = t
		}
	}
	if first == nil {
		return nil, "" // parts without a first volume: nothing to open
	}
	return first, filepath.Join(dir, first.Name)
}

// verifyTask checks a finished file against a checksum, when one is available:
// a hash in the file name, or a sums file that was downloaded alongside it. A
// download nobody can verify is left unmarked rather than shown as passing,
// because a green tick that means "not checked" is worse than no tick.
func (a *App) verifyTask(id, path string) {
	name := filepath.Base(path)
	dir := filepath.Dir(path)

	var sum checksum.Sum
	if s, ok := checksum.FromName(name); ok {
		sum = s
	} else if s, ok := a.sumFromSiblingFile(dir, name); ok {
		sum = s
	} else {
		return
	}

	ok, err := checksum.Verify(path, sum)
	verdict := "ok"
	if err != nil {
		log.Printf("checksum %s: %v", name, err)
		return
	}
	if !ok {
		verdict = "failed"
		log.Printf("checksum mismatch for %s", name)
	}

	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Checksum = verdict
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// sumFromSiblingFile looks for a checksum listing that arrived with the batch
// and pulls this file's entry out of it.
func (a *App) sumFromSiblingFile(dir, name string) (checksum.Sum, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return checksum.Sum{}, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var parse func(io.Reader) ([]checksum.Sum, error)
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".sfv":
			parse = checksum.ParseSFV
		case ".md5", ".sha1", ".sha256", ".sha256sum", ".md5sum":
			parse = checksum.ParseHashFile
		default:
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sums, err := parse(f)
		f.Close()
		if err != nil {
			// Both parsers are strict: one malformed line yields nothing. Left
			// silent, that is indistinguishable from "no checksum file here",
			// and every download in the batch would quietly show as unverified.
			log.Printf("checksum file %s is unusable: %v", e.Name(), err)
			continue
		}
		for _, s := range sums {
			if strings.EqualFold(filepath.Base(s.Name), name) {
				return s, true
			}
		}
	}
	return checksum.Sum{}, false
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

// onWatchJob stages what a dropped file asked for. It runs on the watcher's
// goroutine, so it hands the slow part off and returns quickly.
func (a *App) onWatchJob(j watch.Job) {
	go func() {
		created := a.AddLinks(j.URLs, j.Package)
		if len(created) == 0 {
			return
		}
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		if j.Dir != "" || j.Password != "" {
			opts := TaskOptions{}
			if j.Dir != "" {
				opts.Dir = &j.Dir
			}
			if j.Password != "" {
				opts.Password = &j.Password
			}
			if err := a.SetTaskOptions(ids, opts); err != nil {
				log.Printf("dropped job: %v", err)
			}
		}
		// AutoStart on the job wins over the global setting, which has already
		// started them if it was on.
		if j.AutoStart && !a.Settings.Get().AutoStart {
			a.StartTasks(ids)
		}
	}()
}

// extractTask unpacks a finished archive download and settles the task back to
// done — extraction failures are recorded on the task but don't undo the
// completed download.
// passwordsFor is the order archive passwords are tried in: the task's own
// first, because it was set for exactly this file, then the global list.
func (a *App) passwordsFor(t *core.Task) []string {
	var out []string
	if t != nil && t.Password != "" {
		out = append(out, t.Password)
	}
	return append(out, a.Settings.Get().ArchivePasswords...)
}

func (a *App) extractTask(id, path string, passwords []string) {
	res, err := extract.ExtractWith(path, passwords)
	if err == nil && a.Settings.Get().DeleteArchive {
		for _, v := range res.Volumes {
			_ = os.Remove(v)
		}
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Status = core.StatusDone
	if err != nil {
		t.Error = "extract: " + err.Error()
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
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
