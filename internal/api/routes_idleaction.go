package api

// The end-of-queue action: whether the wait queue is idle, whether a
// cancellable countdown is running, and calling one off. The action itself is
// configured through PUT /api/settings like every other switch on the
// Automation page (Settings.IdleAction). What is left here is live state,
// cancelling a countdown, and the two questions about the operator's own
// command that only this instance can answer: would it resolve here, and what
// happens when it runs.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
)

func registerIdleAction(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/idle-action",
		"the end-of-queue action: whether the queue is idle, whether a countdown is running, and when it fires",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.IdleActionState())
		})

	reg.Add(http.MethodPost, "/api/idle-action/cancel",
		"call off a countdown in progress, without turning the action off for the next time the queue goes idle",
		func(w http.ResponseWriter, r *http.Request) {
			a.CancelIdleAction()
			writeJSON(w, a.IdleActionState())
		})

	// Served rather than compiled into the interface, as /api/queue/priorities
	// is: a menu built from the client's own list offers entries this build
	// cannot carry out.
	//
	// Offered and not Actions, and this is the only place the filtered list
	// belongs. Actions is the validation vocabulary settings sanitize reads,
	// and filtering that by capability would rewrite a stored "suspend" to
	// "none" on the next unrelated save. See idleaction.Actions.
	reg.Add(http.MethodGet, "/api/idle-action/actions",
		"the end-of-queue actions this build can offer, in menu order",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, idleaction.Offered(a.IdleCapabilities()))
		})

	// Resolves the configured command and reports what it found without
	// running it, so it is safe to press on an action that would otherwise put
	// the machine to sleep.
	//
	// 200 even when the problem is not none: the value of this route is the
	// sentence it carries, and a page cannot read the sentence out of a 500.
	// The status says the check ran, not that the command is fine.
	//
	// POST rather than GET, like routes_reconnect.go's test route: it is an
	// action the operator takes, and it must not end up in a browser's
	// history, a prefetch or a link somebody can be sent.
	reg.Add(http.MethodPost, "/api/idle-action/check",
		"resolve the configured end-of-queue command and report what would run, without running it",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.IdleCommandCheck(buildinfo.Deployment))
		})

	// Run the configured command once, now, exactly as the countdown would.
	//
	// Refused unless the configured action is the command one, with 409 rather
	// than 400: nothing about the request is malformed, the instance is not in
	// a state where it means anything. A test button that quits the process or
	// suspends the machine is the thing itself under a reassuring label, and
	// those are the two actions nobody can undo from the page.
	reg.Add(http.MethodPost, "/api/idle-action/run",
		"run the configured end-of-queue command once, now; refused unless the action is the command one",
		func(w http.ResponseWriter, r *http.Request) {
			cfg := a.Settings.Get().IdleAction
			if cfg.Action != idleaction.ActionCommand {
				writeJSONStatus(w, http.StatusConflict, map[string]string{
					"error": "the end-of-queue action is not the command one, so there is nothing to run from here",
				})
				return
			}
			writeJSON(w, a.RunIdleCommandNow(cfg.Command))
		})
}
