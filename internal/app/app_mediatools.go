package app

// The two external programs a media download actually runs - yt-dlp and ffmpeg
// - as far as this App is concerned: what they are, and the one operation that
// changes what they are.
//
// The API layer only ever calls App methods, the same split a.JDStatus() and
// a.ResolverPriority() already follow: internal/api never imports a subsystem
// to ask it something directly, so a route stays four lines and the decisions
// stay where the state is.

import (
	"context"
	"log"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// mediaToolsState is embedded on App for the reason iconCache is: the fields
// stay in the file that owns them instead of being two more lines in app.go
// that nothing in app.go touches.
//
// Built on first use rather than in New, exactly like unpack and iconCache
// above it. That is not laziness for its own sake - it is what keeps this
// feature to ONE line in app.go, and app.go is the file every wave wants to
// edit at once. DataDir is set in New's own struct literal, so it is already
// there by the time anything can call this.
type mediaToolsState struct {
	mediaToolsOnce sync.Once
	mediaTools     *mediatools.Prober
}

func (a *App) mediaToolsProber() *mediatools.Prober {
	a.mediaToolsOnce.Do(func() { a.mediaTools = mediatools.NewProber(a.DataDir) })
	return a.mediaTools
}

// MediaTools is what the Resolvers settings page and the diagnostics bundle
// both read. It NEVER touches the network - see mediatools.Prober.Read - so a
// browser sitting on either page is not making outbound calls, and it is cached
// for a minute so that page is not a process fountain either.
func (a *App) MediaTools() mediatools.Status {
	return a.mediaToolsProber().Read()
}

// YtdlpLatest asks GitHub what the newest yt-dlp release is and orders it
// against the one that is installed.
//
// The ONE call in this feature that leaves the box, and it happens only on an
// explicit press or on the opt-in "ask when this page opens" switch. It
// downloads nothing and replaces nothing.
func (a *App) YtdlpLatest(ctx context.Context) mediatools.Latest {
	return mediatools.CheckLatest(ctx, a.MediaTools().Ytdlp.Version)
}

// UpdateYtdlp fetches, verifies, smoke-tests and installs the newest yt-dlp,
// then puts it in force without a restart.
//
// The two lines after the install are what make that last part true, and they
// are in this order for a reason. Invalidate first, so the probe that
// rewireBackends' logging and the settings page's next load will read is the
// new binary's and not the previous one's; then rewireBackends, which is what
// actually re-resolves the path, rebuilds the yt-dlp backend around it and
// registers or unregisters the resolver accordingly. Skipping the second would
// leave a correct card over a resolver still spawning the old copy until
// something else happened to rewire.
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

// RevertYtdlp deletes the fetched copy and its record, handing KL_YTDLP and
// PATH back the job, and answers with the status that results - so the page can
// say which yt-dlp is running now rather than asking again and drawing a gap in
// between.
func (a *App) RevertYtdlp() (mediatools.Status, error) {
	if err := mediatools.Remove(a.DataDir); err != nil {
		return a.MediaTools(), err
	}
	a.mediaToolsProber().Invalidate()
	a.rewireBackends()
	log.Print("yt-dlp: the fetched copy was removed; the system's own copy is in force again")
	return a.MediaTools(), nil
}
