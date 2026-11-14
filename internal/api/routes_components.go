package api

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerComponents(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/components/update",
		"how far Update now has come: yt-dlp, ffmpeg, Deno, Java and JDownloader, each with what its update came to",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.ComponentUpdateState())
		})

	// The run outlives the request: ffmpeg alone can take many minutes over a
	// slow line, and GET follows it.
	reg.Add(http.MethodPost, "/api/components/update",
		"ask after every component at once and fetch what is newer, whatever the automatic update would do",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSONStatus(w, http.StatusAccepted, a.StartComponentUpdate())
		})

	reg.Add(http.MethodGet, "/api/components/qbittorrent",
		"the qBittorrent this instance runs: its state, the version running, the version held and the versions to choose from",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.QBittorrentStatus())
		})

	reg.Add(http.MethodPost, "/api/components/qbittorrent/start",
		"fetch and start the qBittorrent this instance runs again, after a try that failed",
		func(w http.ResponseWriter, r *http.Request) {
			a.StartQBittorrent()
			writeJSONStatus(w, http.StatusAccepted, a.QBittorrentStatus())
		})
}
