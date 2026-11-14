package api

// The sign-in browser (internal/signinbrowser), for signing in to a site
// without the extension: the image's Chromium in the container, an installed
// browser on the desktop. Every route needs admin and none is open to the
// phrase group.
//
//	GET  /api/signinbrowser            whether a browser is there to sign in with, its name, and the site open in it
//	POST /api/signinbrowser/{id}/open  open it on a site's login page: {"width", "height", "scale"}
//	GET  /api/signinbrowser/stream     WebSocket: JPEG frames down as binary messages, the
//	                                   page's state down and the user's input up as JSON
//	POST /api/signinbrowser/close      close it, storing the session it holds
//
// What travels down never carries the session itself.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/signinbrowser"
)

// signInBrowserRecheck is how often an open stream checks that the session or
// token which opened it would still be let in, so signing everybody out ends
// the window's control too.
const signInBrowserRecheck = 5 * time.Second

func registerSignInBrowser(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/signinbrowser",
		"whether a browser is there to sign in with, which one, and the site open in it",
		func(w http.ResponseWriter, r *http.Request) {
			out := map[string]any{"available": a.SignInBrowserAvailable(), "browser": "", "site": a.SignInBrowser().OpenSite()}
			if out["available"] == true {
				out["browser"] = a.SignInBrowserName()
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodPost, "/api/signinbrowser/{id}/open",
		"open the sign-in browser on a site's login page, sized to the window that shows it",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Width  int     `json:"width"`
				Height int     `json:"height"`
				Scale  float64 `json:"scale"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			st, err := a.OpenSignInBrowser(r.Context(), r.PathValue("id"), body.Width, body.Height, body.Scale)
			switch {
			case errors.Is(err, app.ErrNoSuchSite):
				http.Error(w, err.Error(), http.StatusNotFound)
			case errors.Is(err, app.ErrNoSignInBrowser):
				http.Error(w, err.Error()+": sign in to the site in your own browser and let the browser extension "+
					"hand the session over.", http.StatusServiceUnavailable)
			case err != nil:
				http.Error(w, "the sign-in browser did not start: "+err.Error(), http.StatusBadGateway)
			default:
				writeJSON(w, st)
			}
		})

	reg.Add(http.MethodGet, "/api/signinbrowser/stream",
		"WebSocket of the open sign-in browser: its frames and state down, the user's pointer and keys up",
		func(w http.ResponseWriter, r *http.Request) {
			streamSignInBrowser(a, w, r)
		})

	reg.Add(http.MethodPost, "/api/signinbrowser/close",
		"close the sign-in browser, storing the session it holds, and say how its site stands",
		func(w http.ResponseWriter, r *http.Request) {
			st, ok := a.CloseSignInBrowser(r.Context())
			if !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(w, st)
		})
}

func streamSignInBrowser(a *app.App, w http.ResponseWriter, r *http.Request) {
	// No InsecureSkipVerify: the library then requires Origin to match Host,
	// as serveWS does.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	admitted := stillAdmitted(a, r)
	go func() {
		t := time.NewTicker(signInBrowserRecheck)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if !admitted() {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var in signinbrowser.Input
			if json.Unmarshal(data, &in) == nil {
				a.SignInBrowser().Input(in)
			}
		}
	}()

	if err := a.SignInBrowser().Watch(ctx, wsViewer{ctx: ctx, c: c}); err != nil {
		c.Close(websocket.StatusNormalClosure, "no sign-in window is open")
		return
	}
	c.Close(websocket.StatusNormalClosure, "")
}

// wsViewer shows the sign-in browser over a WebSocket. A write that cannot get
// through in time ends the stream rather than holding the browser up.
type wsViewer struct {
	ctx context.Context
	c   *websocket.Conn
}

func (v wsViewer) Frame(jpeg []byte) error { return v.write(websocket.MessageBinary, jpeg) }

func (v wsViewer) Send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return v.write(websocket.MessageText, b)
}

func (v wsViewer) write(kind websocket.MessageType, b []byte) error {
	ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
	defer cancel()
	return v.c.Write(ctx, kind, b)
}
