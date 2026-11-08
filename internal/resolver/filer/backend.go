package filer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader = ownlogin.Downloader

// Status is what filer.net made of the stored login the last time it was
// asked.
type Status = ownlogin.Status

// spentBench keeps a login without traffic out of routing, so the next links
// go another way, until it is tried again.
const spentBench = time.Hour

// Backend downloads filer.net files on the stored premium login: it asks the
// API for the download link and hands it to the engine. filer.net keeps no
// session, so every download asks for the account afresh, which also tells
// the traffic left.
type Backend struct {
	*ownlogin.Login
	*ownlogin.Tasks

	client   *Client
	onUpdate func(taskID string, u core.Update)
	// inspectPause spaces the questions of one Inspect, as JDownloader's
	// plugin spaces its calls to the API.
	inspectPause time.Duration
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{client: c, onUpdate: onUpdate, inspectPause: 500 * time.Millisecond}
	b.Login = ownlogin.NewLogin(ownlogin.Signer{Login: c.Login, Reason: reason, None: ErrNoLogin})
	b.Tasks = ownlogin.NewTasks(b.Login, eng, onUpdate, ownlogin.Config{Name: "filer.net", Unlock: b.unlock, MaxConns: conns})
	return b
}

// unlock asks the API for the file's download link, after the file itself.
func (b *Backend) unlock(ctx context.Context, taskID, link string, _ func(time.Duration) error) (string, int64, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", 0, fmt.Errorf("filer.net: %s is not a link to a file", link)
	}
	id := fileIDOf(norm)
	f, err := b.client.File(ctx, id)
	if err != nil {
		return "", 0, err
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: f.Name, Size: f.Size})
	s, err := b.Session(ctx, false)
	if err != nil {
		return "", 0, err
	}
	if !s.Account.Premium {
		return "", 0, ErrNotPremium
	}
	if s.Account.TrafficMax > 0 && s.Account.TrafficLeft <= 0 {
		b.Bench(spentBench)
		return "", 0, ErrTraffic
	}
	if s.Account.TrafficMax > 0 && f.Size > s.Account.TrafficLeft {
		return "", 0, fmt.Errorf("%w for this file, which is %s while %s are left", ErrTraffic,
			ownlogin.SizeText(f.Size), ownlogin.SizeText(s.Account.TrafficLeft))
	}
	direct, err := b.client.Link(ctx, s.User, s.Password, id)
	if errors.Is(err, ErrTraffic) {
		b.Bench(spentBench)
	}
	return direct, f.Size, err
}

// reason is the cause a failed download is reported with.
func reason(err error) core.Reason {
	var refused *LoginError
	var api *APIError
	switch {
	case errors.Is(err, ErrGone):
		return core.ReasonGone
	case errors.As(err, &refused), errors.Is(err, ErrNotPremium), errors.Is(err, ErrNoLogin), errors.Is(err, ErrPassword):
		return core.ReasonAuth
	case errors.Is(err, ErrTraffic), errors.Is(err, ErrDenied):
		return core.ReasonLimit
	case errors.Is(err, ErrBusy), errors.Is(err, ErrSignIn), errors.As(err, &api) && api.HTTP >= http.StatusInternalServerError:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}
