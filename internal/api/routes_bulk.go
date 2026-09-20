package api

// Operations on a whole selection. A route that takes one id turns a
// hundred-row selection into a hundred requests, store writes and broadcasts,
// which is slow enough to look broken and can fail halfway.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// bulkResult is the answer for every route here: what was actually touched, so
// the interface can report "12 removed" without re-fetching the list to work
// out which twelve. Only the delete route sets Undo and UndoMs, and only when
// the removal left the files where they were.
type bulkResult struct {
	Ids   []string `json:"ids"`
	Count int      `json:"count"`
	// Undo is the token that puts this removal back, empty when there is nothing
	// to put back (see app.RemoveTasksUndoable for why erasing the files earns no
	// token).
	Undo string `json:"undo,omitempty"`
	// UndoMs is how long that token stays good, in milliseconds. Sent rather
	// than hardcoded in the browser so the button is up for exactly as long as
	// the server still holds the rows.
	UndoMs int64 `json:"undoMs,omitempty"`
}

func bulkDone(w http.ResponseWriter, ids []string) {
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, bulkResult{Ids: ids, Count: len(ids)})
}

func registerBulk(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/tasks/enabled", "switch a selection of links on or off",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids     []string `json:"ids"`
				Enabled bool     `json:"enabled"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			bulkDone(w, a.SetEnabled(body.Ids, body.Enabled))
		})
	reg.Add(http.MethodPost, "/api/tasks/hold", "park a selection, or let it go again",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids  []string `json:"ids"`
				Hold bool     `json:"hold"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			bulkDone(w, a.SetHold(body.Ids, body.Hold))
		})
	reg.Add(http.MethodPost, "/api/tasks/force", "mark a selection to run ahead of the limits",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids    []string `json:"ids"`
				Forced bool     `json:"forced"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			bulkDone(w, a.SetForced(body.Ids, body.Forced))
		})
	// Deleting the rows and deleting the files are two fields, so the
	// destructive one is never implied by the other. The interface confirms it
	// with the file count and the byte total before it is sent.
	reg.Add(http.MethodPost, "/api/tasks/delete", "remove a selection from the list; files:true also erases what was downloaded. The answer carries an undo token while the rows can still be put back",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids   []string `json:"ids"`
				Files bool     `json:"files"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			// Not bulkDone: removing rows asks nothing first when no files are
			// involved, and the selection can include rows a filter is hiding,
			// so the answer has to carry enough to offer the undo.
			removed, undo := a.RemoveTasksUndoable(body.Ids, body.Files)
			if removed == nil {
				removed = []string{}
			}
			out := bulkResult{Ids: removed, Count: len(removed), Undo: undo}
			if undo != "" {
				out.UndoMs = app.UndoWindow.Milliseconds()
			}
			writeJSON(w, out)
		})
	reg.Add(http.MethodPost, "/api/tasks/undo-delete", "put back the rows one removal took, while its token is still good",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Token string `json:"token"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// An expired or unknown token answers with an empty list and 200.
			// The window closing is the ordinary end of a token's life, and a
			// 404 would be reported as a broken button.
			bulkDone(w, a.UndoRemove(body.Token))
		})
	reg.Add(http.MethodGet, "/api/cleanup/{class}", "which tasks a cleanup class would take, without taking them",
		func(w http.ResponseWriter, r *http.Request) {
			ids, err := a.CleanupPreview(app.CleanupClass(r.PathValue("class")))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			bulkDone(w, ids)
		})
	reg.Add(http.MethodPost, "/api/cleanup/{class}", "remove every task in a cleanup class; files=1 also erases what was downloaded",
		func(w http.ResponseWriter, r *http.Request) {
			ids, err := a.Cleanup(app.CleanupClass(r.PathValue("class")), r.URL.Query().Get("files") == "1")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			bulkDone(w, ids)
		})
}
