package app

// Quiet mode while a media server plays. internal/playback asks the servers and
// reports one bit; this file turns it into quiet mode (app_quiet.go) and into a
// hold on the unpacking queue (app_extract.go).
//
// A stream is a third source beside the switch and the timetable, and the
// weakest of the three: it can only switch the mode on. Pressing the turtle off
// while a film runs waives that film, and the next one starts the mode again.

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/playback"
)

// startPlayback builds the watcher and runs it until Close. Called from New
// once a.Accounts and a.Settings exist.
func (a *App) startPlayback() {
	a.Playback = playback.NewWatcher(playback.Options{
		Settings: func() playback.Settings { return a.Settings.Get().Playback },
		Servers:  func() []playback.Server { return a.Settings.Get().PlaybackServers },
		Token:    a.PlaybackStore().Token,
		OnChange: a.setPlayback,
	})
	a.spawn(func() { a.Playback.Run(a.ctx) })
}

// PlaybackStore is the sealed store the servers' tokens live in. A fresh
// wrapper per call is safe, as with MediaHookStore.
func (a *App) PlaybackStore() *playback.Store { return playback.NewStore(a.Accounts) }

// PlaybackStatus is the watcher's last conclusion, for the settings card.
func (a *App) PlaybackStatus() playback.Status {
	if a.Playback == nil {
		return playback.Status{}
	}
	return a.Playback.Status()
}

// CheckPlayback asks one server once with its sealed token, for the card's test
// button. An App assembled in a test without a watcher answers through a client
// of its own.
func (a *App) CheckPlayback(ctx context.Context, s playback.Server) ([]playback.Stream, error) {
	if a.Playback == nil {
		return playback.Ask(ctx, playback.NewClient(), s, a.PlaybackStore().Token(s.ID))
	}
	return a.Playback.Check(ctx, s)
}

// setPlayback is the watcher's report that something started or stopped
// counting.
func (a *App) setPlayback(active bool) {
	cfg := a.Settings.Get()
	a.mu.Lock()
	a.quiet.playback = active
	if !active {
		a.quiet.waived = false
	}
	a.settleQuietLocked(cfg)
	a.mu.Unlock()
	a.afterQuietChange()
}

// extractHeldLocked is whether the unpacking queue waits for a stream. The
// archive already being unpacked is left to finish, as quiet mode leaves a
// running transfer alone. Caller holds a.mu.
func (a *App) extractHeldLocked() bool {
	return a.quiet.byPlayback() && a.Settings.Get().Playback.HoldExtraction
}

// resumeExtractionsLocked starts the unpacking worker again for jobs that
// waited out a stream. Caller holds a.mu.
func (a *App) resumeExtractionsLocked() {
	st := a.unpackLocked()
	if st.busy || a.extractHeldLocked() || nextQueuedLocked(st) == nil {
		return
	}
	st.busy = true
	a.spawn(a.runExtractions)
}

// publishHeldExtractions sends every waiting job again, so the list says
// whether it waits for a stream or for its turn.
func (a *App) publishHeldExtractions() {
	for _, j := range a.ExtractJobs() {
		if j.Status == ExtractQueued {
			a.Hub.Broadcast("extract", j)
		}
	}
}
