package api

// The fortress status: how this instance is doing and what happened, in the
// format every app of a fortress answers, for the other members and for
// dashboards such as Homepage or Homarr.
//
// The routes are open to the guard because they are never open to a caller:
// they check their own credential even on an instance without a password,
// where the guard lets everything through. A member of the group, a browser
// session or an API token with the status or read right gets in.

import (
	"net/http"

	"github.com/junkerderprovinz/parleyport/kit/signin"
	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/fortress"
)

func registerFortress(reg *Registry, a *app.App) {
	h := status.Handler(fortress.Source{Events: a.FortressLog, Read: a.FortressSnapshot}, fortressAccess(a))
	// Every method, so the handler answers anything but GET with the 405 the
	// contract asks for instead of the /api/ catch-all's 404.
	reg.AddOpen(AnyMethod, status.StatusPath,
		"how this instance is doing, what needs you and what is running, in the fortress status format; "+
			"needs a member of the group, a session or an API token with the status right, also without a password",
		h.ServeHTTP)
	reg.AddOpen(AnyMethod, status.LogPath,
		"what happened on this instance, oldest first, from ?after=<cursor> with at most ?limit=200 entries; "+
			"the same credentials as the status",
		h.ServeHTTP)
}

// fortressAccess decides who may read the fortress status.
func fortressAccess(a *app.App) func(*http.Request) error {
	return func(r *http.Request) error {
		if fromGroup(r) {
			return nil
		}
		if v, ok := signin.From(r.Context()); ok && v.Via != signin.ViaAnyone && v.Via != signin.ViaToken {
			return nil
		}
		// Asked of the token itself, since an open app lets everybody in
		// and a dashboard still needs one.
		tok, ok := anyToken(a, bearerToken(r))
		if !ok {
			return status.ErrUnauthenticated
		}
		if !tok.Has(apitoken.ScopeStatus) && !tok.Has(apitoken.ScopeRead) {
			return status.ErrForbidden
		}
		return nil
	}
}
