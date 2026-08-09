package api

// The task list and everything that acts on one task or on an explicit set of
// them. The operations that act on a whole selection or on a whole class of
// tasks are in routes_bulk.go.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerTasks(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/tasks", "every task, oldest first",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Tasks())
		})
	reg.Add(http.MethodPost, "/api/tasks/start", "move collected tasks into the download queue (no ids = all collected)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
			}
			_ = decodeBody(r, &body) // empty/absent = start all collected
			a.StartTasks(body.Ids)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/package", "move tasks into a package (an empty name ungroups them)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids     []string `json:"ids"`
				Package string   `json:"package"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			a.SetPackage(body.Ids, body.Package)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/restart", "re-run finished or failed tasks from scratch (no ids = all errored)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
			}
			_ = decodeBody(r, &body) // empty/absent = restart all errored
			a.RestartTasks(body.Ids)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/recheck", "ask the hosts again whether these links are still there (no ids = the whole collector)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
			}
			_ = decodeBody(r, &body)    // empty/absent = recheck all collected
			go a.RecheckTasks(body.Ids) // probing hosts can take a while
			w.WriteHeader(http.StatusAccepted)
		})
	reg.Add(http.MethodPost, "/api/tasks/priority", "lift or drop tasks in the wait queue",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids      []string `json:"ids"`
				Priority int      `json:"priority"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			a.SetPriority(body.Ids, body.Priority)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/move", `move tasks to the "top" or "bottom" of the wait queue`,
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids   []string `json:"ids"`
				Where string   `json:"where"` // "top" or "bottom"
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			a.MoveTasks(body.Ids, body.Where)
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/options", "per-task overrides: name, destination folder, archive password, comment, priority, unpacking. A field left out of the body is left as it is",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
				app.TaskOptions
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			if err := a.SetTaskOptions(body.Ids, body.TaskOptions); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/{id}/pause", "pause one running or queued task",
		func(w http.ResponseWriter, r *http.Request) {
			a.Pause(r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		})
	reg.Add(http.MethodPost, "/api/tasks/{id}/resume", "put one paused task back in the queue",
		func(w http.ResponseWriter, r *http.Request) {
			a.Resume(r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		})
	// Removing a task takes it off the list. ?files=1 additionally deletes what
	// was downloaded — an explicit, opt-in act.
	reg.Add(http.MethodDelete, "/api/tasks/{id}", "remove one task; ?files=1 also deletes what was downloaded",
		func(w http.ResponseWriter, r *http.Request) {
			a.Remove(r.PathValue("id"), r.URL.Query().Get("files") == "1")
			w.WriteHeader(http.StatusNoContent)
		})
}
