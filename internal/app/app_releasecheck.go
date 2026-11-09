package app

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/update"
)

// A copy that cannot replace itself, a container or a Flatpak, asks GitHub
// once a day for a newer release while AutoUpdateCheck is on. What it found is
// kept in the data folder, so a restart neither forgets it nor asks again
// before the day is over. The first run waits a few minutes, as the yt-dlp
// update does, so a container caught in a restart loop does not ask on every
// attempt, and a check that could not reach GitHub is tried again within the
// hour.
const (
	releaseCheckFirstRun = 5 * time.Minute
	releaseCheckEvery    = 24 * time.Hour
	releaseCheckRetry    = time.Hour
	releaseCheckFile     = "release-check.json"
)

// releaseCheckState is embedded on App so its fields stay in this file.
type releaseCheckState struct {
	// releaseVersion is the running version and checkRelease asks GitHub
	// about it; the tests replace both.
	releaseVersion string
	checkRelease   func(ctx context.Context) update.Info
	// releaseNotices is set by StartReleaseCheck. The desktop build that
	// updates itself has its own note and leaves it off.
	releaseNotices atomic.Bool

	releaseMu     sync.Mutex
	releaseLoaded bool
	release       releaseRecord
}

// releaseRecord is the last check that reached GitHub, as it is kept on disk.
// Latest is empty when that check found nothing newer.
type releaseRecord struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest,omitempty"`
	URL       string    `json:"url,omitempty"`
}

// ReleaseNotice is a newer release the daily check found.
type ReleaseNotice struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// StartReleaseCheck starts the daily release check. The server binary and the
// Flatpak call it rather than New, which every test calls. Close stops it.
func (a *App) StartReleaseCheck() {
	a.releaseNotices.Store(true)
	first := max(releaseCheckFirstRun, a.releaseDue(releaseCheckEvery))
	a.spawn(func() { a.releaseCheckLoop(first, releaseCheckEvery, releaseCheckRetry) })
}

// releaseCheckLoop reads the switch on every run, so turning it on takes
// effect within retry and without a restart. A check made from the Info page
// in between counts as the day's.
func (a *App) releaseCheckLoop(first, every, retry time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		wait := retry
		if a.Settings.Get().AutoUpdateCheck {
			if due := a.releaseDue(every); due > 0 {
				wait = due
			} else if a.CheckRelease(a.ctx).Checked {
				wait = every
			}
		}
		timer.Reset(wait)
	}
}

// CheckRelease asks GitHub whether a newer release is out and keeps the answer
// when GitHub could be asked.
func (a *App) CheckRelease(ctx context.Context) update.Info {
	info := a.checkRelease(ctx)
	if !info.Checked {
		return info
	}
	rec := releaseRecord{CheckedAt: time.Now()}
	if info.Available {
		rec.Latest, rec.URL = info.Latest, info.URL
	}
	a.releaseMu.Lock()
	defer a.releaseMu.Unlock()
	a.releaseLoaded, a.release = true, rec
	if b, err := json.Marshal(rec); err == nil {
		// A file torn by a crash reads as no check at all, which costs one
		// question more.
		if err := os.WriteFile(filepath.Join(a.DataDir, releaseCheckFile), b, 0o600); err != nil {
			log.Printf("update: could not keep the release check's answer: %v", err)
		}
	}
	return info
}

// ReleaseNotice is the newer release the last check found, or nil when there
// is none, the switch is off or this copy runs no daily check.
func (a *App) ReleaseNotice() *ReleaseNotice {
	if !a.releaseNotices.Load() || !a.Settings.Get().AutoUpdateCheck {
		return nil
	}
	rec := a.lastRelease()
	if !update.Newer(rec.Latest, a.releaseVersion) {
		return nil
	}
	return &ReleaseNotice{Version: rec.Latest, URL: rec.URL}
}

// releaseDue is how long until the next check is due, zero or less when it is.
func (a *App) releaseDue(every time.Duration) time.Duration {
	rec := a.lastRelease()
	if rec.CheckedAt.IsZero() {
		return 0
	}
	return time.Until(rec.CheckedAt.Add(every))
}

// lastRelease is the last check that reached GitHub, read from the data folder
// the first time it is asked for.
func (a *App) lastRelease() releaseRecord {
	a.releaseMu.Lock()
	defer a.releaseMu.Unlock()
	if !a.releaseLoaded {
		a.releaseLoaded = true
		var rec releaseRecord
		if b, err := os.ReadFile(filepath.Join(a.DataDir, releaseCheckFile)); err == nil && json.Unmarshal(b, &rec) == nil {
			a.release = rec
		}
	}
	return a.release
}
