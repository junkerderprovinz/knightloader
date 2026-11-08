package xfs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/ownlogin"
)

// Downloader is the byte-transfer backend the download link is handed to.
type Downloader interface {
	// Handover starts the transfer of url. relink reads the file's page again
	// for a fresh url, for what is left when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// maxCaptchaTries is how often a free download asks for a captcha again after
// the site turned an answer down, before it fails.
const maxCaptchaTries = 3

// maxSteps is how many forms a download sends before it gives up on a site
// that keeps answering with another one.
const maxSteps = 4

// answerLife is how long a captcha's answer is sure to be taken. A countdown
// longer than that is sat out mostly before the captcha is asked, as
// JDownloader does, so the answer is still fresh when the form goes in.
const answerLife = 100 * time.Second

// LoginState is what the backend found out about the stored login.
type LoginState struct {
	// Checked is set once the account page was read.
	Checked bool
	// Refused is a login the site turned down.
	Refused bool
	// Err is why the last look found nothing out, nil when it did. What an
	// earlier look found is kept beside it.
	Err error
	Account
}

// Backend signs in with the user's login, reads a file's page, waits out
// whatever the site asks for, sends the download forms and hands the link to
// the engine. A wait keeps the task running with the time left in its note.
type Backend struct {
	*hoster.Runs
	// Handback gives the login to JDownloader when the client cannot use it.
	ownlogin.Handback

	client   *Client
	site     *Site
	eng      Downloader
	onUpdate func(taskID string, u core.Update)

	// Password returns the download password a task carries, "" for none.
	Password func(taskID string) string
	// Login returns the stored login, ok false for none or one that is
	// switched off.
	Login func() (user, password string, ok bool)
	// Solve answers a captcha. taskID is the download waiting on it, "" for a
	// sign-in no download waits on.
	Solve func(ctx context.Context, taskID string, c Captcha) (string, error)
	// LoadSession and SaveSession keep a signed-in session across restarts,
	// so a restart needs no sign-in and no captcha. "" is none.
	LoadSession func() string
	SaveSession func(cookies string)
	// OnLogin hears every change to what is known about the login.
	OnLogin func(LoginState)
	// Sleep sits out a wait. Tests shorten it.
	Sleep func(ctx context.Context, d time.Duration) error

	// signMu lets one sign-in run at a time, so downloads that start together
	// share its captcha.
	signMu  sync.Mutex
	session *Session
	resumed bool

	stateMu sync.Mutex
	state   LoginState
	known   bool
	signing bool
}

func NewBackend(c *Client, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	b := &Backend{client: c, site: c.site, eng: eng, onUpdate: onUpdate, Sleep: hoster.Sleep}
	b.Runs = hoster.NewRuns(eng, onUpdate, b.run, func(ctx context.Context, d time.Duration) error { return b.Sleep(ctx, d) })
	return b
}

// Client is the page client the backend and its resolver share.
func (b *Backend) Client() *Client { return b.client }

// Premium reports whether the stored login was last found with premium and
// traffic left, and is not with JDownloader. It asks nothing, so the app may
// call it holding its lock.
func (b *Backend) Premium() bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	st := b.state
	return b.known && !st.Refused && st.Premium && (st.TrafficMax == 0 || st.TrafficLeft > 0) && !b.Handed()
}

// LoginStatus is what is known about the stored login, and whether a sign-in
// is under way. It asks nothing, so the app may call it holding its lock.
func (b *Backend) LoginStatus() (st LoginState, signingIn bool) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.state, b.signing
}

// Forget drops the session and what is known about the login, for a login
// that was changed, switched off or removed.
func (b *Backend) Forget() {
	b.signMu.Lock()
	b.session, b.resumed = nil, true
	b.signMu.Unlock()
	b.save("")
	b.stateMu.Lock()
	b.state, b.known = LoginState{}, false
	b.stateMu.Unlock()
	b.Reclaim()
}

// CheckLogin reads the account page for the plan and the traffic left. It
// signs in for that only when signIn is set, since a sign-in can ask for a
// captcha.
func (b *Backend) CheckLogin(ctx context.Context, signIn bool) {
	if _, _, ok := b.credentials(); !ok {
		b.Forget()
		return
	}
	for range 2 {
		s, err := b.signedIn(ctx, "", signIn)
		if s == nil && err == nil {
			return
		}
		if err != nil {
			b.learn(err, nil)
			b.Fail(err, misread(err))
			return
		}
		acct, err := b.client.Account(ctx, s)
		if errors.Is(err, errSignedOut) {
			b.dropSession(s)
			continue
		}
		b.learn(err, &acct)
		if err != nil {
			b.Fail(err, misread(err))
		}
		return
	}
}

// signedIn returns the session to download on: the one in use, the saved one,
// or a new sign-in when signIn is set. It is nil without an error when there
// is none and no sign-in was wanted.
func (b *Backend) signedIn(ctx context.Context, taskID string, signIn bool) (*Session, error) {
	b.signMu.Lock()
	defer b.signMu.Unlock()
	if b.session != nil {
		return b.session, nil
	}
	if !b.resumed {
		b.resumed = true
		if saved := b.loadSession(); saved != "" {
			b.session = b.client.Resume(saved)
			return b.session, nil
		}
	}
	if !signIn {
		return nil, nil
	}
	user, password, ok := b.credentials()
	if !ok {
		return nil, ErrNoLogin
	}
	b.setSigning(true)
	s, err := b.client.SignIn(ctx, user, password, func(ctx context.Context, c Captcha) (string, error) {
		return b.solve(ctx, taskID, c)
	})
	b.setSigning(false)
	if err != nil {
		return nil, err
	}
	b.session = s
	b.save(s.Cookies())
	return s, nil
}

func (b *Backend) setSigning(on bool) {
	b.stateMu.Lock()
	b.signing = on
	b.stateMu.Unlock()
}

// dropSession forgets s once the site has ended it.
func (b *Backend) dropSession(s *Session) {
	b.signMu.Lock()
	defer b.signMu.Unlock()
	if b.session == s {
		b.session = nil
		b.save("")
	}
}

// learn folds what a look at the login found into what is known: the account
// page read, or the error that kept it from being read.
func (b *Backend) learn(err error, acct *Account) {
	if errors.Is(err, context.Canceled) {
		return
	}
	b.stateMu.Lock()
	switch {
	case acct != nil && err == nil:
		b.state = LoginState{Checked: true, Account: *acct}
	case errors.Is(err, ErrLoginRefused):
		b.state = LoginState{Refused: true}
	default:
		b.state.Err = err
	}
	b.known = true
	st := b.state
	b.stateMu.Unlock()
	if b.OnLogin != nil {
		b.OnLogin(st)
	}
}

// learnAccount changes what is known about the plan after a download ran into
// its end.
func (b *Backend) learnAccount(change func(*Account)) {
	b.stateMu.Lock()
	change(&b.state.Account)
	acct := b.state.Account
	b.stateMu.Unlock()
	b.learn(nil, &acct)
}

// run is one go at a download, with the connections the site allows rather
// than those the task asks for.
func (b *Backend) run(ctx context.Context, taskID, link string, _ int) {
	// A download routed here before the login went to JDownloader follows
	// the login.
	if b.Handed() {
		b.handBack(taskID, errors.New("the login is with JDownloader"))
		return
	}
	premium := b.Premium()
	captchas := 1
	for {
		direct, conns, err := b.unlock(ctx, taskID, link, premium, func(d time.Duration) error { return b.wait(ctx, taskID, d) })
		var w *WaitError
		switch {
		case ctx.Err() != nil:
			// Pause and Remove set the task's state themselves.
			return
		case err == nil:
			if premium {
				b.Worked()
			}
			b.handOn(ctx, taskID, link, direct, conns)
			return
		case errors.As(err, &w):
			if b.wait(ctx, taskID, w.Wait) != nil {
				return
			}
		case errors.Is(err, ErrCaptchaRefused) && captchas < maxCaptchaTries:
			captchas++
		case premium && (b.Fail(err, misread(err)) || b.Handed() && ownlogin.Unexplained(misread(err))):
			b.handBack(taskID, err)
			return
		default:
			b.fail(taskID, err)
			return
		}
	}
}

// unlock fetches the download link and the connections it may use, on the
// login when premium is set and for free otherwise. countdown sits out the
// free page's countdown.
func (b *Backend) unlock(ctx context.Context, taskID, link string, premium bool, countdown func(time.Duration) error) (string, int, error) {
	if premium {
		direct, err := b.unlockPremium(ctx, taskID, link)
		return direct, b.site.PremiumConns, err
	}
	direct, err := b.unlockFree(ctx, taskID, link, countdown)
	return direct, b.site.FreeConns, err
}

func (b *Backend) unlockPremium(ctx context.Context, taskID, link string) (string, error) {
	for try := 0; ; try++ {
		s, err := b.signedIn(ctx, taskID, true)
		if err != nil {
			b.learn(err, nil)
			return "", err
		}
		p, err := b.open(ctx, taskID, s, link)
		direct := ""
		for step := 0; err == nil && direct == ""; step++ {
			switch {
			case p.Direct != "":
				direct = p.Direct
			case p.free() && !p.signedIn:
				// The free page of a session that ended.
				err = errSignedOut
			case p.free():
				// The free page while signed in: the account has no premium
				// any more.
				b.learnAccount(func(a *Account) { a.Premium = false })
				return "", ErrPremiumExpired
			case step == maxSteps:
				err = errTooManySteps
			default:
				var answer string
				if answer, err = b.pass(ctx, taskID, p, func(d time.Duration) error { return b.Sleep(ctx, d) }); err == nil {
					p, err = p.Send(ctx, Input{Password: b.password(taskID), Answer: answer})
				}
			}
		}
		switch {
		case errors.Is(err, errSignedOut) && try == 0:
			b.dropSession(s)
			continue
		case errors.Is(err, ErrTraffic):
			b.learnAccount(func(a *Account) {
				a.TrafficLeft = 0
				a.TrafficMax = max(a.TrafficMax, b.site.DailyTraffic)
			})
		case errors.Is(err, errSignedOut):
			err = ErrSignInFailed
		}
		return direct, err
	}
}

var errTooManySteps = errors.New("the site kept answering with another form and never with the download")

func (b *Backend) unlockFree(ctx context.Context, taskID, link string, countdown func(time.Duration) error) (string, error) {
	p, err := b.open(ctx, taskID, b.client.NewSession(), link)
	for step := 0; err == nil; step++ {
		if p.Direct != "" {
			return p.Direct, nil
		}
		if step == maxSteps {
			return "", errTooManySteps
		}
		var answer string
		if answer, err = b.pass(ctx, taskID, p, countdown); err == nil {
			p, err = p.Send(ctx, Input{Password: b.password(taskID), Answer: answer})
		}
	}
	return "", err
}

// pass answers the page's captcha and sits out its countdown. The time the
// captcha takes counts towards the countdown, as in a browser.
func (b *Backend) pass(ctx context.Context, taskID string, p *Page, countdown func(time.Duration) error) (string, error) {
	if p.Captcha == nil {
		return "", sitOut(p.Countdown, countdown)
	}
	if err := sitOut(p.Countdown-answerLife, countdown); err != nil {
		return "", err
	}
	start := time.Now()
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: "Waiting for the captcha " + b.site.Name + " asks for"})
	answer, err := b.solve(ctx, taskID, *p.Captcha)
	if err != nil {
		return "", err
	}
	return answer, sitOut(min(p.Countdown, answerLife)-time.Since(start), countdown)
}

// sitOut waits d, if there is anything to wait.
func sitOut(d time.Duration, countdown func(time.Duration) error) error {
	if d > 0 {
		return countdown(d)
	}
	return nil
}

// open reads the file's page and puts the name and size it gives on the task.
func (b *Backend) open(ctx context.Context, taskID string, s *Session, link string) (*Page, error) {
	p, err := b.client.Open(ctx, s, link)
	if p != nil && p.Name != "" {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: p.Name, Size: p.Size})
	}
	return p, err
}

// solve asks Solve and turns its failures into ErrNoAnswer, other than a
// download that stopped meanwhile.
func (b *Backend) solve(ctx context.Context, taskID string, c Captcha) (string, error) {
	if b.Solve == nil {
		return "", ErrNoAnswer
	}
	token, err := b.Solve(ctx, taskID, c)
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		return "", fmt.Errorf("%w: %v", ErrNoAnswer, err)
	}
	return token, nil
}

// handOn gives the link to the engine.
func (b *Backend) handOn(ctx context.Context, taskID, link, direct string, conns int) {
	if !b.Handing(ctx, taskID) {
		return
	}
	b.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		direct, _, err := b.unlock(ctx, taskID, link, b.Premium(), func(d time.Duration) error { return b.Sleep(ctx, d) })
		return direct, err
	})
}

// wait sits out d, keeping the time left in the task's note.
func (b *Backend) wait(ctx context.Context, taskID string, d time.Duration) error {
	return b.Wait(ctx, taskID, d, func(left time.Duration) string {
		return "Waiting for " + b.site.Name + ": " + hoster.Span(left) + " left"
	})
}

func (b *Backend) password(taskID string) string {
	if b.Password == nil {
		return ""
	}
	return b.Password(taskID)
}

func (b *Backend) credentials() (string, string, bool) {
	if b.Login == nil {
		return "", "", false
	}
	return b.Login()
}

func (b *Backend) loadSession() string {
	if b.LoadSession == nil {
		return ""
	}
	return b.LoadSession()
}

func (b *Backend) save(cookies string) {
	if b.SaveSession != nil {
		b.SaveSession(cookies)
	}
}

// fail reports err, named after the site, with the cause the backend knows.
func (b *Backend) fail(taskID string, err error) {
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: b.site.Name + ": " + err.Error(), Reason: reasonOf(err)})
}

// handBack hands the download to the next backend, which is JDownloader on
// the login once the reconciler has given it there.
func (b *Backend) handBack(taskID string, err error) {
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: b.site.Name + ": " + err.Error(),
		Unsupported: true, Takeover: b.site.Name})
}

func reasonOf(err error) core.Reason {
	var w *WaitError
	switch {
	case errors.Is(err, ErrGone):
		return core.ReasonGone
	case errors.Is(err, ErrTraffic):
		return core.ReasonLimit
	case errors.Is(err, ErrNoAnswer), errors.Is(err, ErrCaptchaRefused):
		return core.ReasonCaptcha
	case errors.Is(err, ErrPremiumOnly), errors.Is(err, ErrPassword), errors.Is(err, ErrPasswordRefused),
		errors.Is(err, ErrPremiumExpired), errors.Is(err, ErrLoginRefused), errors.Is(err, ErrSignInBlocked),
		errors.Is(err, ErrSignInFailed), errors.Is(err, ErrNoLogin):
		return core.ReasonAuth
	case errors.As(err, &w):
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}

// misread is how a failure on the login is weighed (see ownlogin.Handback):
// by its cause, except a sign-in the site neither took nor turned down in
// words the client knows, which may be the client misreading the page.
func misread(err error) core.Reason {
	if errors.Is(err, ErrSignInFailed) {
		return core.ReasonUnknown
	}
	return reasonOf(err)
}
