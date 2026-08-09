package api

// Unpacking, as work the caller can see and act on rather than something that
// happens to a download on its way to done.
//
// The two verbs here are the ones the automatic path never had. Extraction used
// to fire only as the tail of a finishing download, so an archive that failed on
// a wrong password or a full disk could not be tried again without fetching
// every volume a second time, and one that was going wrong could not be called
// off at all.

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
			// The reply carries the jobs, so a client that started three
			// extractions has their ids without asking again - and, when some of
			// the selection could not be unpacked, both halves of the answer at
			// once rather than a bare error that hides the ones that did start.
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
