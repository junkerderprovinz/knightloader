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

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
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

	jd    backend // headless-JD backend, nil unless KL_JD is set and reachable
	ytdlp backend // yt-dlp media backend, nil unless the yt-dlp binary is present

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

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-file links) route through it.
	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	if yb := ytdlp.NewBackend(ytbin, filepath.Join(dataDir, "downloads"), a.onUpdate); yb.Available() {
		a.ytdlp = yb
		a.Registry.Register(ytdlp.Resolver{})
		log.Printf("yt-dlp backend enabled: %s", ytbin)
	}

	// Optional headless-JD backend: when KL_JD points at a reachable JD
	// Deprecated API, links route through JD's crawler and hoster plugins.
	if base := os.Getenv("KL_JD"); base != "" {
		jb := jd.NewBackend(base, a.onUpdate)
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); using direct downloads only", err)
		} else {
			a.jd = jb
			a.Registry.Register(jd.Resolver{})
			log.Printf("headless JD backend enabled: %s", base)
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
	case resolverID == "ytdlp" && a.ytdlp != nil:
		return a.ytdlp
	default:
		return a.Engine
	}
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
