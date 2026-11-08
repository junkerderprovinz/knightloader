package ownlogin

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// maxStrikes is how many outages in a row, each in words the hoster's client
// knows but none of them explained by the hoster, a login takes before it
// goes to JDownloader.
const maxStrikes = 3

// Handback gives a login KnightLoader's own client could not use to
// JDownloader, which signs in through its own plugin, until the client works
// with it again. The zero value keeps the login with KnightLoader.
type Handback struct {
	// Moved is called when the login goes to JDownloader or comes back. Nil
	// calls nothing.
	Moved func()

	mu      sync.Mutex
	handed  bool
	strikes int
}

// Handed reports whether the login is with JDownloader.
func (h *Handback) Handed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handed
}

// Fail weighs a failure on the login, reason being what the hoster's client
// made of err, and reports whether it handed the login to JDownloader. An
// answer the client could not place may be the client misreading the hoster,
// and so may outages that keep coming. A hoster out of reach and an answer
// the client understood, a wrong password included, leave the login where it
// is: JDownloader would meet the same.
func (h *Handback) Fail(err error, reason core.Reason) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || unreachable(err) {
		return false
	}
	h.mu.Lock()
	switch reason {
	case core.ReasonUnknown:
		h.strikes = maxStrikes
	case core.ReasonUnavailable:
		h.strikes++
	default:
		h.strikes = 0
	}
	moved := !h.handed && h.strikes >= maxStrikes
	if moved {
		h.handed = true
	}
	h.mu.Unlock()
	if moved {
		h.moved()
	}
	return moved
}

// Worked records a download the client unlocked, which ends a run of
// outages.
func (h *Handback) Worked() {
	h.mu.Lock()
	h.strikes = 0
	h.mu.Unlock()
}

// Reclaim takes the login back from JDownloader, after a check that worked
// or for a login that was changed.
func (h *Handback) Reclaim() {
	h.mu.Lock()
	moved := h.handed
	h.handed, h.strikes = false, 0
	h.mu.Unlock()
	if moved {
		h.moved()
	}
}

func (h *Handback) moved() {
	if h.Moved != nil {
		h.Moved()
	}
}

// Unexplained reports whether a failure for reason leaves open whether the
// client read the hoster right: one it could not place, or an outage. Once
// the login is with JDownloader, such a failure takes the download there too.
func Unexplained(reason core.Reason) bool {
	return reason == core.ReasonUnknown || reason == core.ReasonUnavailable
}

// unreachable reports whether err is the hoster not being reached at all.
func unreachable(err error) bool {
	var u *url.Error
	var n net.Error
	return errors.As(err, &u) || errors.As(err, &n)
}
