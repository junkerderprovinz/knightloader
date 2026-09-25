package api

// What the event programs are doing, and the names their arguments may use.
//
// The rows get no write route here. EventPrograms is written by PUT and PATCH
// /api/settings like the event targets beside it, so a program can be set only
// by whoever may change the settings, and the redact-and-merge round trip that
// keeps the command line out of the browser has one way in.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// eventProgramRow is one configured program as the page sees it: the stored
// row joined onto the live health, and a check of the program that runs
// nothing. Never the path or the arguments, which the settings serve as stars.
type eventProgramRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Check is what resolving the stored program found, without starting it:
	// empty when it would start, "notFound" or "notExecutable" when it would
	// not, "empty" when there is no program yet.
	Check idleaction.Problem `json:"check,omitempty"`
	// The rest is eventprog.Health, absent where nothing has run since this
	// process started.
	LastStart      time.Time          `json:"lastStart,omitzero"`
	LastEvent      script.Trigger     `json:"lastEvent,omitempty"`
	LastOK         time.Time          `json:"lastOk,omitzero"`
	LastProblem    idleaction.Problem `json:"lastProblem,omitempty"`
	LastExitCode   int                `json:"lastExitCode"`
	LastOutput     string             `json:"lastOutput,omitempty"`
	LastDurationMS int64              `json:"lastDurationMs"`
	Runs           int                `json:"runs"`
	Failed         int                `json:"failed"`
	Dropped        int                `json:"dropped"`
}

// enabledEventPrograms is how many programs can start on an event, which is not
// how many rows exist.
func enabledEventPrograms(s settings.Settings) int {
	n := 0
	for _, p := range s.EventPrograms {
		if p.Enabled && len(p.Triggers) > 0 {
			n++
		}
	}
	return n
}

func registerEventPrograms(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/eventprograms",
		"every configured event program with whether its program resolves, when it last ran, how that ended and how many events were dropped",
		func(w http.ResponseWriter, r *http.Request) {
			live := map[string]eventprog.Health{}
			for _, h := range a.EventProgramHealth() {
				live[h.ProgramID] = h
			}
			programs := a.Settings.Get().EventPrograms
			rows := make([]eventProgramRow, 0, len(programs))
			for _, p := range programs {
				row := eventProgramRow{ID: p.ID, Name: p.Name, Enabled: p.Enabled, Check: p.Command.Preflight(nil).Problem}
				if h, ok := live[p.ID]; ok {
					row.LastStart, row.LastEvent, row.LastOK = h.LastStart, h.LastEvent, h.LastOK
					row.LastProblem, row.LastExitCode, row.LastOutput = h.LastProblem, h.LastExitCode, h.LastOutput
					row.LastDurationMS, row.Runs, row.Failed, row.Dropped = h.LastDurationMS, h.Runs, h.Failed, h.Dropped
				}
				rows = append(rows, row)
			}
			writeJSON(w, rows)
		})

	reg.Add(http.MethodGet, "/api/eventprograms/placeholders",
		"the placeholder names a program's arguments may use, with the scope and the triggers that carry each one",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, eventprog.Placeholders())
		})
}
