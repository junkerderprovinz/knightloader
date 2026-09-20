package app

// Event targets: the seam between this package and internal/notify. notify
// subscribes to the event stream on its own, so all it needs from here is the
// configuration, a way to read its health, and the test button.
//
// The test button lives here rather than in the route because it needs the
// stored secrets. The browser only ever sees "********" for header values (see
// settings.Redacted), so a draft sent as posted would carry the placeholder and
// fail with a 401 that says nothing about the real configuration.

import (
	"context"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// applyEventTargets makes the running dispatcher match the configuration. It
// runs on every settings change, so adding a target needs no restart. notify.Set
// reconciles the list so unrelated saves keep each worker's queue and health.
func (a *App) applyEventTargets(s settings.Settings) {
	if a.EventTargets == nil {
		return
	}
	a.EventTargets.Set(s.EventTargets)
}

// EventTargetHealth reports what each target has done since this process
// started, or nil when there is no dispatcher. The route joins it onto the
// configured list so a saved target that never delivered still shows up.
func (a *App) EventTargetHealth() []notify.Health {
	if a.EventTargets == nil {
		return nil
	}
	return a.EventTargets.Health()
}

// TestEventTarget sends one sample event to one target right away and reports
// the attempt. It really sends, and it bypasses the dispatcher so a running
// target's queue is left alone and an unsaved row can be tested.
//
// The request's context is used, so a browser that navigated away does not
// leave this waiting on the target's full time limit.
func (a *App) TestEventTarget(ctx context.Context, draft notify.Target) notify.Attempt {
	// The same Merge the save path uses, so a secret never follows a changed
	// address here either.
	merged := notify.Merge([]notify.Target{draft}, a.Settings.Get().EventTargets)
	return notify.Send(ctx, merged[0], notify.SampleFiring(time.Now()), a.Settings.Get().InstanceName)
}
