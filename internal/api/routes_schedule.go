package api

// The timetable, as the interface reads it and as it saves it.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

func registerSchedule(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/schedule", "the timetable, what it says right now, and when that next changes",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.ScheduleState())
		})

	// The whole ordered list in one PUT rather than per-row routes: entries
	// apply in order and the last write to a field wins, which is what lets a
	// narrow "resume for lunch" sit inside a broad "pause all night", so
	// separate moves from two browsers could scramble the order.
	//
	// The rows stay in settings.Settings, where the module registry reads
	// them. Unlike PUT /api/settings this route reports every bad row by
	// position, and it writes only the Schedule field, so it never replays a
	// stale copy of other settings. A stale general settings save can still put
	// an old timetable back, as it can for rules or connections.
	reg.Add(http.MethodPut, "/api/schedule",
		"replace the timetable; a row Validate refuses is reported by position and reason, not a flat 400",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Entries []schedule.Entry `json:"entries"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Every row is checked, so the editor can mark all mistakes at once.
			if errs := invalidScheduleRows(body.Entries); len(errs) > 0 {
				writeJSONStatus(w, http.StatusBadRequest, scheduleValidationError{Errors: errs})
				return
			}
			// ApplySettings re-arms the runner and pushes a limit change to JD
			// live; writing the store directly would not.
			next := a.Settings.Get()
			next.Schedule = body.Entries
			if _, err := a.ApplySettings(next); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, a.ScheduleState())
		})
}

// scheduleRowError is one row a save refused, by 1-based position, with
// schedule.Entry.Validate's reason.
type scheduleRowError struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

// scheduleValidationError is the body of a refused PUT /api/schedule. Unlike
// the {error, code, params} envelope it names every failing row.
type scheduleValidationError struct {
	Errors []scheduleRowError `json:"errors"`
}

// invalidScheduleRows runs every row through Validate and names each one that
// fails.
func invalidScheduleRows(entries []schedule.Entry) []scheduleRowError {
	var errs []scheduleRowError
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			errs = append(errs, scheduleRowError{Row: i + 1, Error: err.Error()})
		}
	}
	return errs
}
