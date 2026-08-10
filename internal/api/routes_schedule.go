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

	// PUT, not the sub-resource shape ("/api/schedule/{id}") a row-level CRUD
	// endpoint would suggest: order is load-bearing here exactly as it is for a
	// rule set (schedule.Schedule.At applies every covering entry in order and
	// the last write to a field wins, which is what lets a narrow "resume for
	// lunch" sit inside a broad "pause all night"), so the only edit that is
	// ever safe to describe is "here is the whole ordered list now" - a
	// move-up/move-down pair of routes could have two browsers interleave into
	// a scrambled order with neither request ever failing.
	//
	// WHY THIS ROUTE EXISTS, given that every sibling field of the settings
	// document explicitly decided against one: routes_rules.go, routes_
	// connections.go and routes_reconnect.go each carry a comment refusing a
	// second write path for their own field of settings.Settings, on the same
	// reasoning - the field is served by GET /api/settings, saved by PUT
	// /api/settings, and a second writer is a second place for that round trip
	// to be gotten wrong. That reasoning is sound and this route does not
	// contradict it: it does not compete with PUT /api/settings for ownership
	// of Schedule, because it never accepts or replays a client's copy of any
	// field but this one (see below). What it adds on top - reporting which
	// timetable ROW is wrong and why, rather than the single flat sentence PUT
	// /api/settings's validateRows stops at on the first bad row - is worth a
	// dedicated door for a table-shaped field edited row by row, the same way
	// /api/rules/preview and /api/connections/test are dedicated doors onto
	// fields that also live in the settings document.
	//
	// WHERE THE ROWS LIVE, the question this route's brief asked to be settled
	// on purpose rather than by default: they stay inside settings.Settings
	// (settings.go's Schedule field), not in a schedule.json of their own.
	// Two facts earned that, both checked in this tree rather than assumed
	// from a plan:
	//
	//   1. Nothing in this codebase actually has "its own small store" for a
	//      field-of-settings reason. rules.json was proposed for exactly this
	//      worry and was never built - routes_rules.go's own comment explains
	//      why the wiring landed the opposite way. Connections and Reconnect
	//      made the same call. Forking Schedule out would make it the only
	//      field in the app with this shape, for a race no worse than the one
	//      every sibling field already carries (below) - a new precedent
	//      bought for a marginal gain.
	//   2. routes_features.go's module registry reads s.Schedule directly
	//      (Enabled: len(s.Schedule) > 0, and the "scheduler" case of
	//      setFeature) to drive the Modules page's switch and its parked-value
	//      restore. Moving Schedule out of Settings means that file changes
	//      too - a file this wave's lane table does not hand to 10A, and a
	//      second wave 10 agent has as much reason to be touching it as this
	//      one does. Keeping the field where setFeature already expects it
	//      avoids that collision entirely rather than asking the plan to
	//      referee it.
	//
	// THE RACE ITSELF, named rather than left silent: PUT /api/settings still
	// replays a browser's whole draft on every save (web/src/pages/Settings.tsx
	// - "every edit spreads the object rather than rebuilding it"), so a
	// second settings tab left open since before a schedule edit can still
	// save over it with the copy of Schedule it loaded. That risk is real and
	// this route does not remove it - it is the exact risk Rules, Connections
	// and Reconnect already carry today, unaddressed, for the same reason.
	// What this route does buy, concretely: the settings sub-page this wave
	// adds (web/src/pages/settings/Schedule.tsx) never joins the shared
	// settings draft at all - it reads and writes ONLY through this route,
	// against whatever is live on the server at the moment of the click, the
	// same way setFeature already does for this one field ("read current,
	// write the one field, ApplySettings" - see the handler below). So a
	// timetable save can never itself be the stale side of that race, and it
	// can never carry along some unrelated field a general Settings save was
	// about to write on the strength of a copy loaded minutes or hours
	// earlier. A stale GENERAL save can still, in principle, put an old
	// timetable back - the same as it can put back an old rule set or an old
	// proxy list - and the honest reason to accept that here rather than
	// building the merge machinery Rules/Connections/Reconnect also lack: a
	// timetable is the kind of thing one person sets up once and revisits
	// occasionally, not a field two people fight over inside the same minute.
	reg.Add(http.MethodPut, "/api/schedule",
		"replace the timetable; a row Validate refuses is reported by position and reason, not a flat 400",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Entries []schedule.Entry `json:"entries"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Every row is checked, not just the first: the editor shows the whole
			// table at once, and answering about one mistake at a time when three
			// rows are wrong is the generic-400 experience this route exists to
			// replace with something a form can point at.
			if errs := invalidScheduleRows(body.Entries); len(errs) > 0 {
				writeJSONStatus(w, http.StatusBadRequest, scheduleValidationError{Errors: errs})
				return
			}
			// Read current, write the one field, ApplySettings - not a client-
			// supplied whole Settings object. See the doc comment above for why
			// this shape, and setFeature's "scheduler" case for the precedent: it
			// is the same three steps, because ApplySettings is what re-arms
			// a.sched with the new timetable and pushes a limit change to JD live;
			// writing the store directly would persist the edit and leave the
			// runner applying the old one until the next unrelated boundary.
			next := a.Settings.Get()
			next.Schedule = body.Entries
			if _, err := a.ApplySettings(next); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, a.ScheduleState())
		})
}

// scheduleRowError is one row a save refused, by position (1-indexed, so it
// reads as "row 2" rather than the array index nobody but the form should
// know about) and schedule.Entry.Validate's own reason.
type scheduleRowError struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

// scheduleValidationError is the body of a refused PUT /api/schedule. A
// dedicated shape rather than routes_settings.go's {error, code, params}
// envelope, because that one names a single field and this refusal is
// per-row - the form needs to know WHICH rows to mark, and a flat sentence
// cannot say that.
type scheduleValidationError struct {
	Errors []scheduleRowError `json:"errors"`
}

// invalidScheduleRows runs every row through its own Validate and names each
// one that fails, so a table of ten windows with one typo is refused with a
// pointer at the typo rather than at "the schedule".
func invalidScheduleRows(entries []schedule.Entry) []scheduleRowError {
	var errs []scheduleRowError
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			errs = append(errs, scheduleRowError{Row: i + 1, Error: err.Error()})
		}
	}
	return errs
}
