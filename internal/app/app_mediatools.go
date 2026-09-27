package app

// yt-dlp and ffmpeg as far as the App is concerned: what is installed, and the
// operations that replace it.

import (
	"context"
	"log"
	"sync"
	"time"

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
	// latestYtdlp and installYtdlp are mediatools.CheckLatest and
	// mediatools.Install, which the tests replace so that nothing leaves the
	// machine.
	latestYtdlp  func(ctx context.Context, installed string) mediatools.Latest
	installYtdlp func(ctx context.Context, dataDir string) (mediatools.ManagedRecord, error)
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

// RevertYtdlp deletes the fetched copy and its record, handing KL_YTDLP and PATH
// back the job, and returns the resulting status so the page can show which
// yt-dlp is running without asking again.
func (a *App) RevertYtdlp() (mediatools.Status, error) {
	a.ytdlpMu.Lock()
	defer a.ytdlpMu.Unlock()
	if err := mediatools.Remove(a.DataDir); err != nil {
		return a.MediaTools(), err
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Print("yt-dlp: the fetched copy was removed; the system's own copy is in force again")
	return a.MediaTools(), nil
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
	a.spawn(func() { a.ytdlpAutoLoop(ytdlpAutoFirstRun, ytdlpAutoEvery) })
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
		timer.Reset(every)
	}
}

// autoUpdateYtdlp is one daily run. It fetches only when GitHub names a release
// newer than the yt-dlp in use. A version it cannot put in order, such as a
// build from git, is left alone, and so is a machine with no yt-dlp at all:
// the switch keeps yt-dlp current and was never asked to install it.
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
		log.Print("yt-dlp: none is installed, so the daily update has nothing to keep current")
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
