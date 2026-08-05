// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; a download backend (the Gopeed engine or
// headless JD) reports changes, the app persists them and broadcasts them.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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
	Store    *store.Store
	Engine   *engine.Engine
	Hub      *hub.Hub
	Registry *resolver.Registry
	Accounts *accounts.Store

	jd     backend // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp  backend // yt-dlp media backend, nil unless the yt-dlp binary is present
	torbox backend // TorBox debrid backend, nil unless a TorBox key is present

	mu    sync.Mutex
	tasks map[string]*core.Task
}

func New(dataDir string) (*App, error) {
	st, err := store.Open(filepath.Join(dataDir, "knightloader.db"))
	if err != nil {
		return nil, err
	}
	a := &App{
		Store:    st,
		Hub:      hub.New(),
		Registry: resolver.NewRegistry(),
		tasks:    map[string]*core.Task{},
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
	if yb := ytdlp.NewBackend(ytbin, filepath.Join(dataDir, "downloads"), a.onUpdate); yb.Available() {
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

	// Reload persisted tasks; anything mid-flight shows as paused on boot.
	existing, err := st.All()
	if err != nil {
		return nil, err
	}
	for _, t := range existing {
		if t.Status == core.StatusRunning || t.Status == core.StatusQueued {
			t.Status = core.StatusPaused
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

// AddLinks resolves each URL and enqueues a download on the matching backend.
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
			Status:    core.StatusQueued,
			CreatedAt: time.Now(),
		}
		a.put(t)
		conns := result.Connections
		if conns <= 0 {
			conns = 4
		}
		a.backendFor(res.Info().ID).Download(t.ID, result.DirectURL, result.Headers, conns)
		created = append(created, t)
	}
	return created
}

func (a *App) Pause(id string)  { a.backendFor(a.resolverOf(id)).Pause(id) }
func (a *App) Resume(id string) { a.backendFor(a.resolverOf(id)).Resume(id) }

func (a *App) Remove(id string) {
	a.backendFor(a.resolverOf(id)).Remove(id)
	a.mu.Lock()
	delete(a.tasks, id)
	a.mu.Unlock()
	_ = a.Store.Delete(id)
	a.Hub.Broadcast("removed", map[string]string{"id": id})
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

func (a *App) resolverOf(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.tasks[id]; t != nil {
		return t.Resolver
	}
	return ""
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
