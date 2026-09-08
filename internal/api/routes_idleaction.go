package api

// The end-of-queue action: whether the wait queue is idle right now, whether
// a cancellable countdown is running, and calling one off. The action itself
// is configured through the ordinary settings document (Settings.IdleAction,
// internal/settings/settings_idleaction.go) - the same PUT /api/settings
// every other plain switch and number on the Downloads settings page already
// goes through. This file only serves what that document alone cannot
// answer: live state, cancelling a countdown in progress, and the two
// questions about the operator's own command that only this instance can
// answer - would it resolve here, and what does it do when it runs.

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

	// Served rather than compiled into the interface, for the same reason
	// /api/queue/priorities is: a menu built from the client's own list
	// offers whatever that build was compiled with, and an entry this build
	// cannot carry out is a control that does nothing when it is pressed.
	//
	// Offered, NOT Actions. The two are different lists on purpose and this
	// is the only place the filtered one may be used: Actions is the
	// validation vocabulary that settings sanitize reads, and filtering
	// THAT by capability would rewrite a stored "suspend" to "none" on the
	// next unrelated settings save. See idleaction.Actions' own doc comment.
	// The wire shape is unchanged (a flat array of ids), so nothing in the
	// frontend has to move to read it.
	reg.Add(http.MethodGet, "/api/idle-action/actions",
		"the end-of-queue actions this build can offer, in menu order",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, idleaction.Offered(a.IdleCapabilities()))
		})

	// The 3am button. It resolves the CONFIGURED command and reports what it
	// found without running it, which is what makes it safe to press on an
	// action that would otherwise put the machine to sleep.
	//
	// 200 EVEN WHEN THE PROBLEM IS NOT NONE, deliberately: a preflight that
	// answers 500 for "that program is not in this image" cannot be read by
	// the page that asked, and the whole value of this route is the sentence
	// it carries. The status code says "the check ran", not "the command is
	// fine".
	//
	// POST rather than GET for the reason routes_reconnect.go's own test
	// route is a POST: it is an action the operator takes, not a document a
	// page loads, and it must not end up in a browser's history, a prefetch
	// or a link somebody can be sent.
	reg.Add(http.MethodPost, "/api/idle-action/check",
		"resolve the configured end-of-queue command and report what would run, without running it",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.IdleCommandCheck(buildinfo.Deployment))
		})

	// Run the configured command once, now, exactly as the countdown would.
	//
	// REFUSED UNLESS THE CONFIGURED ACTION IS "command", with 409 rather than
	// 400: nothing about the request is malformed, the instance is simply not
	// in a state where this means anything. A "test" button that quits the
	// process or suspends the machine because that is what happened to be
	// configured is not a test, it is the thing itself with a reassuring
	// label - and the two host-level actions are precisely the ones nobody
	// can undo from the page they pressed it on.
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
