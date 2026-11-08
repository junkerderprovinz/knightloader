package nitroflare

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

// Status is what Nitroflare made of the stored login the last time it was
// asked.
type Status = ownlogin.Status

// spentBench keeps a login without traffic out of routing, so the next links
// go another way, until it is tried again.
const spentBench = time.Hour

// Backend downloads Nitroflare files on the stored premium login: it asks the
// API for the download link and hands it to the engine. Nitroflare keeps no
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
	b.Tasks = ownlogin.NewTasks(b.Login, eng, onUpdate, ownlogin.Config{Name: "Nitroflare", Unlock: b.unlock})
	return b
}

// unlock asks the API for the file's download link, after the file itself,
// as Nitroflare's API documentation asks according to JDownloader's plugin.
func (b *Backend) unlock(ctx context.Context, taskID, link string, _ func(time.Duration) error) (string, int64, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", 0, fmt.Errorf("nitroflare: %s is not a link to a file", link)
	}
	id := fileIDOf(norm)
	files, err := b.client.Files(ctx, []string{id})
	if err != nil {
		return "", 0, err
	}
	f, ok := files[id]
	if !ok || !f.Online {
		return "", 0, ErrGone
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
	case errors.As(err, &refused), errors.Is(err, ErrNotPremium), errors.Is(err, ErrNoLogin):
		return core.ReasonAuth
	case errors.Is(err, ErrTraffic):
		return core.ReasonLimit
	case errors.As(err, &api) && api.Status >= http.StatusInternalServerError:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}
