package api

// The prompt side of a captcha: listing what is pending and answering it.
// Skipping lives in routes_captcha_skip.go and the widget page in
// routes_captcha_widget.go. GET /api/captcha answers captcha.Challenge values
// as they are; the type holds only what a browser needs.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
)

func registerCaptcha(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/captcha", "every captcha challenge currently pending on this instance; reading it counts as watching them for a few seconds, for the kinds listed in watch (image, click, widget) or for all of them, and not at all with watch=0",
		func(w http.ResponseWriter, r *http.Request) {
			// An app that polls this list, over the relay as well, holds no
			// socket to report itself on, so its reads say it is watching,
			// for the kinds it can answer when it lists them. The web
			// interface reports over its socket and reads with watch=0.
			switch watch := r.URL.Query().Get("watch"); watch {
			case "0":
			case "":
				a.CaptchaSeen(nil)
			default:
				a.CaptchaSeen(strings.Split(watch, ","))
			}
			writeJSON(w, a.CaptchaChallenges())
		})

	// A POST because it makes a live call to the JD sidecar, like every other
	// "ask again now" route.
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
					writeRefusal(w, http.StatusServiceUnavailable, "noJD", err.Error(), nil)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Answered directly as well as broadcast, so the submitting
			// browser learns at once whether the answer came too late.
			writeJSON(w, struct {
				StillValid bool `json:"stillValid"`
			}{stillValid})
		})

	reg.Add(http.MethodPost, "/api/captcha/{id}/unanswerable", "say that a web window could not load this challenge, so the windows watching stop holding the paid solvers back for it",
		func(w http.ResponseWriter, r *http.Request) {
			a.CaptchaUnanswerable(r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		})
}
