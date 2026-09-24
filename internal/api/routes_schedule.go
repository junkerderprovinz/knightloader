package api

// The timetable, as the interface reads it and as it saves it.

import (
	"errors"
	"fmt"
	"net/http"
	"time"

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

	// A route of its own rather than a field of the PUT above, so a table saved
	// from a second browser can neither end a suspension nor start one.
	reg.Add(http.MethodPut, "/api/schedule/suspend",
		"set the whole timetable aside for some minutes, until a given moment or until it is lifted, leaving the rows as they are",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Minutes int        `json:"minutes"`
				Until   *time.Time `json:"until"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			until, err := suspendEnd(time.Now(), body.Minutes, body.Until)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := a.SuspendSchedule(until); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, app.ErrSuspendEnded) {
					status = http.StatusBadRequest
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, a.ScheduleState())
		})

	reg.Add(http.MethodDelete, "/api/schedule/suspend", "let the timetable apply again at once",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.ResumeSchedule(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, a.ScheduleState())
		})
}

// maxSuspendMinutes bounds a suspension given as a length. A month is far past
// any "for a while", and an open end says "until I lift it" plainly.
const maxSuspendMinutes = 31 * 24 * 60

// suspendEnd turns a request into the moment the timetable applies again: a
// length from the server's own clock, so a browser whose clock is off cannot
// shorten it, or an instant the browser chose, such as its own midnight.
// Neither is an open end, the zero time.
func suspendEnd(now time.Time, minutes int, until *time.Time) (time.Time, error) {
	switch {
	case minutes != 0 && until != nil:
		return time.Time{}, errors.New("give minutes or until, not both")
	case minutes < 0 || minutes > maxSuspendMinutes:
		return time.Time{}, fmt.Errorf("minutes has to be between 1 and %d", maxSuspendMinutes)
	case minutes > 0:
		return now.Add(time.Duration(minutes) * time.Minute), nil
	case until != nil:
		return *until, nil
	}
	return time.Time{}, nil
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
