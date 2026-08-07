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
type bulkResult struct {
	Ids   []string `json:"ids"`
	Count int      `json:"count"`
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
	reg.Add(http.MethodPost, "/api/tasks/delete", "remove a selection from the list; files:true also erases what was downloaded",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids   []string `json:"ids"`
				Files bool     `json:"files"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			bulkDone(w, a.RemoveTasks(body.Ids, body.Files))
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
