package app

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/parleyport/kit/signin"
)

// signInEnding is what ends KnightLoader's own ways in after the kit ended
// its own, set once the routes exist.
type signInEnding = func(ctx context.Context, what signin.Ending) error

var errNoRoutesYet = errors.New("the routes are not up yet")

// OnSignInEnded sets what ends KnightLoader's own ways in: the MCP clients'
// tokens, the live streams, the pairing group after a reset. The kit keeps
// an ending until it ran, so one it hands over before this is set runs then.
func (a *App) OnSignInEnded(end signInEnding) { a.signInEnd.Store(&end) }

func (a *App) signInEnded(ctx context.Context, what signin.Ending) error {
	end := a.signInEnd.Load()
	if end == nil {
		return errNoRoutesYet
	}
	return (*end)(ctx, what)
}

// publicURL is the first known address in full, which passkeys and sign-in
// services are bound to, by scheme and host alone.
func (a *App) publicURL() *url.URL {
	for _, d := range a.Settings.Get().KnownDomains {
		u, err := url.Parse(strings.TrimSpace(d))
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			continue
		}
		return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}
	}
	return nil
}
