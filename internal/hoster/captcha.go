package hoster

import (
	"context"
	"errors"
	"fmt"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// CaptchaKind is the kind of check a hoster asks for. A widget's kind is the
// vendor name the captcha package uses.
type CaptchaKind string

const (
	Turnstile CaptchaKind = "turnstile"
	Recaptcha CaptchaKind = "recaptcha"
	HCaptcha  CaptchaKind = "hcaptcha"
	Image     CaptchaKind = "image"
)

// Captcha is a check a hoster puts in front of a download link.
type Captcha struct {
	Kind CaptchaKind
	// SiteKey is a widget's key.
	SiteKey string
	// PageURL is the page the widget sits on, which it is solved for.
	PageURL string
	// Image is the picture of an image captcha.
	Image []byte
}

// ErrNoAnswer is a captcha nobody answered.
var ErrNoAnswer = errors.New("nobody answered the captcha")

// Solve answers a captcha for the download taskID.
type Solve func(ctx context.Context, taskID string, c Captcha) (string, error)

// Ask has solve answer c for the task, with the task's note saying that the
// download waits for the captcha name asks for. A nil solve and a failed one
// both come back as ErrNoAnswer, unless ctx ended meanwhile.
func (r *Runs) Ask(ctx context.Context, taskID, name string, solve Solve, c Captcha) (string, error) {
	if solve == nil {
		return "", ErrNoAnswer
	}
	r.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: "Waiting for the captcha " + name + " asks for"})
	answer, err := solve(ctx, taskID, c)
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		return "", fmt.Errorf("%w: %v", ErrNoAnswer, err)
	}
	return answer, nil
}
