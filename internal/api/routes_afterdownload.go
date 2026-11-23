package api

import (
	"errors"
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

	reg.Add(http.MethodGet, "/api/afterdownload/engines/{id}/targets",
		"what one ArrowLoop instance of the fortress can copy to, for a new job of the After download station",
		func(w http.ResponseWriter, r *http.Request) {
			list, err := a.AfterDownloadDestinations(r.Context(), r.PathValue("id"))
			if err != nil {
				refuseAfterDownload(w, err)
				return
			}
			writeJSON(w, list)
		})

	reg.Add(http.MethodPost, "/api/afterdownload/jobs",
		"create a job in an ArrowLoop instance of the fortress that starts from a category's folder: category, member, name, target and mode (mirror, sync or move)",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Category string `json:"category"`
				Member   string `json:"member"`
				Name     string `json:"name"`
				Target   string `json:"target"`
				Mode     string `json:"mode"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			job, err := a.CreateAfterDownloadJob(r.Context(), body.Category, body.Member, body.Name, body.Target, body.Mode)
			if err != nil {
				refuseAfterDownload(w, err)
				return
			}
			writeJSON(w, map[string]string{"job": job})
		})
}

// refuseAfterDownload answers a failed call to ArrowLoop with a code the
// station words, afterDownload. and one of afterdownload.Code's.
func refuseAfterDownload(w http.ResponseWriter, err error) {
	if errors.Is(err, app.ErrNoFolder) {
		writeRefusal(w, http.StatusBadRequest, "afterDownload.noFolder", err.Error(), nil)
		return
	}
	code := afterdownload.Code(err)
	status := http.StatusBadGateway
	switch code {
	case afterdownload.CodeNoFortress, afterdownload.CodeGone:
		status = http.StatusNotFound
	case afterdownload.CodeNameTaken:
		status = http.StatusConflict
	case afterdownload.CodeBadRequest, afterdownload.CodeNoSuchTarget, afterdownload.CodeSourceNotMounted:
		status = http.StatusBadRequest
	case afterdownload.CodePermissionOff:
		status = http.StatusForbidden
	}
	writeRefusal(w, status, "afterDownload."+code, err.Error(), nil)
}
