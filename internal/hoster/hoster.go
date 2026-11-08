// Package hoster runs the downloads of KnightLoader's own clients for file
// hosters that hand out a plain download link once asked: it asks the
// hoster's client for the link, sits out the waits the hoster puts in the
// way with the time left in the task's note, hands a captcha on to the app's
// captcha prompts, and passes the link on to the engine. Each hoster's package only has to know how to ask. Runs, which
// keeps a task's runs and waits, also carries the backends that ask their
// hoster in more steps: 1fichier, the XFileSharing sites and the hosters on a
// premium login (internal/ownlogin).
package hoster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Link is the download link a hoster handed out for one file.
type Link struct {
	URL string
	// Headers go with every request for URL, such as a token the hoster
	// checks on its download servers.
	Headers map[string]string
	// Conns caps the connections the hoster allows for this download, 0 for
	// no cap of its own.
	Conns int
	// Name and Size are what the hoster said about the file, shown on the
	// task while the link is handed on. Either may be empty.
	Name string
	Size int64
}

// Unlocker asks one hoster for the download link behind a file link.
type Unlocker interface {
	// Unlock returns the download link for link. password is the task's
	// download password, "" for none. A *WaitError asks for a pause before
	// the next try, and a *Refusal is a reason the download cannot go ahead.
	Unlock(ctx context.Context, link, password string) (Link, error)
}

// Captcha is a widget a hoster's page wants solved before it hands out the
// link.
type Captcha struct {
	// Vendor is the widget's maker as the captcha package names it, such as
	// "turnstile".
	Vendor  string
	SiteKey string
	// PageURL is the page the widget sits on, which it is solved for.
	PageURL string
}

// AskingUnlocker is an Unlocker whose hoster may put a captcha in the way.
// ask hands the captcha to whoever answers captchas and returns the answer.
type AskingUnlocker interface {
	UnlockAsking(ctx context.Context, link, password string, ask func(context.Context, Captcha) (string, error)) (Link, error)
}

// ErrNoAnswer is a captcha nobody answered.
var ErrNoAnswer = errors.New("nobody answered the captcha")

// WaitError is a hoster asking for a pause before the next try.
type WaitError struct {
	Wait time.Duration
	// Why is what the task's note says while it waits, such as "Mediafire
	// limits free downloads". Empty reads as a plain wait for the hoster.
	Why string
	// Reason is what the task fails with once the backend has waited as
	// often as it will.
	Reason core.Reason
}

func (e *WaitError) Error() string {
	why := e.Why
	if why == "" {
		why = "the hoster asked for a pause"
	}
	return fmt.Sprintf("%s, for %s", why, e.Wait)
}

// Refusal is a hoster turning a download down, with the reason the task
// fails with.
type Refusal struct {
	Reason core.Reason
	Msg    string
}

func (r *Refusal) Error() string { return r.Msg }

// Gone is the refusal for a file that is gone from the hoster. host prefixes
// the message, as every error of these clients names the hoster.
func Gone(host string) error {
	return &Refusal{Reason: core.ReasonGone, Msg: host + ": the file has been deleted or never existed"}
}

// IsGone reports whether err is a hoster saying the file is not there.
func IsGone(err error) bool {
	var r *Refusal
	return errors.As(err, &r) && r.Reason == core.ReasonGone
}

// reasonOf is the reason a failed unlock fails its task with.
func reasonOf(err error) core.Reason {
	var r *Refusal
	var w *WaitError
	switch {
	case errors.As(err, &r):
		return r.Reason
	case errors.As(err, &w):
		return w.Reason
	case errors.Is(err, ErrNoAnswer):
		return core.ReasonCaptcha
	}
	return core.ReasonUnknown
}

// Span writes a wait as people read it, rounded up: "1 h 5 min", "37 min",
// "45 s".
func Span(d time.Duration) string {
	switch {
	case d >= time.Hour:
		m := int((d + time.Minute - 1) / time.Minute)
		if m%60 == 0 {
			return fmt.Sprintf("%d h", m/60)
		}
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d min", (d+time.Minute-1)/time.Minute)
	}
	return fmt.Sprintf("%d s", (d+time.Second-1)/time.Second)
}
