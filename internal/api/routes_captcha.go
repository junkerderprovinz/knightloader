package api

// The prompt side of a captcha: listing what is currently pending and
// submitting an answer to one of them. Giving up on one (skip/blacklist) is
// routes_captcha_skip.go's route, not this file's - see that file's own
// package comment for why JD's own blacklist makes a second store here
// unnecessary. Rendering a live third-party widget is
// routes_captcha_widget.go's route - this file only ever hands the browser
// the sitekey data a challenge already carries; it does not run a vendor's
// script itself.
//
// GET /api/captcha answers internal/captcha.Challenge values verbatim
// (encoding/json on the exported struct, no reshaping): 7F's own doc comment
// on that type already scopes it to what a browser needs and nothing a JD
// internal id would leak beyond the challenge's own opaque id - see
// challenge.go's doc comment on Challenge.ID ("opaque: callers pass it back
// unchanged and must not parse it").

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
)

func registerCaptcha(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/captcha", "every captcha challenge currently pending on this instance",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.CaptchaChallenges())
		})

	// Not GET, even though nothing here is a write from the caller's own
	// point of view: it makes a live call to the JD sidecar rather than
	// only reading this process's own state (CaptchaChallenges above stays
	// GET for exactly that reason), and every other "ask again right now"
	// route in this app is POST for the same reason - /api/tasks/recheck,
	// this package's own precedent, one file up.
	reg.Add(http.MethodPost, "/api/captcha/refresh", "poll the captcha source right now instead of waiting for the next automatic check",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.RefreshCaptchas(r.Context()))
		})

	reg.Add(http.MethodPost, "/api/captcha/{id}/answer", "submit an answer to one captcha challenge",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if id == "" {
				http.Error(w, "which challenge is this an answer to?", http.StatusBadRequest)
				return
			}
			var body struct {
				Text string `json:"text"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			stillValid, err := a.AnswerCaptcha(r.Context(), id, body.Text)
			if err != nil {
				if errors.Is(err, captcha.ErrJDNotConfigured) {
					// Matches routes_captcha_skip.go's own precedent for the
					// identical failure: no JD sidecar configured is "this
					// backend is unavailable", not a malformed request.
					http.Error(w, err.Error(), http.StatusServiceUnavailable)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// stillValid is returned in the body rather than only reaching the
			// caller through the "captchaResolved" hub broadcast this also
			// triggers (app_captcha.go's settleCaptcha): the submitting browser
			// needs the direct, synchronous answer to "did this arrive too
			// late" to react immediately (captcha.Source.Answer's own doc
			// comment), and a WebSocket round trip is not owed to the one
			// caller who already has it firsthand.
			writeJSON(w, struct {
				StillValid bool `json:"stillValid"`
			}{stillValid})
		})
}
