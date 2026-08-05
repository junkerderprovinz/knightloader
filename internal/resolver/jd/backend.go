package jd

import (
	"fmt"
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

	// Two separate patiences, because they answer two different questions.
	// appearBy asks whether JD ever accepted the link at all — crawling and
	// captchas take minutes, not hours. stall asks whether a download that did
	// start has stopped moving. A single wall-clock deadline conflated the two
	// and killed healthy multi-hour downloads at the thirty-minute mark.
	appearBy := time.Now().Add(appearLimit)
	var seen bool
	var lastBytes int64
	lastMoved := time.Now()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !seen && time.Now().After(appearBy) {
				b.onUpdate(taskID, core.Update{
					Status: core.StatusError,
					Err:    "jd: the link never reached JDownloader's download list",
				})
				return
			}
			if seen && time.Since(lastMoved) > stallLimit {
				b.onUpdate(taskID, core.Update{
					Status: core.StatusError,
					Err:    "jd: no progress for " + stallLimit.String(),
				})
				return
			}

			puuid, err := b.c.PackageUUID(pkg)
			if err != nil || puuid == 0 {
				continue // still crawling / not in the download list yet
			}
			links, err := b.c.QueryDownloads(puuid)
			if err != nil || len(links) == 0 {
				continue
			}
			seen = true

			// JD may have crawled one link into several files. Reporting only
			// the first would show a fraction of the real size and call the
			// task done while the rest is still downloading, so the whole
			// package is summed instead.
			u := aggregate(links)
			if u.Loaded != lastBytes {
				lastBytes = u.Loaded
				lastMoved = time.Now()
			}
			b.onUpdate(taskID, u)
			if u.Status == core.StatusDone {
				return
			}
		}
	}
}

// appearLimit is how long JD gets to turn a submitted link into a download.
// Crawling, container decryption and a captcha all happen in here.
const appearLimit = 15 * time.Minute

// stallLimit is how long a started download may make no progress before it is
// given up on. Generous on purpose: a hoster cool-down is minutes, and JD
// handles its own waiting.
const stallLimit = 45 * time.Minute

// aggregate folds every file JD produced for one link into a single update, so
// the size and progress shown are the package's, not the first file's.
func aggregate(links []DownloadLink) core.Update {
	u := core.Update{Status: core.StatusRunning, Name: links[0].Name}
	done := 0
	for i := range links {
		u.Size += links[i].BytesTotal
		u.Loaded += links[i].BytesLoaded
		u.Speed += links[i].Speed
		if links[i].Finished || links[i].Status == "Finished" {
			done++
		}
	}
	if len(links) > 1 {
		u.Name = fmt.Sprintf("%s (+%d)", links[0].Name, len(links)-1)
	}
	if done == len(links) {
		u.Status = core.StatusDone
		u.Speed = 0
	}
	return u
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
