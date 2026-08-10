package api

// The end-of-queue action: whether the wait queue is idle right now, whether
// a cancellable countdown is running, and calling one off. The action itself
// is configured through the ordinary settings document (Settings.IdleAction,
// internal/settings/settings_idleaction.go) - the same PUT /api/settings
// every other plain switch and number on the Downloads settings page already
// goes through. This file only serves what that document alone cannot
// answer: live state, and cancelling a countdown in progress.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
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
	// cannot carry out is a control that does nothing when it is pressed -
	// see internal/idleaction.Actions' own doc comment for why the list is
	// exactly two entries today.
	reg.Add(http.MethodGet, "/api/idle-action/actions",
		"the end-of-queue actions this build can offer, in menu order",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, idleaction.Actions())
		})
}
