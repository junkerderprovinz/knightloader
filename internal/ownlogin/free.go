package ownlogin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// CaptchaKind is the kind of check a free download asks for. A widget's kind
// is the vendor name the captcha package uses.
type CaptchaKind string

const (
	Turnstile CaptchaKind = "turnstile"
	Recaptcha CaptchaKind = "recaptcha"
	HCaptcha  CaptchaKind = "hcaptcha"
	Image     CaptchaKind = "image"
)

// Captcha is the check a hoster asks for before a free download.
type Captcha struct {
	Kind CaptchaKind
	// SiteKey is a widget's key.
	SiteKey string
	// PageURL is the page the widget is on, which it is solved for.
	PageURL string
	// Image is the picture of an image captcha.
	Image []byte
}

// ErrNoAnswer is a captcha nobody answered.
var ErrNoAnswer = errors.New("nobody answered the captcha")

// PauseError is the hoster making the address wait between free downloads.
type PauseError struct {
	Wait time.Duration
	// Msg is the hoster's words, "" for none.
	Msg string
}

func (e *PauseError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("the hoster makes this address wait %s before the next free download", hoster.Span(e.Wait))
}

const (
	// maxCaptchas is how often a free download asks for a captcha after the
	// hoster turned an answer down, before it fails.
	maxCaptchas = 3
	// maxPause is the longest pause between free downloads a task sits out.
	// A longer one fails the task, which the app retries once it is over.
	maxPause = 3 * time.Hour
	// maxPauses is how many pauses in a row a task sits out.
	maxPauses = 3
)

// FreeUnlock asks the hoster for the download link of a free download. ask
// has a captcha answered and delay sits out a countdown, both shown on the
// task.
type FreeUnlock func(ctx context.Context, taskID, link string, ask func(Captcha) (string, error), delay func(time.Duration) error) (string, error)

// FreeConfig is what a hoster brings to its free downloads.
type FreeConfig struct {
	// Name is the hoster as the task's notes name it.
	Name   string
	Unlock FreeUnlock
	// Reason is the cause a failure the runner does not know is reported
	// with.
	Reason func(error) core.Reason
	// Refused is the hoster turning a captcha's answer down. The download
	// asks again, up to maxCaptchas times.
	Refused error
}

// Free runs a hoster's free downloads: the captcha, the countdown and the
// pause between free downloads, each shown on the task, and then the link
// over one connection, which is all a free download gets.
type Free struct {
	*hoster.Runs

	// Sleep waits d, or less when ctx ends first.
	Sleep func(ctx context.Context, d time.Duration) error
	// Solve answers a captcha for the task. Nil answers none.
	Solve func(ctx context.Context, taskID string, c Captcha) (string, error)

	eng      Downloader
	onUpdate func(taskID string, u core.Update)
	cfg      FreeConfig
}

func NewFree(eng Downloader, onUpdate func(taskID string, u core.Update), cfg FreeConfig) *Free {
	f := &Free{Sleep: hoster.Sleep, eng: eng, onUpdate: onUpdate, cfg: cfg}
	f.Runs = hoster.NewRuns(eng, onUpdate, f.run, func(ctx context.Context, d time.Duration) error { return f.Sleep(ctx, d) })
	return f
}

func (f *Free) run(ctx context.Context, taskID, link string, _ int) {
	captchas, pauses := 1, 0
	for {
		direct, err := f.unlock(ctx, taskID, link, func(d time.Duration) error {
			return f.wait(ctx, taskID, d, "Waiting for "+f.cfg.Name+": %s left")
		})
		var pause *PauseError
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			f.handOn(ctx, taskID, link, direct)
			return
		case errors.As(err, &pause) && pause.Wait <= maxPause && pauses < maxPauses:
			pauses++
			if f.wait(ctx, taskID, pause.Wait, f.cfg.Name+" makes this address wait between free downloads, %s left") != nil {
				return
			}
		case f.cfg.Refused != nil && errors.Is(err, f.cfg.Refused) && captchas < maxCaptchas:
			captchas++
		default:
			f.fail(taskID, err)
			return
		}
	}
}

func (f *Free) unlock(ctx context.Context, taskID, link string, delay func(time.Duration) error) (string, error) {
	return f.cfg.Unlock(ctx, taskID, link, func(c Captcha) (string, error) { return f.ask(ctx, taskID, c) }, delay)
}

// ask puts the captcha in front of whoever answers captchas and waits for
// the answer.
func (f *Free) ask(ctx context.Context, taskID string, c Captcha) (string, error) {
	if f.Solve == nil {
		return "", ErrNoAnswer
	}
	f.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: "Waiting for the captcha " + f.cfg.Name + " asks for"})
	answer, err := f.Solve(ctx, taskID, c)
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		return "", fmt.Errorf("%w: %v", ErrNoAnswer, err)
	}
	return answer, nil
}

// fail reports err with its cause. A pause too long to sit out asks the app
// to try again once it is over.
func (f *Free) fail(taskID string, err error) {
	u := core.Update{Status: core.StatusError, Err: err.Error()}
	var pause *PauseError
	switch {
	case errors.As(err, &pause):
		u.Reason, u.Retry = core.ReasonLimit, pause.Wait
	case errors.Is(err, ErrNoAnswer), f.cfg.Refused != nil && errors.Is(err, f.cfg.Refused):
		u.Reason = core.ReasonCaptcha
	default:
		u.Reason = f.cfg.Reason(err)
	}
	f.onUpdate(taskID, u)
}

// handOn gives the link to the engine on one connection. A link that stops
// working part way is fetched afresh, captcha and all.
func (f *Free) handOn(ctx context.Context, taskID, link, direct string) {
	if !f.Handing(ctx, taskID) {
		return
	}
	f.eng.Handover(taskID, direct, 1, func(ctx context.Context) (string, error) {
		return f.unlock(ctx, taskID, link, func(d time.Duration) error { return f.Sleep(ctx, d) })
	})
}

func (f *Free) wait(ctx context.Context, taskID string, d time.Duration, note string) error {
	return f.Wait(ctx, taskID, d, func(left time.Duration) string { return fmt.Sprintf(note, hoster.Span(left)) })
}
