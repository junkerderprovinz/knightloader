// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; a download backend (the Gopeed engine or
// headless JD) reports changes, the app persists them and broadcasts them.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// backend is a download backend: the embedded Gopeed engine or headless JD.
// Both report progress through the app's onUpdate callback.
type backend interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string)
}

type App struct {
	Store      *store.Store
	Engine     *engine.Engine
	Hub        *hub.Hub
	Registry   *resolver.Registry
	Accounts   *accounts.Store
	Settings   *settings.Store
	Federation *federation.Manager

	jd     backend // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp  backend // yt-dlp media backend, nil unless the yt-dlp binary is present
	torbox backend // TorBox debrid backend, nil unless a TorBox key is present

	dlDir string // where engine + yt-dlp downloads land (extraction source)

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
		tasks:      map[string]*core.Task{},
		active:     map[string]bool{},
		started:    map[string]bool{},
	}
	a.Registry.Register(resolver.Direct{})

	eng, err := engine.New(filepath.Join(dataDir, "downloads"), a.onUpdate)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Engine = eng

	acc, err := accounts.Open(dataDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Accounts = acc

	// Resolve which hoster backends are configured, then fetch TorBox's
	// supported-host list once so routing can tell file hosters (→ TorBox/JD)
	// from media pages (→ yt-dlp).
	torboxKey := os.Getenv("KL_TORBOX")
	if torboxKey == "" {
		torboxKey, _ = acc.Get("torbox")
	}
	jdBase := os.Getenv("KL_JD")

	var hosterSet map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = fetchTorboxHosters(torboxKey)
	}

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-hoster, non-file links) route through it.
	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		yb.RateLimit = func() int64 { return a.Settings.Get().SpeedLimit }
		a.ytdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: hosterSet})
		log.Printf("yt-dlp backend enabled: %s", ytbin)
	}

	// Optional TorBox debrid backend: when a key is present, supported hoster
	// links are unlocked into a direct CDN URL the engine then downloads.
	if torboxKey != "" {
		a.torbox = torbox.NewBackend(torbox.NewClient(torboxKey), eng, a.onUpdate)
		a.Registry.Register(torbox.Resolver{Hosts: hosterSet})
		log.Printf("TorBox debrid backend enabled (%d supported hosts)", len(hosterSet))
	}

	// Optional headless-JD backend: the lowest-priority catch-all for hoster
	// links nothing else claims, via JD's crawler and hoster plugins.
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			a.jd = jb
			a.Registry.Register(jd.Resolver{})
			log.Printf("headless JD backend enabled: %s", jdBase)
		}
	}

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

func (a *App) Close() error {
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
	var created []*core.Task
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		res := a.Registry.For(u)
		if res == nil {
			continue
		}
		result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
		if err != nil {
			continue
		}
		t := &core.Task{
			ID:        newID(),
			URL:       u,
			Name:      result.Name,
			Package:   pkg,
			Resolver:  res.Info().ID,
			Size:      result.Size,
			Status:    core.StatusCollected,
			CreatedAt: time.Now(),
		}
		a.put(t)
		// Lightweight analysis for plain file links: a HEAD gives size + an
		// online check while the task waits in the collector.
		if res.Info().ID == "direct" {
			go a.analyze(t.ID, result.DirectURL)
		}
		created = append(created, t)
	}
	return created
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
		a.onUpdate(id, core.Update{Err: "offline: " + err.Error()})
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		a.onUpdate(id, core.Update{Err: "offline (HTTP " + strconv.Itoa(resp.StatusCode) + ")"})
		return
	}
	if resp.ContentLength > 0 {
		a.onUpdate(id, core.Update{Size: resp.ContentLength})
	}
}

// dispatchLocked starts queued tasks while slots are free. FIFO with per-host
// skip-ahead: a host at its limit doesn't block other hosts behind it.
// Caller holds a.mu.
func (a *App) dispatchLocked() {
	cfg := a.Settings.Get()
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
		res := a.Registry.For(t.URL)
		if res == nil {
			t.Status = core.StatusError
			t.Error = "no resolver matches"
			continue
		}
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
		go a.backendFor(t.Resolver).Download(id, result.DirectURL, result.Headers, conns)
	}
	a.queue = rest
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

func (a *App) Remove(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	delete(a.tasks, id)
	delete(a.active, id)
	delete(a.started, id)
	a.dequeueLocked(id)
	a.dispatchLocked()
	a.mu.Unlock()
	if t != nil {
		a.backendFor(t.Resolver).Remove(id)
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

// SetAccount stores (or, with an empty secret, clears) a credential for a
// service such as "torbox". Applied on the next start.
func (a *App) SetAccount(service, secret string) error {
	return a.Accounts.Set(service, secret)
}

// AddLinksCnL satisfies the Click'n'Load listener's Adder interface.
func (a *App) AddLinksCnL(urls []string, pkg string) { a.AddLinks(urls, pkg) }

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
	if sl, ok := a.jd.(speedLimiter); ok {
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
	// A finished download that is an archive continues as an extraction (local
	// backends only; JD downloads live on the JD box).
	if u.Status == core.StatusDone && t.Resolver != "jd" &&
		a.Settings.Get().Extract && extract.Supported(t.Name) {
		t.Status = core.StatusExtracting
		go a.extractTask(id, filepath.Join(a.dlDir, t.Name))
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// extractTask unpacks a finished archive download and settles the task back to
// done — extraction failures are recorded on the task but don't undo the
// completed download.
func (a *App) extractTask(id, path string) {
	res, err := extract.Extract(path)
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
