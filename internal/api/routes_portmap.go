package api

// Attempting a UPnP mapping for a torrent listen port.
//
// The route lives here, in its own file, rather than inside
// routes_torrents.go - it is registered under /api/torrents/portmap to match
// that file's own /api/torrents/* naming (see web/src/pages/settings/
// Torrents.tsx's own doc comment, written against this exact gap before it
// closed: the frontend already assumed this path and this response shape,
// grounded in portmap.Result's own json tags rather than guessed at from
// prose), but registerTorrents in routes_torrents.go is 11.5D's file for
// collector intake and this route is not collector intake. The Registry does
// not care which file calls reg.Add for a given path, only that registerAll
// calls the function that does - see routes.go's own doc comment - so a
// second file contributing to the /api/torrents/* namespace costs nothing
// and avoids two lanes writing the same file.
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
			// Validated here, ahead of portmap.AttemptPort's own identical
			// check, so a bad port names the field in a 400 rather than
			// surfacing as Attempt's generic "portmap: internal port 0 is
			// not 1-65535" - this route knows the field is called port,
			// portmap does not and should not have to.
			if body.Port < 1 || body.Port > 65535 {
				http.Error(w, "port must be 1-65535", http.StatusBadRequest)
				return
			}

			// The same gateway reconnect already knows about, if the user
			// pinned one for a network whose multicast search is filtered:
			// it is the same router either way, and a second, unrelated pin
			// for this one button is not a field the settings page has
			// anywhere to show.
			pinned := a.Settings.Get().Reconnect.UPnPLocation

			res, err := portmap.AttemptPort(r.Context(), portmap.Request{
				InternalPort: body.Port,
				Location:     pinned,
				HTTP:         httpx.New(httpx.Options{}),
			})
			if err != nil {
				// Only a caller mistake reaches this - see AttemptPort's own
				// doc comment - and body.Port is already validated above, so
				// this is unreachable in practice and answered rather than
				// swallowed only as a defensive backstop.
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Result already carries the json tags a page wants
			// (outcome/reason/detail/...), so it is written back as-is.
			writeJSON(w, res)
		})
}
