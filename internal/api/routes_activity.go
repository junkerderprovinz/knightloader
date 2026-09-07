package api

// Calling off ambient background work.
//
// The counters themselves have no route and deliberately never grow one: they
// are a live signal, broadcast over the hub as "activity" and sent once per
// connection as "activitySnapshot" (internal/app/app_activity.go), not durable
// state worth a GET. This file serves the one thing a broadcast cannot do -
// act on what it is describing.
//
// One route for every kind rather than one per subject. The strip shows a stop
// button on a row exactly when that row's `cancellable` count is above zero,
// and a route per kind would be four more paths saying the same sentence, each
// able to fall out of step with the strip on its own.

import (
	"net/http"
	"strconv"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerActivity(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/activity/{kind}/abort",
		"call off the running background work of one kind (crawl, linkcheck, captcha, autoconfirm, container) - returns how many runs were stopped",
		func(w http.ResponseWriter, r *http.Request) {
			kind, ok := app.KnownActivityKind(r.PathValue("kind"))
			if !ok {
				// Refused rather than answered with a cheerful zero. "Nothing
				// was running" and "that is not a kind" look identical in a
				// count, and the second one is a client bug that would go on
				// being pressed forever.
				http.Error(w, "unknown activity kind "+strconv.Quote(r.PathValue("kind")), http.StatusBadRequest)
				return
			}
			// Zero is a perfectly good answer and not an error: the run this
			// was aimed at may have finished between the strip drawing the
			// button and somebody pressing it, which is the ordinary race for
			// a control that only exists while work is in flight - the same
			// tolerance the captcha skip route describes for "already gone".
			writeJSON(w, map[string]int{"cancelled": a.AbortActivity(kind)})
		})
}
