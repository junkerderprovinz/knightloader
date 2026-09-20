package api

// The task list and everything that acts on one task or on an explicit set of
// them. The operations that act on a whole selection or on a whole class of
// tasks are in routes_bulk.go.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
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
			// Answers with what it did, because "nothing started" has three
			// causes: a halted queue, a link filter holding the named tasks,
			// or ids matching nothing.
			//
			// ByHand, because this route is the button and the only entry that
			// releases a halt somebody set themselves. The automatic callers
			// go through StartTasks and leave the switch where it was.
			writeJSON(w, a.StartTasksByHand(body.Ids))
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
	// reasons is what makes retry aimable. A list of forty failures is several
	// problems at once, and restarting all of them spends a hoster allowance
	// to re-prove that twenty-one links are still dead. The values are
	// core.Reason's, the empty string included, so the unclassified failures
	// stay reachable as a group.
	reg.Add(http.MethodPost, "/api/tasks/restart", "re-run finished or failed tasks from scratch (no ids = all errored); reasons narrows it to those causes",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids     []string      `json:"ids"`
				Reasons []core.Reason `json:"reasons"`
			}
			_ = decodeBody(r, &body) // empty/absent = restart all errored
			a.RestartTasksIn(body.Ids, body.Reasons)
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
	reg.Add(http.MethodPost, "/api/tasks/options", "per-task overrides: name, destination folder, archive password, comment, priority, unpacking, and the backend this task is pinned to. A field left out of the body is left as it is",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
				app.TaskOptions
				// Resolver pins these tasks to one download backend, by
				// resolver id ("torbox", "jd", "alldebrid#work"). An empty
				// string takes the pin off; a field left out of the body is
				// untouched, the contract every TaskOptions field follows.
				//
				// Beside the embedded struct rather than inside it because it
				// goes through app.PinResolver: the pin is an instruction to
				// the dispatcher, not a property of the file, and it
				// dispatches on the spot so a bad pin fails where the person
				// who typed it is looking.
				Resolver *string `json:"resolver"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			// Both halves are validated before either is applied, so a request
			// carrying a good rename and a backend this instance does not have
			// changes nothing. Applying in turn would leave the selection
			// half-edited behind a 400 naming one of the two problems.
			if body.Resolver != nil {
				if err := a.ResolverPinnable(*body.Resolver); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if err := a.SetTaskOptions(body.Ids, body.TaskOptions); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.Resolver != nil {
				if err := a.PinResolver(body.Ids, *body.Resolver); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
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
	reg.Add(http.MethodDelete, "/api/tasks/{id}", "remove one task; ?files=1 also deletes what was downloaded",
		func(w http.ResponseWriter, r *http.Request) {
			a.Remove(r.PathValue("id"), r.URL.Query().Get("files") == "1")
			w.WriteHeader(http.StatusNoContent)
		})
}
