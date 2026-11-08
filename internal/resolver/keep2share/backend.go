package keep2share

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

// Status is what the site made of the stored login the last time it was
// asked.
type Status = ownlogin.Status

const (
	// loginWait is how long a download waits when the site counted too many
	// sign-ins, as long as JDownloader's plugin waits.
	loginWait = 31 * time.Minute
	// spentBench keeps a login without traffic out of routing, so the next
	// links go another way, until it is tried again.
	spentBench = time.Hour
)

// Backend downloads the files of one site on the stored premium login: it
// asks the API for the download link and hands it to the engine. The token
// is kept for as long as the site takes it, as JDownloader's plugin keeps it,
// since the sites ask for a captcha at sign-ins that come too often.
type Backend struct {
	*ownlogin.Login
	*ownlogin.Tasks

	host     Host
	client   *Client
	onUpdate func(taskID string, u core.Update)
	// inspectPause spaces the questions of one Inspect, as JDownloader's
	// plugin spaces its calls to the API.
	inspectPause time.Duration
}

func NewBackend(h Host, c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{host: h, client: c, onUpdate: onUpdate, inspectPause: 2 * time.Second}
	b.Login = ownlogin.NewLogin(ownlogin.Signer{
		Login: func(ctx context.Context, user, password string) (string, Account, error) {
			token, acct, err := c.Login(ctx, user, password)
			return token, acct, b.named(err)
		},
		Account: func(ctx context.Context, token string) (Account, error) {
			acct, err := c.Account(ctx, token)
			return acct, b.named(err)
		},
		Expired: func(err error) bool { return errors.Is(err, errSession) },
		None:    b.named(ErrNoLogin),
	})
	b.Tasks = ownlogin.NewTasks(eng, onUpdate, ownlogin.Config{
		Unlock: b.unlock,
		Reason: reason,
		Busy:   ErrBusy, BusyWait: loginWait, MaxBusy: 1,
		BusyNote: h.Name + " counted too many sign-ins and asks for a pause, next try in %s",
	})
	return b
}

// named puts the site in front of err, so the three sites' errors tell which
// one answered. The sign-in names its own.
func (b *Backend) named(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", b.host.Name, err)
}

// unlock asks the API for the file's download link. The traffic left is not
// held against the file's size: the site counts a file once a day, so one
// that went out before goes out again on less, and getUrl knows.
func (b *Backend) unlock(ctx context.Context, taskID, link string, _ func(time.Duration) error) (string, int64, error) {
	norm, ok := b.host.Normalize(link)
	if !ok {
		return "", 0, fmt.Errorf("%s: %s is not a link to a file", b.host.Name, link)
	}
	id := b.host.fileIDOf(norm)
	files, err := b.client.Files(ctx, []string{id})
	if err != nil {
		return "", 0, b.named(err)
	}
	f, ok := files[id]
	if !ok || !f.Available || f.Folder {
		return "", 0, b.named(ErrGone)
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: f.Name, Size: f.Size})
	for retried := false; ; retried = true {
		s, err := b.Session(ctx, retried)
		if err != nil {
			return "", 0, err
		}
		if !s.Account.Premium {
			return "", 0, b.named(ErrNotPremium)
		}
		direct, err := b.client.Link(ctx, s.Token, id)
		if errors.Is(err, errSession) && !retried {
			continue
		}
		if errors.Is(err, ErrTraffic) || errors.Is(err, ErrQuota) {
			b.Bench(spentBench)
		}
		return direct, f.Size, b.named(err)
	}
}

// reason is the cause a failed download is reported with.
func reason(err error) core.Reason {
	var refused *LoginError
	var api *APIError
	switch {
	case errors.Is(err, ErrGone), errors.Is(err, ErrBlocked):
		return core.ReasonGone
	case errors.As(err, &refused), errors.Is(err, ErrNotPremium), errors.Is(err, ErrNoLogin), errors.Is(err, ErrRestricted):
		return core.ReasonAuth
	case errors.Is(err, ErrTraffic), errors.Is(err, ErrQuota), errors.Is(err, ErrDenied):
		return core.ReasonLimit
	case errors.Is(err, ErrBusy), errors.Is(err, errSession), errors.As(err, &api) && api.Status >= http.StatusInternalServerError:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}
