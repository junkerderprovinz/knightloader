// Package app wires the store, engine, resolver registry and WebSocket hub into
// one coordinator. It owns task state; the engine reports changes, the app
// persists them and broadcasts them to every UI.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

type App struct {
	Store    *store.Store
	Engine   *engine.Engine
	Hub      *hub.Hub
	Registry *resolver.Registry

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

	eng, err := engine.New(filepath.Join(dataDir, "downloads"), a.onEngineUpdate)
	if err != nil {
		st.Close()
		return nil, err
	}
	a.Engine = eng

	// Reload persisted tasks. Anything that was mid-flight is shown as paused on
	// boot (cross-restart auto-resume is a later refinement).
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

// AddLinks resolves each URL and enqueues a download. Unsupported URLs are skipped.
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
		a.Engine.Download(t.ID, result.DirectURL, result.Headers, conns)
		created = append(created, t)
	}
	return created
}

func (a *App) Pause(id string)  { a.Engine.Pause(id) }
func (a *App) Resume(id string) { a.Engine.Resume(id) }

func (a *App) Remove(id string) {
	a.Engine.Remove(id)
	a.mu.Lock()
	delete(a.tasks, id)
	a.mu.Unlock()
	_ = a.Store.Delete(id)
	a.Hub.Broadcast("removed", map[string]string{"id": id})
}

func (a *App) put(t *core.Task) {
	a.mu.Lock()
	a.tasks[t.ID] = t
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

func (a *App) onEngineUpdate(id string, u engine.Update) {
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
