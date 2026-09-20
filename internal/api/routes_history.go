package api

// The download history: what this instance has fetched, which is not what is
// in the list. A route of its own rather than a flag on the task list, because
// the list is a working set, cleared by hand and trimmed by retention, while
// "did I already download this" has to survive somebody pressing "clear
// finished".

import (
	"net/http"
	"strconv"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// defaultHistoryPage bounds what a client that names no limit gets: the table
// runs into the thousands, and rendering all of it by default gets slower the
// longer the instance has been in use.
const defaultHistoryPage = 500

// historyLimit reads ?limit=, refusing nothing: a value that is not a number
// is a client that meant the default. Zero and below mean everything, which is
// what an export asks for.
func historyLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultHistoryPage
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultHistoryPage
	}
	return n
}

func registerHistory(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/history", "what this instance has finished downloading, newest first; ?limit=0 for all of it",
		func(w http.ResponseWriter, r *http.Request) {
			entries, err := a.History(historyLimit(r))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, entries)
		})

	reg.Add(http.MethodDelete, "/api/history", "empty the download history; the task list and the files are untouched",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.ClearHistory(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
