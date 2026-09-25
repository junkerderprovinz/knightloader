package torbox

import (
	"context"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend TorBox hands the resolved CDN URL to.
type Downloader interface {
	// Handover starts the transfer of url. relink asks TorBox for a fresh url
	// to the same file, for what is left when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// Backend unlocks a hoster link via TorBox, then delegates the actual download
// to the engine. It mirrors TorBox's own prepare phase into the task, then the
// engine's progress takes over once the CDN URL is ready.
type Backend struct {
	c   *Client
	eng Downloader

	onUpdate func(taskID string, u core.Update)

	// Created is told the job id of every web download this backend starts,
	// as Torrents names it, so the import from the account knows the download
	// for one of its own.
	Created func(job string)

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string // original hoster link, for resume
	handed map[string]bool   // true once the engine owns the transfer
	jobID  map[string]int64  // TorBox web-download id, for cleanup on Remove
	// conns is the dispatcher's connection count for each task, kept for the
	// handover after the prepare phase and after a Resume.
	conns map[string]int
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		c: c, eng: eng, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		link:   map[string]string{},
		handed: map[string]bool{},
		jobID:  map[string]int64{},
		conns:  map[string]int{},
	}
}

func (b *Backend) Download(taskID, link string, _ map[string]string, conns int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.handed[taskID] = false
	b.conns[taskID] = conns
	b.mu.Unlock()
	b.start(taskID, link)
}

// start begins an unlock. Its cancel stays in b.cancel until the next start or
// a Remove replaces it: a run that deleted its own entry on the way out could
// delete the one a Resume had just put there, and the next Pause would not
// reach that unlock.
func (b *Backend) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link)
}

func (b *Backend) run(ctx context.Context, taskID, link string) {
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: "unlocking via TorBox…"})
	id, err := b.c.CreateWebDownload(ctx, link)
	if err != nil {
		b.fail(ctx, taskID, err)
		return
	}
	if b.Created != nil {
		b.Created(webJob(id))
	}
	b.mu.Lock()
	_, live := b.link[taskID]
	if live {
		b.jobID[taskID] = id
	}
	b.mu.Unlock()
	if !live {
		// Removed while TorBox was creating the job, so Remove had no job to
		// delete.
		b.deleteJob(id)
		return
	}

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
		b.fail(ctx, taskID, err)
		return
	}
	name := f.Name
	if name == "" {
		name = ready.Name
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: f.Size})

	// Hand the direct CDN URL to the engine; its progress now drives the task.
	// Pause and Remove cancel under b.mu, so one that came after TorBox
	// answered is seen here and the link is not handed on.
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.handed[taskID] = true
	conns := b.conns[taskID]
	b.mu.Unlock()
	b.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		return b.c.RequestDL(ctx, id, f.ID)
	})
}

// fail reports err on the task, unless the run was cancelled: Pause and Remove
// cancel it and set the task's state themselves.
func (b *Backend) fail(ctx context.Context, taskID string, err error) {
	if ctx.Err() != nil {
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Speed: 0, Err: "torbox: " + err.Error(), HostDown: SiteDisabled(err)})
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	if cancel := b.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
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

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	handed := b.handed[taskID]
	job := b.jobID[taskID]
	delete(b.cancel, taskID)
	delete(b.link, taskID)
	delete(b.handed, taskID)
	delete(b.jobID, taskID)
	delete(b.conns, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID, deleteFiles)
	}
	if job != 0 {
		b.deleteJob(job)
	}
}

// deleteJob drops a web download from the TorBox account, best effort and off
// the caller's goroutine.
func (b *Backend) deleteJob(job int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = b.c.Delete(ctx, job)
	}()
}
