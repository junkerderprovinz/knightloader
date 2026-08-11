package api

// The script editor's REST surface (build-plan.md's Wave 11, 11B) - CRUD
// over internal/script's own store, "run now" for both the editor's Test
// Run button and the task row's manual action (components/ScriptActions.tsx),
// and the trigger vocabulary the editor's picker is built from. See
// internal/script's own package doc comment, "wiring this in is deliberately
// not this package's job", and web/src/lib/scripts.ts's file doc comment for
// the exact wire shape this file was written against - every route, verb and
// field name below matches that file's proposal, because it was checked
// field-for-field against internal/script/script.go's own JSON tags rather
// than guessed.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func registerScripts(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/scripts", "every saved script, sorted by name",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Scripts.ListScripts())
		})

	reg.Add(http.MethodGet, "/api/scripts/triggers", "the trigger vocabulary this build actually fires, for the editor's picker",
		func(w http.ResponseWriter, r *http.Request) {
			all := script.AllTriggers()
			out := make([]string, len(all))
			for i, t := range all {
				out[i] = string(t)
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/scripts", "save a new script; refused if it does not compile or fails validation",
		func(w http.ResponseWriter, r *http.Request) {
			var body scriptInput
			if !decodeJSON(w, r, &body) {
				return
			}
			saved, err := a.Scripts.SaveScript(body.toScript(""))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSONStatus(w, http.StatusCreated, saved)
		})

	reg.Add(http.MethodPut, "/api/scripts/{id}", "save changes to one script; refused if it does not compile or fails validation",
		func(w http.ResponseWriter, r *http.Request) {
			var body scriptInput
			if !decodeJSON(w, r, &body) {
				return
			}
			saved, err := a.Scripts.SaveScript(body.toScript(r.PathValue("id")))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, saved)
		})

	reg.Add(http.MethodDelete, "/api/scripts/{id}", "remove one saved script",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if _, ok := a.Scripts.GetScript(id); !ok {
				http.Error(w, "script not found", http.StatusNotFound)
				return
			}
			if err := a.Scripts.DeleteScript(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

	reg.Add(http.MethodPost, "/api/scripts/{id}/run",
		"run one saved script immediately, ignoring its own enabled flag and trigger - the editor's Test Run button and the task row's manual action both call this",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if _, ok := a.Scripts.GetScript(id); !ok {
				http.Error(w, "script not found", http.StatusNotFound)
				return
			}
			// An absent body means "no task", the same tolerance decodeBody's
			// own doc comment gives for the routes where that is a valid
			// request rather than a malformed one - the toolbar-placed Test
			// Run button has no task to send at all.
			var body struct {
				TaskID string `json:"taskId"`
			}
			_ = decodeBody(r, &body)

			var task *script.TaskView
			if body.TaskID != "" {
				tv, ok := a.ScriptTask(body.TaskID)
				if !ok {
					http.Error(w, "task not found", http.StatusNotFound)
					return
				}
				task = &tv
			}
			res, err := a.Scripts.RunNow(r.Context(), id, task, a.ScriptQueue())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, res)
		})
}

// scriptInput is POST/PUT's body - ScriptInput's exact fields
// (web/src/lib/scripts.ts), decoded into its own narrow struct rather than
// straight into script.Script so a request body can never set ID,
// CreatedAt or UpdatedAt itself; toScript is the one place those three are
// filled in, always from the server's own side (an empty id for a create,
// the URL's id for an update - internal/script/store.go's save then decides
// CreatedAt from whichever of the two that turns out to be).
type scriptInput struct {
	Name      string         `json:"name"`
	Trigger   script.Trigger `json:"trigger"`
	Enabled   bool           `json:"enabled"`
	Code      string         `json:"code"`
	TimeoutMS int            `json:"timeoutMs,omitempty"`
}

func (in scriptInput) toScript(id string) script.Script {
	return script.Script{
		ID:        id,
		Name:      in.Name,
		Trigger:   in.Trigger,
		Code:      in.Code,
		Enabled:   in.Enabled,
		TimeoutMS: in.TimeoutMS,
	}
}
