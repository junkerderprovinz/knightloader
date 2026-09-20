package app

// yt-dlp and ffmpeg as far as the App is concerned: what is installed, and the
// operations that replace it.

import (
	"context"
	"log"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// mediaToolsState is embedded on App so its fields stay in this file. The prober
// is built on first use; DataDir is already set by then.
type mediaToolsState struct {
	mediaToolsOnce sync.Once
	mediaTools     *mediatools.Prober
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
// installed one. It is the only call here that leaves the box, made on an
// explicit press or the opt-in check on page load.
func (a *App) YtdlpLatest(ctx context.Context) mediatools.Latest {
	return mediatools.CheckLatest(ctx, a.MediaTools().Ytdlp.Version)
}

// UpdateYtdlp fetches, verifies, smoke-tests and installs the newest yt-dlp,
// then puts it in force without a restart.
//
// Invalidate comes first so the next probe reads the new binary; rewireBackends
// then re-resolves the path and rebuilds the yt-dlp backend around it.
func (a *App) UpdateYtdlp(ctx context.Context) (mediatools.ManagedRecord, error) {
	rec, err := mediatools.Install(ctx, a.DataDir)
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
	if err := mediatools.Remove(a.DataDir); err != nil {
		return a.MediaTools(), err
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Print("yt-dlp: the fetched copy was removed; the system's own copy is in force again")
	return a.MediaTools(), nil
}
