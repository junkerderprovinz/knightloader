package api

// Unpacking, as work the caller can see and act on rather than something that
// happens to a download on its way to done. Without the two verbs here, an
// archive that failed on a wrong password or a full disk could only be retried
// by fetching every volume again.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerExtract(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/extract", "every unpacking job, oldest first",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.ExtractJobs())
		})
	reg.Add(http.MethodPost, "/api/extract/start", "unpack these finished downloads now, whatever the unpacking switch says",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Ids []string `json:"ids"`
			}
			if !decodeJSON(w, r, &body) || !requireIDs(w, body.Ids) {
				return
			}
			// The reply carries the jobs so a partly refused selection still
			// shows which extractions did start.
			err := a.StartExtraction(body.Ids)
			if err != nil {
				writeJSONStatus(w, http.StatusMultiStatus, map[string]any{
					"jobs":    a.ExtractJobs(),
					"refused": err.Error(),
				})
				return
			}
			writeJSON(w, a.ExtractJobs())
		})
	reg.Add(http.MethodPost, "/api/extract/{id}/abort", "call off one unpacking and remove the half-written output",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.AbortExtraction(r.PathValue("id")); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}
