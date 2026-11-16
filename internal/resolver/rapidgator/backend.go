package rapidgator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader = ownlogin.Downloader

// Status is what Rapidgator made of the stored login the last time it was
// asked.
type Status = ownlogin.Status

const (
	// sessionAge is how long a token is used before signing in afresh, as
	// long as JDownloader's plugin keeps one. Rapidgator blocks accounts that
	// sign in too often, so a token is kept rather than fetched per download.
	sessionAge = 45 * time.Minute
	// loginWait is how long a download waits when Rapidgator asks for a pause
	// between sign-ins, which JDownloader's plugin waits as well.
	loginWait = 5 * time.Minute
	// maxBusy is how many rounds of loginWait a download sits out before it
	// fails and the app's retries take over.
	maxBusy = 3
	// spentBench keeps a login without traffic out of routing, so the next
	// links go another way, until it is tried again.
	spentBench = time.Hour
	// smallFile is the size up to which a file gets one connection.
	// JDownloader's plugin found small files breaking on more.
	smallFile = 2 << 20
)

// Backend downloads Rapidgator files on the stored premium login: it asks the
// API for the download link and hands it to the engine. A wait Rapidgator
// asks for keeps the task running with the time left in its note.
type Backend struct {
	*ownlogin.Login
	*ownlogin.Tasks

	client   *Client
	onUpdate func(taskID string, u core.Update)
	sleep    func(ctx context.Context, d time.Duration) error
	// inspectPause spaces the questions of one Inspect, so a long paste does
	// not look like a crawler to Rapidgator.
	inspectPause time.Duration
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{client: c, onUpdate: onUpdate, sleep: hoster.Sleep, inspectPause: 500 * time.Millisecond}
	b.Login = ownlogin.NewLogin(ownlogin.Signer{
		Login: c.Login, Account: c.User, Reason: reason, Expired: func(err error) bool { return errors.Is(err, errSession) },
		Age: sessionAge, None: ErrNoLogin,
	})
	b.Tasks = ownlogin.NewTasks(b.Login, eng, onUpdate, ownlogin.Config{
		Name: "Rapidgator", Unlock: b.unlock,
		WaitNote: "Waiting for Rapidgator: %s left",
		Busy:     ErrBusy, BusyWait: loginWait, MaxBusy: maxBusy,
		BusyNote:  "Rapidgator asks for a pause between sign-ins, next try in %s",
		SmallFile: smallFile,
	})
	b.Tasks.Sleep = func(ctx context.Context, d time.Duration) error { return b.sleep(ctx, d) }
	return b
}

// unlock asks the API for the file's download link. delay waits out the pause
// the link comes with before it is used.
func (b *Backend) unlock(ctx context.Context, taskID, link string, delay func(time.Duration) error) (string, int64, error) {
	norm, ok := Normalize(link)
	if !ok {
		return "", 0, fmt.Errorf("rapidgator: %s is not a link to a file", link)
	}
	id := fileIDOf(norm)
	for retried := false; ; retried = true {
		s, err := b.Session(ctx, retried)
		if err != nil {
			return "", 0, err
		}
		if !s.Account.Premium {
			return "", 0, ErrNotPremium
		}
		// A plan without a traffic quota reports no total.
		if s.Account.TrafficMax > 0 && s.Account.TrafficLeft <= 0 {
			b.Bench(spentBench)
			return "", 0, ErrTraffic
		}
		f, err := b.client.File(ctx, s.Token, id)
		if errors.Is(err, errSession) && !retried {
			continue
		}
		if err != nil {
			return "", 0, err
		}
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: f.Name, Size: f.Size})
		if s.Account.TrafficMax > 0 && f.Size > s.Account.TrafficLeft {
			return "", 0, fmt.Errorf("%w for this file, which is %s while %s are left", ErrTraffic,
				ownlogin.SizeText(f.Size), ownlogin.SizeText(s.Account.TrafficLeft))
		}
		direct, wait, err := b.client.Link(ctx, s.Token, id)
		if errors.Is(err, errSession) && !retried {
			continue
		}
		if errors.Is(err, ErrTraffic) {
			b.Bench(spentBench)
		}
		if err != nil {
			return "", 0, err
		}
		if wait > 0 {
			if err := delay(wait); err != nil {
				return "", 0, err
			}
		}
		return direct, f.Size, nil
	}
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
	case errors.Is(err, ErrTraffic), errors.Is(err, ErrDenied):
		return core.ReasonLimit
	case errors.Is(err, ErrBusy), errors.Is(err, errSession), errors.As(err, &api) && api.Status >= http.StatusInternalServerError:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}
