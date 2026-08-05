package jd

import (
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Backend performs delegated downloads through headless JD and mirrors the live
// progress into KnightLoader tasks via onUpdate. It satisfies the same contract
// as the Gopeed engine (Download/Pause/Resume/Remove), so the app treats both
// backends the same way.
type Backend struct {
	c        *Client
	onUpdate func(taskID string, u core.Update)

	mu   sync.Mutex
	stop map[string]chan struct{} // taskID -> poll stopper
}

func NewBackend(base string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		c:        NewClient(base),
		onUpdate: onUpdate,
		stop:     map[string]chan struct{}{},
	}
}

// Reachable reports whether the configured JD instance answers.
func (b *Backend) Reachable() error { return b.c.Ping() }

func (b *Backend) pkgName(taskID string) string { return "KL-" + taskID }

// Download hands the link to JD (auto-crawl + start) and polls its progress.
func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	go func() {
		if _, err := b.c.AddLinks(url, b.pkgName(taskID), true); err != nil {
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + err.Error()})
			return
		}
		b.poll(taskID)
	}()
}

func (b *Backend) poll(taskID string) {
	stop := make(chan struct{})
	b.mu.Lock()
	b.stop[taskID] = stop
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.stop, taskID)
		b.mu.Unlock()
	}()

	pkg := b.pkgName(taskID)
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(30 * time.Minute)

	for {
		select {
		case <-stop:
			return
		case <-deadline:
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: timeout"})
			return
		case <-ticker.C:
			puuid, err := b.c.PackageUUID(pkg)
			if err != nil || puuid == 0 {
				continue // still crawling / not in the download list yet
			}
			links, err := b.c.QueryDownloads(puuid)
			if err != nil || len(links) == 0 {
				continue
			}
			link := &links[0]
			u := core.Update{
				Name:   link.Name,
				Size:   link.BytesTotal,
				Loaded: link.BytesLoaded,
				Speed:  link.Speed,
				Status: core.StatusRunning,
			}
			if link.Finished || link.Status == "Finished" {
				u.Status = core.StatusDone
				u.Speed = 0
				b.onUpdate(taskID, u)
				return
			}
			b.onUpdate(taskID, u)
		}
	}
}

func (b *Backend) Pause(taskID string)  { b.setEnabled(taskID, false) }
func (b *Backend) Resume(taskID string) { b.setEnabled(taskID, true) }

func (b *Backend) setEnabled(taskID string, enabled bool) {
	ids := b.linkIDs(taskID)
	if len(ids) > 0 {
		_ = b.c.SetEnabled(enabled, ids)
	}
}

func (b *Backend) Remove(taskID string, _ bool) {
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		close(s)
		delete(b.stop, taskID)
	}
	b.mu.Unlock()
	if puuid, err := b.c.PackageUUID(b.pkgName(taskID)); err == nil && puuid != 0 {
		_ = b.c.RemoveLinks(nil, []int64{puuid})
	}
}

func (b *Backend) linkIDs(taskID string) []int64 {
	puuid, err := b.c.PackageUUID(b.pkgName(taskID))
	if err != nil || puuid == 0 {
		return nil
	}
	links, err := b.c.QueryDownloads(puuid)
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.UUID)
	}
	return ids
}
