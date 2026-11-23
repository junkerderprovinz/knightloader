package app

// Event targets: the seam between this package and internal/notify, which
// subscribes to the event stream on its own and needs from here only the
// channels, a way to read their health, and the test button.

import (
	"context"

	kitnotify "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// applyEventTargets makes the running dispatcher match the channels. It runs
// on every settings change, so a new channel needs no restart.
func (a *App) applyEventTargets(s settings.Settings) {
	if a.EventTargets == nil {
		return
	}
	a.EventTargets.Set(s.NotifyChannels)
}

// EventTargetHealth reports what each channel has done since this process
// started, or nil when there is no dispatcher.
func (a *App) EventTargetHealth() []notify.Health {
	if a.EventTargets == nil {
		return nil
	}
	return a.EventTargets.Health()
}

// TestEventTarget sends one sample event to one channel right away. The
// browser holds the channel with its secrets redacted, so the stored ones are
// merged back first, under the save's rule that a secret never follows a
// changed destination.
func (a *App) TestEventTarget(ctx context.Context, draft kitnotify.Channel) kitnotify.Result {
	s := a.Settings.Get()
	merged := kitnotify.Merge([]kitnotify.Channel{draft}, s.NotifyChannels)
	return notify.Test(ctx, merged[0], s.InstanceName, nil)
}
