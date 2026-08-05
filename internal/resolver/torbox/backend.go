package torbox

import (
	"context"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend TorBox hands the resolved CDN URL to
// (the embedded engine satisfies it).
type Downloader interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string)
}

// Backend unlocks a hoster link via TorBox, then delegates the actual download
// to the engine. It mirrors TorBox's own prepare phase into the task, then the
// engine's progress takes over once the CDN URL is ready.
type Backend struct {
	c   *Client
	eng Downloader

	onUpdate func(taskID string, u core.Update)

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string // original hoster link, for resume
	handed map[string]bool   // true once the engine owns the transfer
	jobID  map[string]int64  // TorBox web-download id, for cleanup on Remove
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		c: c, eng: eng, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		link:   map[string]string{},
		handed: map[string]bool{},
		jobID:  map[string]int64{},
	}
}

func (b *Backend) Download(taskID, link string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.handed[taskID] = false
	b.mu.Unlock()
	b.start(taskID, link)
}

func (b *Backend) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link)
}

func (b *Backend) run(ctx context.Context, taskID, link string) {
	defer func() {
		b.mu.Lock()
		delete(b.cancel, taskID)
		b.mu.Unlock()
	}()

	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: "unlocking via TorBox…"})
	id, err := b.c.CreateWebDownload(ctx, link)
	if err != nil {
		b.fail(taskID, err)
		return
	}
	b.mu.Lock()
	b.jobID[taskID] = id
	b.mu.Unlock()

	// Poll until TorBox has the file on its CDN, mirroring its own fetch phase.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var ready *WebDownload
	for ready == nil {
		wd, err := b.c.Get(ctx, id)
		if err == nil && wd != nil {
			if wd.DownloadPresent && len(wd.Files) > 0 {
				ready = wd
				break
			}
			size := wd.Size
			b.onUpdate(taskID, core.Update{
				Status: core.StatusRunning,
				Name:   wd.Name,
				Size:   size,
				Loaded: int64(wd.Progress * float64(size)),
				Speed:  wd.DownloadSpeed,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}

	f := ready.Files[0]
	direct, err := b.c.RequestDL(ctx, id, f.ID)
	if err != nil {
		b.fail(taskID, err)
		return
	}
	name := f.Name
	if name == "" {
		name = ready.Name
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: f.Size})

	// Hand the direct CDN URL to the engine; its progress now drives the task.
	b.mu.Lock()
	b.handed[taskID] = true
	b.mu.Unlock()
	b.eng.Download(taskID, direct, nil, 8)
}

func (b *Backend) fail(taskID string, err error) {
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Speed: 0, Err: "torbox: " + err.Error()})
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	cancel := b.cancel[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
	}
	if cancel != nil {
		cancel()
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused, Speed: 0})
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	link := b.link[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link)
	}
}

func (b *Backend) Remove(taskID string) {
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	handed := b.handed[taskID]
	job := b.jobID[taskID]
	delete(b.link, taskID)
	delete(b.handed, taskID)
	delete(b.jobID, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID)
	}
	if job != 0 {
		// Best-effort: also drop the job from the TorBox account.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = b.c.Delete(ctx, job)
		}()
	}
}
