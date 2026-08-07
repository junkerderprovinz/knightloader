package api

// The timetable, as the interface reads it.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerSchedule(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/schedule", "the timetable, what it says right now, and when that next changes",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.ScheduleState())
		})
}
