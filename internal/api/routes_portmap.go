package api

// Attempting a UPnP mapping for a torrent listen port. Registered under
// /api/torrents/portmap to match routes_torrents.go's naming, but kept in its
// own file because that one is about collector intake. The Registry does not
// care which file calls reg.Add for a path, only that registerAll reaches the
// function that does.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/portmap"
)

func registerPortmap(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/torrents/portmap",
		"attempt a UPnP mapping for a torrent listen port on both TCP and UDP, reporting confirmed, unconfirmed or failed - never a bare success",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Port int `json:"port"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Validated ahead of portmap.AttemptPort's own check so the
			// refusal names the field. This route knows it is called port;
			// portmap does not.
			if body.Port < 1 || body.Port > 65535 {
				http.Error(w, "port must be 1-65535", http.StatusBadRequest)
				return
			}

			// Reuses the gateway reconnect was pinned to, for a network whose
			// multicast search is filtered. It is the same router either way,
			// and the settings page has nowhere to show a second pin.
			pinned := a.Settings.Get().Reconnect.UPnPLocation

			res, err := portmap.AttemptPort(r.Context(), portmap.Request{
				InternalPort: body.Port,
				Location:     pinned,
				HTTP:         httpx.New(httpx.Options{}),
			})
			if err != nil {
				// AttemptPort only errors on a malformed request, which the
				// port check above has already ruled out.
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, res)
		})
}
