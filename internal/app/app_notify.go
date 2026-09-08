package app

// The event targets: the seam between this package and internal/notify.
//
// It is deliberately thin, and thinner than app_feeds.go's equivalent, because
// internal/notify needs nothing from here except the event stream it already
// subscribes to. There is no callback back into the app, no store to keep and
// no state to reconcile beyond the list itself - a target reports on what
// happened, it never asks this package to do anything - so the whole seam is
// three functions: hand the dispatcher the configuration, read its health, and
// send one message by hand for the test button.
//
// WHY THE TEST BUTTON LIVES HERE AND NOT IN THE ROUTE: it needs the STORED
// secrets. The browser is shown "********" in place of every header value (see
// settings.Redacted), so the draft row it posts back carries the placeholder
// rather than the token, and a route that sent that draft as it stands would
// test a request with eight literal stars where the credential goes and report
// a 401 that has nothing to do with the operator's configuration. Merging the
// real values in requires reading the settings store, and it requires doing it
// under exactly the rule notify.Merge enforces: only while the row still points
// at the same address.

import (
	"context"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// applyEventTargets makes the running dispatcher match the configuration. It
// runs on every settings change, so adding a target does not need a restart.
//
// The list is handed over whole and notify.Set does the reconciling, for the
// reason applyFeeds gives about its own pollers: a worker carries a queue and a
// health row, and rebuilding on every save would mean saving the speed limit
// blanks the status table the operator is reading.
func (a *App) applyEventTargets(s settings.Settings) {
	if a.EventTargets == nil {
		return
	}
	a.EventTargets.Set(s.EventTargets)
}

// EventTargetHealth reports what each target has been doing since this process
// started.
//
// A nil answer is a real one: no dispatcher, so nothing is being sent at all.
// The route joins this onto the CONFIGURED list rather than serving it alone,
// which is the same call routes_feeds.go makes and for the same reason - the
// row that matters most is the one missing from here, a target that is saved
// and switched on and has never managed to deliver anything.
func (a *App) EventTargetHealth() []notify.Health {
	if a.EventTargets == nil {
		return nil
	}
	return a.EventTargets.Health()
}

// TestEventTarget sends one made-up event to one target, right now, and reports
// everything about the attempt.
//
// IT REALLY SENDS. There is no dry run here and there deliberately is not one:
// the whole worth of the button is that it answers about the request the target
// will really make, and a dry run answers about a different request. The copy
// beside the button says so in as many words ("your phone will buzz and your
// chat room will see it").
//
// It goes nowhere near the dispatcher, exactly as InspectFeed goes nowhere near
// the feed runner. Testing a target that is already sending must not disturb
// its queue, and testing one that has never been saved must not create a worker
// - this is a person asking what is at an address, not a target being switched
// on. It follows that this works on a row the operator has not saved yet, which
// is most of its value.
//
// The context is the request's own, so a browser that navigated away does not
// leave this waiting on a push server for the target's whole time limit.
func (a *App) TestEventTarget(ctx context.Context, draft notify.Target) notify.Attempt {
	// The stored secrets go back in first, and through the same Merge the save
	// path uses rather than a second, looser copy of it here. A second copy is
	// how the test button becomes the one place the "a secret does not follow a
	// changed address" rule is not enforced - and the test button is the one
	// place a caller can name any address it likes without saving anything.
	merged := notify.Merge([]notify.Target{draft}, a.Settings.Get().EventTargets)
	return notify.Send(ctx, merged[0], notify.SampleFiring(time.Now()), a.Settings.Get().InstanceName)
}
