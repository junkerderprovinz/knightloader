package app

// yt-dlp and ffmpeg as far as the App is concerned: what is installed, and the
// operations that replace it.

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// mediaToolsState is embedded on App so its fields stay in this file. The prober
// is built on first use; DataDir is already set by then.
type mediaToolsState struct {
	mediaToolsOnce sync.Once
	mediaTools     *mediatools.Prober
	// ytdlpMu keeps a fetch, a revert and the daily update off the same files
	// at the same time.
	ytdlpMu sync.Mutex
	// ffmpegMu does the same for the fetched ffmpeg.
	ffmpegMu sync.Mutex
	// latestYtdlp and installYtdlp are mediatools.CheckLatest and
	// mediatools.Install, which the tests replace so that nothing leaves the
	// machine.
	latestYtdlp   func(ctx context.Context, installed string) mediatools.Latest
	installYtdlp  func(ctx context.Context, dataDir string) (mediatools.ManagedRecord, error)
	installFFmpeg func(ctx context.Context, dataDir string) (mediatools.FFmpegRecord, error)

	// fetchingFFmpeg is set while an ffmpeg that was missing is on its way,
	// and awaitingFFmpeg holds the rows that failed for want of it meanwhile,
	// each with yt-dlp's sentence. Both are guarded by the App's mu.
	fetchingFFmpeg bool
	awaitingFFmpeg map[string]string

	// InstallMissingTools lets the daily update fetch yt-dlp and ffmpeg where
	// none is found, and makes its first run right at start in that case. The
	// desktop app sets it: a portable copy, and an installation that updated
	// itself, carry only the program and not the yt-dlp the installer puts
	// beside it, and no desktop copy brings ffmpeg.
	InstallMissingTools bool
}

func (a *App) mediaToolsProber() *mediatools.Prober {
	a.mediaToolsOnce.Do(func() { a.mediaTools = mediatools.NewProber(a.DataDir) })
	return a.mediaTools
}

// MediaTools is what the Resolvers settings page and the diagnostics bundle
// read. It stays off the network and is cached for a minute, so an open page
// neither calls out nor keeps spawning processes.
func (a *App) MediaTools() mediatools.Status {
	return a.mediaToolsProber().Read()
}

// YtdlpLatest asks GitHub for the newest yt-dlp release and compares it with the
// installed one. Besides the daily update it is the only call here that leaves
// the box, made on an explicit press or the opt-in check on page load.
func (a *App) YtdlpLatest(ctx context.Context) mediatools.Latest {
	return a.latestYtdlp(ctx, a.MediaTools().Ytdlp.Version)
}

// UpdateYtdlp fetches, verifies, smoke-tests and installs the newest yt-dlp,
// then puts it in force without a restart.
func (a *App) UpdateYtdlp(ctx context.Context) (mediatools.ManagedRecord, error) {
	a.ytdlpMu.Lock()
	defer a.ytdlpMu.Unlock()
	return a.updateYtdlpLocked(ctx)
}

// updateYtdlpLocked is UpdateYtdlp with ytdlpMu already held.
//
// Invalidate comes first so the next probe reads the new binary; rewireBackends
// then re-resolves the path and rebuilds the yt-dlp backend around it.
func (a *App) updateYtdlpLocked(ctx context.Context) (mediatools.ManagedRecord, error) {
	rec, err := a.installYtdlp(ctx, a.DataDir)
	if err != nil {
		return rec, err
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Printf("yt-dlp: fetched %s (%s) and put it in force", rec.Version, rec.Asset)
	return rec, nil
}

// The daily update waits a few minutes after start, so a start is not spent
// downloading and a container caught in a restart loop does not ask GitHub on
// every attempt.
const (
	ytdlpAutoFirstRun = 5 * time.Minute
	ytdlpAutoEvery    = 24 * time.Hour
)

// StartYtdlpAutoUpdate starts the daily yt-dlp update. The binaries call it
// rather than New, which every test calls. Close stops it and cancels a
// download in flight.
func (a *App) StartYtdlpAutoUpdate() {
	// Known before the first run, which asks about yt-dlp first, so a video
	// that fails in the meantime waits for the ffmpeg about to come.
	fetchFFmpeg := a.InstallMissingTools && a.Settings.Get().YtdlpAutoUpdate &&
		mediatools.CanInstallFFmpeg(a.DataDir) == nil &&
		mediatools.ResolveFFmpeg(a.DataDir).FFmpegSource == mediatools.SourceNone
	if fetchFFmpeg {
		a.beginFFmpegFetch()
	}
	a.spawn(func() {
		first := ytdlpAutoFirstRun
		st := a.MediaTools()
		if fetchFFmpeg || (a.InstallMissingTools && (!st.Ytdlp.Found || !st.FFmpeg.Found)) {
			first = 0
		}
		a.ytdlpAutoLoop(first, ytdlpAutoEvery)
	})
}

// ytdlpAutoLoop reads the switch on every run rather than once, so turning it
// on or off takes effect without a restart.
func (a *App) ytdlpAutoLoop(first, every time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		a.autoUpdateYtdlp()
		a.autoUpdateFFmpeg()
		timer.Reset(every)
	}
}

// autoUpdateYtdlp is one daily run. It fetches only when GitHub names a release
// newer than the yt-dlp in use. A version it cannot put in order, such as a
// build from git, is left alone. So is a machine with no yt-dlp at all unless
// InstallMissingTools is set: in the container a missing yt-dlp is a broken
// image or an operator's choice, not something to paper over.
func (a *App) autoUpdateYtdlp() {
	if !a.Settings.Get().YtdlpAutoUpdate {
		return
	}
	if err := mediatools.CanInstall(a.DataDir); err != nil {
		log.Printf("yt-dlp: the daily update is skipped, there is nowhere to put a fetched copy: %v", err)
		return
	}
	a.ytdlpMu.Lock()
	defer a.ytdlpMu.Unlock()

	current := a.MediaTools().Ytdlp
	if !current.Found {
		if !a.InstallMissingTools {
			log.Print("yt-dlp: none is installed, so the daily update has nothing to keep current")
			return
		}
		log.Print("yt-dlp: none is installed, so the daily update fetches the newest release")
		if _, err := a.updateYtdlpLocked(a.ctx); err != nil && a.ctx.Err() == nil {
			log.Printf("yt-dlp: fetching it failed, media links stay with the other backends until the next try: %v", err)
		}
		return
	}
	latest := a.latestYtdlp(a.ctx, current.Version)
	switch {
	case a.ctx.Err() != nil:
	case !latest.Checked:
		log.Printf("yt-dlp: the daily update could not ask GitHub for the newest release: %s", latest.Detail)
	case latest.Compare == mediatools.CompareSame || latest.Compare == mediatools.CompareOlder:
		log.Printf("yt-dlp: %s is up to date (newest release %s), nothing fetched", current.Version, latest.Tag)
	case latest.Compare != mediatools.CompareNewer:
		log.Printf("yt-dlp: %s and the newest release %s cannot be put in order, so the daily update leaves it alone", current.Version, latest.Tag)
	default:
		if _, err := a.updateYtdlpLocked(a.ctx); err != nil && a.ctx.Err() == nil {
			log.Printf("yt-dlp: the daily update to %s failed and %s stays in use: %v", latest.Tag, current.Version, err)
		}
	}
}

// autoUpdateFFmpeg is the daily run's ffmpeg half, which only ever touches a
// copy it fetched itself: an ffmpeg on PATH is left to whoever installed it.
// A missing one is fetched where InstallMissingTools allows it, and a fetched
// one is replaced once it is FFmpegRefresh old.
func (a *App) autoUpdateFFmpeg() {
	ready := false
	defer func() { a.endFFmpegFetch(ready) }()
	if !a.Settings.Get().YtdlpAutoUpdate || !a.InstallMissingTools {
		return
	}
	if err := mediatools.CanInstallFFmpeg(a.DataDir); err != nil {
		return
	}
	a.ffmpegMu.Lock()
	defer a.ffmpegMu.Unlock()

	switch ff := mediatools.ResolveFFmpeg(a.DataDir); ff.FFmpegSource {
	case mediatools.SourcePath:
		ready = true
		return
	case mediatools.SourceManaged:
		// A failed refresh leaves the copy in place, so it counts either way.
		ready = true
		rec, err := mediatools.LoadFFmpegRecord(a.DataDir)
		if err == nil && rec != nil && time.Since(rec.FetchedAt) < mediatools.FFmpegRefresh {
			return
		}
		log.Print("ffmpeg: the fetched copy is due, so the daily update fetches the newest build")
	default:
		log.Print("ffmpeg: none is installed, so the daily update fetches the newest build")
		a.beginFFmpegFetch()
	}
	rec, err := a.installFFmpeg(a.ctx, a.DataDir)
	if err != nil {
		if a.ctx.Err() == nil {
			log.Printf("ffmpeg: fetching it failed and nothing was replaced: %v", err)
		}
		return
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Printf("ffmpeg: fetched %s (%s) and put it in force", rec.Version, rec.Asset)
	ready = true
}

func (a *App) beginFFmpegFetch() {
	a.mu.Lock()
	a.fetchingFFmpeg = true
	a.mu.Unlock()
}

// awaitFFmpegLocked puts a row that failed for want of ffmpeg back in the
// queue while one is being fetched, and reports whether it did. The run left
// nothing to carry on with, so the next one starts afresh. Caller holds a.mu.
func (a *App) awaitFFmpegLocked(t *core.Task) bool {
	if !a.fetchingFFmpeg {
		return false
	}
	if a.awaitingFFmpeg == nil {
		a.awaitingFFmpeg = map[string]string{}
	}
	a.awaitingFFmpeg[t.ID] = t.Error
	t.Status = core.StatusQueued
	t.ClearFailure()
	t.Loaded, t.Speed = 0, 0
	t.Note = ""
	t.StalledSince = time.Time{}
	delete(a.active, t.ID)
	delete(a.started, t.ID)
	if !slices.Contains(a.queue, t.ID) {
		a.queue = append(a.queue, t.ID)
	}
	return true
}

// endFFmpegFetch lets the rows that waited for the fetch go: to the dispatcher
// when an ffmpeg is in place, to the failure they met when not. On shutdown
// they stay queued, and the next start fetches again.
func (a *App) endFFmpegFetch(ready bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.fetchingFFmpeg || a.ctx.Err() != nil {
		return
	}
	a.fetchingFFmpeg = false
	held := a.awaitingFFmpeg
	a.awaitingFFmpeg = nil
	var settled []taskCopy
	for id, sentence := range held {
		t := a.tasks[id]
		// A row paused or removed meanwhile is no longer waiting.
		if ready || t == nil || t.Status != core.StatusQueued {
			continue
		}
		a.dequeueLocked(id)
		t.Status = core.StatusError
		t.Reason = core.ReasonNoFFmpeg
		t.SetError(sentence, core.CodeNoFFmpeg, nil)
		t.GaveUp = true
		t.Waiting = core.WaitingNone
		a.forgetLinkLocked(t)
		settled = append(settled, a.copyLocked(t))
	}
	a.dispatchLocked()
	if len(settled) > 0 {
		a.spawn(func() { a.publishTasks(settled) })
	}
}
