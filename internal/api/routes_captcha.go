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
	reg.Add(http.MethodGet, "/api/captcha", "every captcha challenge currently pending on this instance; reading it counts as watching them for a few seconds, for the kinds listed in watch (image, click, widget, and turnstile from a reader that runs Cloudflare Turnstile as well) or for all of them, and not at all with watch=0",
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
			res, err := a.AnswerCaptcha(r.Context(), id, body.Text)
			if err != nil {
				if errors.Is(err, captcha.ErrJDNotConfigured) {
					writeRefusal(w, http.StatusServiceUnavailable, "noJD", err.Error(), nil)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Answered directly as well as broadcast, so the submitting
			// browser learns at once whether the answer came too late, and
			// for a test captcha whether it was right.
			writeJSON(w, res)
		})

	reg.Add(http.MethodPost, "/api/captcha/test", "put up a test captcha, a picture of five characters this instance drew, which arrives and is answered like a real one and says whether the answer was right; it goes to the paid captcha accounts only with solvers set, since they bill it",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Solvers bool `json:"solvers"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			c, err := a.CreateTestCaptcha(body.Solvers)
			if err != nil {
				code := ""
				switch {
				case errors.Is(err, app.ErrCaptchaOff):
					code = "captchaOff"
				case errors.Is(err, app.ErrCaptchaJDOff):
					code = "captchaJDOff"
				case errors.Is(err, app.ErrNoCaptchaAccount):
					code = "noCaptchaAccount"
				}
				if code != "" {
					writeRefusal(w, http.StatusConflict, code, err.Error(), nil)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, c)
		})

	// The phone app reports on a path of its own, not with a parameter: an
	// older instance would ignore the parameter and count it as a window.
	report := func(by app.CaptchaViewer) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			a.ReportCaptchaUnanswerable(r.PathValue("id"), by)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	withdraw := func(by app.CaptchaViewer) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			a.WithdrawCaptchaUnanswerable(r.PathValue("id"), by)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	reg.Add(http.MethodPost, "/api/captcha/{id}/unanswerable", "say that a web window could not load this challenge, so the windows watching stop holding the paid solvers back for it",
		report(app.CaptchaWindow))
	reg.Add(http.MethodDelete, "/api/captcha/{id}/unanswerable", "withdraw a web window's report that it could not load this challenge, after a refresh has loaded it, so the windows hold the paid solvers back again; a solver already at work carries on",
		withdraw(app.CaptchaWindow))
	reg.Add(http.MethodPost, "/api/captcha/{id}/unanswerable/phone", "say that the phone app could not load this challenge, so its reads of the captcha list stop holding the paid solvers back for it",
		report(app.CaptchaPhone))
	reg.Add(http.MethodDelete, "/api/captcha/{id}/unanswerable/phone", "withdraw the phone app's report that it could not load this challenge, after a refresh has loaded it, so its reads hold the paid solvers back again; a solver already at work carries on",
		withdraw(app.CaptchaPhone))
}
