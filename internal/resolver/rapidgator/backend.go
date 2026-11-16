package rapidgator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
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
	// siteCheck is how long a website session is taken as it stands before
	// its profile page is read again, as long as JDownloader's plugin trusts
	// one.
	siteCheck = 2 * time.Hour
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

	// Solve answers the captcha of the website's sign-in page. Nil answers
	// none.
	Solve hoster.Solve

	// viaSite is a login that signs in on the website, and siteToken the
	// session whose account siteAcct read at siteAt.
	mu        sync.Mutex
	viaSite   bool
	siteToken string
	siteAcct  Account
	siteAt    time.Time
	now       func() time.Time
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{client: c, onUpdate: onUpdate, sleep: hoster.Sleep, inspectPause: 500 * time.Millisecond, now: time.Now}
	b.Login = ownlogin.NewLogin(ownlogin.Signer{
		Login: b.signIn, Account: b.account, Reason: reason, Expired: func(err error) bool { return errors.Is(err, errSession) },
		Age: sessionAge, Keep: isSiteToken, None: ErrNoLogin,
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

// signIn signs in through the API, or on the website where the API will not
// sign the login in. The API passes over a two-factor code, so a code only
// ever goes to the website.
func (b *Backend) signIn(ctx context.Context, user, password, code string) (string, Account, error) {
	b.mu.Lock()
	site := b.viaSite
	b.mu.Unlock()
	var refused *LoginError
	if !site && code == "" {
		token, acct, err := b.client.Login(ctx, user, password, "")
		if !errors.As(err, &refused) || !refused.Activate && !refused.NewAddress && !errors.Is(err, ownlogin.ErrNeedsCode) {
			return token, acct, err
		}
	}
	b.mu.Lock()
	b.viaSite = true
	b.mu.Unlock()
	token, acct, err := b.client.siteLogin(ctx, user, password, code, b.Solve)
	switch {
	case errors.Is(err, errSiteRefused) && refused != nil:
		return "", Account{}, refused
	case errors.Is(err, errSiteRefused):
		return "", Account{}, notActivated()
	case err == nil:
		b.remember(token, acct)
	}
	return token, acct, err
}

// remember keeps what a website session's profile said, until siteCheck has
// passed.
func (b *Backend) remember(token string, acct Account) {
	b.mu.Lock()
	b.siteToken, b.siteAcct, b.siteAt = token, acct, b.now()
	b.mu.Unlock()
}

// forget has the next look at a website session read its profile again.
func (b *Backend) forget() {
	b.mu.Lock()
	b.siteToken = ""
	b.mu.Unlock()
}

// SetLogin stores the login downloads go out on, as ownlogin.Login does. A
// different login tries the API first again.
func (b *Backend) SetLogin(user, password string) bool {
	changed := b.Login.SetLogin(user, password)
	if changed {
		b.mu.Lock()
		b.viaSite, b.siteToken = false, ""
		b.mu.Unlock()
	}
	return changed
}

// account reads the account behind token, from the website's profile page
// for one of its sessions.
func (b *Backend) account(ctx context.Context, token string) (Account, error) {
	if !isSiteToken(token) {
		return b.client.User(ctx, token)
	}
	b.mu.Lock()
	b.viaSite = true
	if token == b.siteToken && b.now().Sub(b.siteAt) < siteCheck {
		acct := b.siteAcct
		b.mu.Unlock()
		return acct, nil
	}
	b.mu.Unlock()
	acct, err := b.client.siteAccount(ctx, token)
	if err != nil {
		b.forget()
		return acct, err
	}
	b.remember(token, acct)
	return acct, nil
}

// file reads the name and size of the file id, from its public page for a
// website session.
func (b *Backend) file(ctx context.Context, token, id string) (File, error) {
	if !isSiteToken(token) {
		return b.client.File(ctx, token, id)
	}
	p, err := b.client.Page(ctx, DefaultSite+"/file/"+id)
	return File{Name: p.name, Size: p.size}, err
}

// link asks for the download link of the file id, from its page for a
// website session, which comes with no wait.
func (b *Backend) link(ctx context.Context, token, id string) (string, time.Duration, error) {
	if !isSiteToken(token) {
		return b.client.Link(ctx, token, id)
	}
	direct, err := b.client.siteLink(ctx, token, id)
	if errors.Is(err, errSession) {
		b.forget()
	}
	return direct, 0, err
}

// unlock asks for the file's download link. delay waits out the pause
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
		f, err := b.file(ctx, s.Token, id)
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
		direct, wait, err := b.link(ctx, s.Token, id)
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
