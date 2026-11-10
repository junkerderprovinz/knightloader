package api

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// afterDownloadState is what the After download station of the category settings
// shows: the ArrowLoop instances to pick a job from, and how the last
// hand-over of each category went.
type afterDownloadState struct {
	// Fortress is false while this instance is in none.
	Fortress bool                   `json:"fortress"`
	Engines  []afterdownload.Engine `json:"engines"`
	// Last is keyed by category id and leaves out those without a hand-over
	// in this process.
	Last map[string]afterdownload.Result `json:"last"`
}

func registerAfterDownload(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/afterdownload",
		"the ArrowLoop instances of the fortress with their jobs, for the After download station of a category, and how each category's last hand-over went",
		func(w http.ResponseWriter, r *http.Request) {
			st := afterDownloadState{
				Fortress: a.InFortress(),
				Engines:  a.AfterDownloadEngines(r.Context()),
				Last:     map[string]afterdownload.Result{},
			}
			for _, c := range a.Settings.Get().Categories {
				if res, ok := a.LastAfterDownload(c.ID); ok {
					st.Last[c.ID] = res
				}
			}
			writeJSON(w, st)
		})
}
