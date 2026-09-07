package api

// Operations on a whole selection. Every one of these exists because the
// interface does it to a selection, and a route that takes one id turns a
// hundred-row selection into a hundred requests, a hundred store writes and a
// hundred broadcasts — which is slow enough to look broken and, worse, can fail
// halfway and leave the list in a state nobody asked for.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// bulkResult is the same answer for every route here: what was actually
// touched, so the interface can report "12 removed" without re-fetching the
// world to work out which twelve.
//
// Undo and UndoMs are set by the delete route alone, and only when the removal
// left the files where they were. Both are omitempty, so every other route here
// answers exactly the two fields it always did.
type bulkResult struct {
	Ids   []string `json:"ids"`
	Count int      `json:"count"`
	// Undo is the token that puts this removal back, empty when there is nothing
	// to put back (see app.RemoveTasksUndoable for why erasing the files earns no
	// token).
	Undo string `json:"undo,omitempty"`
	// UndoMs is how long that token stays good, in milliseconds.
	//
	// Sent rather than hardcoded in the browser because the interface has to hold
	// the button up for exactly as long as the server still holds the rows. Two
	// copies of one duration drift, and both directions of the drift are bad: a
	// button that disappears early throws away an undo that would have worked, and
	// one that lingers answers "there is nothing to undo" to a press that looked
	// perfectly in time.
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
	// Deleting the rows and deleting the files are two different acts and they
	// are two different fields. The destructive one is never a default, is never
	// implied by the other, and the interface confirms it with the file count and
	// the byte total before it is sent.
	reg.Add(http.MethodPost, "/api/tasks/delete", "remove a selection from the list; files:true also erases what was downloaded. The answer carries an undo token while the rows can still be put back",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids   []string `json:"ids"`
				Files bool     `json:"files"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			// The one route here that does not go through bulkDone, because it is
			// the one with something more to say. Removing rows is the only verb
			// on this page that asks nothing first when no files are involved,
			// and the selection it acts on can include rows a filter is hiding -
			// so what comes back has to be enough to offer the press back.
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
			// An expired or unknown token answers with an empty list and 200, not
			// 404: the window closing is the ordinary end of a token's life, and
			// "nothing came back" is a sentence the interface can say to somebody
			// who pressed a second too late. A 404 would be reported as a broken
			// button instead.
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
