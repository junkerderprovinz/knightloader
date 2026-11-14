package app

// The components the app keeps current: yt-dlp, ffmpeg, Deno, the desktop
// build's Java and JDownloader. One switch lets the app update all of them on
// its own, and the Components card's Update now runs the same updates at once.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// The components by the names the card and the check log use.
const (
	componentYtdlp  = "yt-dlp"
	componentFFmpeg = "ffmpeg"
	componentDeno   = "deno"
	componentJava   = "java"
	componentJD     = "jdownloader"
)

// What one component's update came to.
const (
	ComponentChecking = "checking"
	ComponentUpdated  = "updated"
	ComponentCurrent  = "current"
	// ComponentWaits is an update found that waits for JD: until none of
	// its downloads runs, or until it can take the new Java.
	ComponentWaits = "waits"
	// ComponentSkipped is a component this app does not update here, such
	// as an ffmpeg on PATH or the container's own.
	ComponentSkipped = "skipped"
	ComponentFailed  = "failed"
)

// yt-dlp has to keep up with sites that change their pages, the others move
// slower. ffmpeg's rolling build changes every night, so a weekly ask fetches a
// new one most weeks and no more often.
const (
	ytdlpCheckEvery     = 24 * time.Hour
	componentCheckEvery = 7 * 24 * time.Hour
	// componentTick is how often the update loops look whether a component
	// is due, so an update held back by a JD download is tried again soon.
	componentTick = time.Hour
)

func checkEvery(id string) time.Duration {
	if id == componentYtdlp {
		return ytdlpCheckEvery
	}
	return componentCheckEvery
}

// ComponentResult is what one component's update came to, as the card shows
// it. Detail is a fact in English, a path or an error, as in mediatools.Tool.
type ComponentResult struct {
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
	// again has the automatic update ask at its next look rather than once
	// the component is due, for an update held back or a JD not there yet.
	again bool
}

// ComponentUpdate is how far Update now has come.
type ComponentUpdate struct {
	Running bool                       `json:"running"`
	Results map[string]ComponentResult `json:"results,omitempty"`
}

// componentsState is embedded on App so its fields stay in this file.
type componentsState struct {
	checksOnce sync.Once
	checks     *checkLog

	runMu sync.Mutex
	run   ComponentUpdate
}

// checkLog remembers when each component was last asked after, in a file
// beside the fetched tools, so a restart does not ask again before one is due.
// A file that cannot be written only means asking again after a restart.
type checkLog struct {
	mu   sync.Mutex
	path string
	at   map[string]time.Time
}

func (a *App) componentChecks() *checkLog {
	a.checksOnce.Do(func() {
		l := &checkLog{at: map[string]time.Time{}}
		if a.DataDir != "" {
			l.path = filepath.Join(mediatools.ToolsDir(a.DataDir), "checks.json")
			if b, err := os.ReadFile(l.path); err == nil {
				_ = json.Unmarshal(b, &l.at)
			}
		}
		a.checks = l
	})
	return a.checks
}

func (l *checkLog) due(id string, every time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Since(l.at[id]) >= every
}

func (l *checkLog) mark(id string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.at[id] = at
	if l.path == "" {
		return
	}
	b, err := json.MarshalIndent(l.at, "", "  ")
	if err == nil && os.MkdirAll(filepath.Dir(l.path), 0o755) == nil {
		_ = os.WriteFile(l.path, append(b, '\n'), 0o644)
	}
}

// componentDue reports whether the automatic update should ask after id now.
// A missing tool is due at every look until it is there.
func (a *App) componentDue(id string, missing bool) bool {
	return a.Settings.Get().ComponentsAutoUpdate && (missing || a.componentChecks().due(id, checkEvery(id)))
}

// asked records an ask after id, unless it is to be asked again at the next
// look.
func (a *App) asked(id string, r ComponentResult) {
	if !r.again {
		a.componentChecks().mark(id, time.Now())
	}
}

// tickLoop runs run after first and then every every, until Close. run reads
// the switch itself, so turning it on or off takes effect without a restart.
func (a *App) tickLoop(first, every time.Duration, run func()) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		run()
		timer.Reset(every)
	}
}

// StartComponentUpdate starts Update now, which asks after every component at
// once whatever the switch and the cadence say, and fetches what is newer. A
// run already under way is left to finish. ComponentUpdateState follows it.
func (a *App) StartComponentUpdate() ComponentUpdate {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.run.Running {
		return a.copyRunLocked()
	}
	type step struct {
		id  string
		run func(ctx context.Context) ComponentResult
	}
	media := []step{
		{componentYtdlp, func(ctx context.Context) ComponentResult { return a.updateYtdlpComponent(ctx, true) }},
		{componentFFmpeg, a.updateFFmpegComponent},
		{componentDeno, a.updateDenoComponent},
	}
	// JD restarts for its own update and for a new Java, so the two go one
	// after the other.
	jd := []step{{componentJD, a.updateJDComponent}}
	if a.SwitchJava != nil {
		jd = append(jd, step{componentJava, a.updateJavaComponent})
	}
	a.run = ComponentUpdate{Running: true, Results: map[string]ComponentResult{}}
	for _, s := range append(media, jd...) {
		a.run.Results[s.id] = ComponentResult{State: ComponentChecking}
	}
	do := func(s step) {
		r := s.run(a.ctx)
		a.asked(s.id, r)
		a.runMu.Lock()
		a.run.Results[s.id] = r
		a.runMu.Unlock()
	}
	var wg sync.WaitGroup
	for _, s := range media {
		wg.Add(1)
		a.spawn(func() {
			defer wg.Done()
			do(s)
		})
	}
	wg.Add(1)
	a.spawn(func() {
		defer wg.Done()
		for _, s := range jd {
			do(s)
		}
	})
	a.spawn(func() {
		wg.Wait()
		a.runMu.Lock()
		a.run.Running = false
		a.runMu.Unlock()
	})
	return a.copyRunLocked()
}

// ComponentUpdateState is how far the last Update now has come.
func (a *App) ComponentUpdateState() ComponentUpdate {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.copyRunLocked()
}

func (a *App) copyRunLocked() ComponentUpdate {
	out := ComponentUpdate{Running: a.run.Running}
	if a.run.Results != nil {
		out.Results = make(map[string]ComponentResult, len(a.run.Results))
		for id, r := range a.run.Results {
			out.Results[id] = r
		}
	}
	return out
}
