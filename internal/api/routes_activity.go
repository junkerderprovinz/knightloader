package api

// Calling off ambient background work. The counters themselves have no route:
// they are broadcast over the hub as "activity" and "activitySnapshot"
// (internal/app/app_activity.go), not durable state worth a GET.

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
				// "Nothing was running" and "that is not a kind" look
				// identical in a count, so an unknown kind is refused rather
				// than answered with a zero.
				http.Error(w, "unknown activity kind "+strconv.Quote(r.PathValue("kind")), http.StatusBadRequest)
				return
			}
			// Zero is not an error: the run may have finished between the
			// strip drawing the button and somebody pressing it.
			writeJSON(w, map[string]int{"cancelled": a.AbortActivity(kind)})
		})
}
