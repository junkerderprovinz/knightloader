package api

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// passwordFirst refuses what hands out stored passwords or erases files for
// good while no password is set, and reports whether the request may go on.
// Without a password the guard lets in whoever reaches the port; with one, it
// has already asked for a session or a token. The desktop build opens no port,
// so there only its own window gets this far.
func passwordFirst(a *app.App, w http.ResponseWriter, action string) bool {
	if a.Auth.Enabled() || buildinfo.Deployment == "desktop" {
		return true
	}
	writeRefusal(w, http.StatusForbidden, "passwordNotSet", "set a password before "+action, nil)
	return false
}
