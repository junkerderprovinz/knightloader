package app

// yt-dlp, ffmpeg and Deno as far as the App is concerned: what is installed,
// and the operations that replace it.

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
	// ytdlpMu keeps a fetch and the automatic update off the same files at
	// the same time.
	ytdlpMu sync.Mutex
	// ffmpegMu does the same for the fetched ffmpeg, and denoMu for the
	// fetched Deno.
	ffmpegMu sync.Mutex
	denoMu   sync.Mutex
	// latestYtdlp, installYtdlp, updateFFmpeg and updateDeno are
	// mediatools.CheckLatest, Install, UpdateFFmpeg and UpdateDeno, which the
	// tests replace so that nothing leaves the machine.
	latestYtdlp  func(ctx context.Context, installed string) mediatools.Latest
	installYtdlp func(ctx context.Context, dataDir string) (mediatools.ManagedRecord, error)
	updateFFmpeg func(ctx context.Context, dataDir string) (mediatools.FFmpegRecord, bool, error)
	updateDeno   func(ctx context.Context, dataDir string) (mediatools.DenoRecord, bool, error)

	// fetchingFFmpeg is set while an ffmpeg that was missing is on its way,
	// and awaitingFFmpeg holds the rows that failed for want of it meanwhile,
	// each with yt-dlp's sentence. Both are guarded by the App's mu.
	fetchingFFmpeg bool
	awaitingFFmpeg map[string]string

	// InstallMissingTools lets the automatic update fetch yt-dlp, ffmpeg and
	// Deno where none is found, and makes its first run right at start in that
	// case. The desktop app sets it: the macOS app, a portable copy and a
	// Flatpak carry only the program, and a silent install fetches nothing.
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
// installed one. Besides the updates it is the only call here that leaves the
// box, made on an explicit request.
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

// The automatic update waits a few minutes after start, so a start is not
// spent downloading and a container caught in a restart loop does not ask
// GitHub on every attempt.
const ytdlpAutoFirstRun = 5 * time.Minute

// StartYtdlpAutoUpdate starts the automatic update of yt-dlp, ffmpeg and
// Deno. The binaries call it rather than New, which every test calls. Close
// stops it and cancels a download in flight.
func (a *App) StartYtdlpAutoUpdate() {
	// Known before the first run, which asks about yt-dlp first, so a video
	// that fails in the meantime waits for the ffmpeg about to come.
	fetchFFmpeg := a.InstallMissingTools && a.Settings.Get().ComponentsAutoUpdate &&
		mediatools.CanInstallFFmpeg(a.DataDir) == nil &&
		mediatools.ResolveFFmpeg(a.DataDir).FFmpegSource == mediatools.SourceNone
	if fetchFFmpeg {
		a.beginFFmpegFetch()
	}
	a.spawn(func() {
		first := ytdlpAutoFirstRun
		st := a.MediaTools()
		if fetchFFmpeg || (a.InstallMissingTools && (!st.Ytdlp.Found || !st.FFmpeg.Found || !st.Deno.Found)) {
			first = 0
		}
		a.tickLoop(first, componentTick, a.autoUpdateMediaTools)
	})
}

func (a *App) autoUpdateMediaTools() {
	a.autoUpdateYtdlp()
	a.autoUpdateFFmpeg()
	a.autoUpdateDeno()
}

// autoUpdateYtdlp asks after yt-dlp once it is due, or at once where the
// desktop build has none.
func (a *App) autoUpdateYtdlp() {
	missing := a.InstallMissingTools && !a.MediaTools().Ytdlp.Found
	if a.componentDue(componentYtdlp, missing) {
		a.asked(componentYtdlp, a.updateYtdlpComponent(a.ctx, false))
	}
}

// updateYtdlpComponent fetches yt-dlp only when GitHub names a release newer
// than the yt-dlp in use. A version it cannot put in order, such as a build
// from git, is left alone unless somebody pressed Update now. So is a machine
// with no yt-dlp at all unless InstallMissingTools is set or somebody pressed
// it: in the container a missing yt-dlp is a broken image or an operator's
// choice, not something to paper over. A fetched copy that does not start is
// fetched again whatever the versions say, since nothing newer may come out
// for weeks and the resolver passes over it until then.
func (a *App) updateYtdlpComponent(ctx context.Context, pressed bool) ComponentResult {
	if err := mediatools.CanInstall(a.DataDir); err != nil {
		log.Printf("yt-dlp: the update is skipped, there is nowhere to put a fetched copy: %v", err)
		return ComponentResult{State: ComponentSkipped, Detail: err.Error()}
	}
	a.ytdlpMu.Lock()
	defer a.ytdlpMu.Unlock()

	current := a.MediaTools().Ytdlp
	if mediatools.FetchedCopyFails(a.DataDir) {
		log.Printf("yt-dlp: the fetched copy at %s does not start, so the update fetches the newest release in its place", mediatools.BinaryPath(a.DataDir))
		otherwise := "media links stay with the other backends until the next try"
		if current.Found {
			otherwise = current.Version + " stays in use"
		}
		return a.fetchYtdlpLocked(ctx, otherwise)
	}
	if !current.Found {
		if !a.InstallMissingTools && !pressed {
			log.Print("yt-dlp: none is installed, so the update has nothing to keep current")
			return ComponentResult{State: ComponentSkipped}
		}
		log.Print("yt-dlp: none is installed, so the update fetches the newest release")
		return a.fetchYtdlpLocked(ctx, "media links stay with the other backends until the next try")
	}
	latest := a.latestYtdlp(ctx, current.Version)
	switch {
	case ctx.Err() != nil:
		return ComponentResult{State: ComponentFailed, Detail: ctx.Err().Error()}
	case !latest.Checked:
		log.Printf("yt-dlp: the update could not ask GitHub for the newest release: %s", latest.Detail)
		return ComponentResult{State: ComponentFailed, Version: current.Version, Detail: latest.Detail}
	case latest.Compare == mediatools.CompareSame || latest.Compare == mediatools.CompareOlder:
		log.Printf("yt-dlp: %s is up to date (newest release %s), nothing fetched", current.Version, latest.Tag)
		return ComponentResult{State: ComponentCurrent, Version: current.Version}
	case latest.Compare != mediatools.CompareNewer && !pressed:
		log.Printf("yt-dlp: %s and the newest release %s cannot be put in order, so the update leaves it alone", current.Version, latest.Tag)
		return ComponentResult{State: ComponentCurrent, Version: current.Version}
	}
	return a.fetchYtdlpLocked(ctx, current.Version+" stays in use")
}

// fetchYtdlpLocked fetches the newest yt-dlp, saying what happens instead
// when that fails. Caller holds ytdlpMu.
func (a *App) fetchYtdlpLocked(ctx context.Context, otherwise string) ComponentResult {
	rec, err := a.updateYtdlpLocked(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("yt-dlp: fetching the newest release failed, %s: %v", otherwise, err)
		}
		return ComponentResult{State: ComponentFailed, Detail: err.Error()}
	}
	return ComponentResult{State: ComponentUpdated, Version: rec.Version}
}

// autoUpdateFFmpeg asks after ffmpeg once it is due, or at once where the
// desktop build has none.
func (a *App) autoUpdateFFmpeg() {
	missing := a.InstallMissingTools && mediatools.ResolveFFmpeg(a.DataDir).FFmpegSource == mediatools.SourceNone
	if !a.componentDue(componentFFmpeg, missing) {
		// Lets go the rows that wait for an ffmpeg that is not coming.
		a.endFFmpegFetch(false)
		return
	}
	a.asked(componentFFmpeg, a.updateFFmpegComponent(a.ctx))
}

// updateFFmpegComponent only ever touches a copy it fetched itself: an ffmpeg
// on PATH is left to whoever installed it. A missing one is fetched where
// InstallMissingTools allows it, and a fetched one is replaced when the
// release holds another build.
func (a *App) updateFFmpegComponent(ctx context.Context) ComponentResult {
	ready := false
	defer func() { a.endFFmpegFetch(ready) }()
	if err := mediatools.CanInstallFFmpeg(a.DataDir); err != nil {
		return ComponentResult{State: ComponentSkipped, Detail: err.Error()}
	}
	a.ffmpegMu.Lock()
	defer a.ffmpegMu.Unlock()

	switch ff := mediatools.ResolveFFmpeg(a.DataDir); ff.FFmpegSource {
	case mediatools.SourcePath:
		ready = true
		return ComponentResult{State: ComponentSkipped, Detail: ff.FFmpeg}
	case mediatools.SourceManaged:
		// A failed update leaves the copy in place, so it counts either way.
		ready = true
	default:
		if !a.InstallMissingTools {
			return ComponentResult{State: ComponentSkipped}
		}
		log.Print("ffmpeg: none is installed, so the update fetches the newest build")
		a.beginFFmpegFetch()
	}
	rec, current, err := a.updateFFmpeg(ctx, a.DataDir)
	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.Printf("ffmpeg: fetching it failed and nothing was replaced: %v", err)
		}
		return ComponentResult{State: ComponentFailed, Detail: err.Error()}
	case current:
		return ComponentResult{State: ComponentCurrent, Version: rec.Version}
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Printf("ffmpeg: fetched %s (%s) and put it in force", rec.Version, rec.Asset)
	ready = true
	return ComponentResult{State: ComponentUpdated, Version: rec.Version}
}

// autoUpdateDeno asks after Deno once it is due, or at once where the desktop
// build has none.
func (a *App) autoUpdateDeno() {
	_, source := mediatools.ResolveDeno(a.DataDir)
	missing := a.InstallMissingTools && source == mediatools.SourceNone
	if a.componentDue(componentDeno, missing) {
		a.asked(componentDeno, a.updateDenoComponent(a.ctx))
	}
}

// updateDenoComponent is ffmpeg's update for Deno: a Deno on PATH or in
// KL_DENO is left to its owner, a missing one is fetched where
// InstallMissingTools allows it, and a fetched one is replaced by a newer
// release. Nothing waits for it, since yt-dlp runs without one.
func (a *App) updateDenoComponent(ctx context.Context) ComponentResult {
	if err := mediatools.CanInstallDeno(a.DataDir); err != nil {
		return ComponentResult{State: ComponentSkipped, Detail: err.Error()}
	}
	a.denoMu.Lock()
	defer a.denoMu.Unlock()

	switch path, source := mediatools.ResolveDeno(a.DataDir); source {
	case mediatools.SourcePath, mediatools.SourceEnv:
		return ComponentResult{State: ComponentSkipped, Detail: path}
	case mediatools.SourceNone:
		if !a.InstallMissingTools {
			return ComponentResult{State: ComponentSkipped}
		}
		log.Print("deno: none is installed, so the update fetches the newest release")
	}
	rec, current, err := a.updateDeno(ctx, a.DataDir)
	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.Printf("deno: fetching it failed and nothing was replaced, yt-dlp carries on without it: %v", err)
		}
		return ComponentResult{State: ComponentFailed, Detail: err.Error()}
	case current:
		return ComponentResult{State: ComponentCurrent, Version: rec.Version}
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Printf("deno: fetched %s (%s) and handed it to yt-dlp", rec.Version, rec.Asset)
	return ComponentResult{State: ComponentUpdated, Version: rec.Version}
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
