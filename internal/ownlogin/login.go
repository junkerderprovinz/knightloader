// Package ownlogin keeps a hoster login KnightLoader signs in with itself,
// rather than handing it to JDownloader, and runs the downloads that go out on
// it: the session, what the hoster says about the account, the waits the
// hoster asks for and the handover of the download link to the engine. The
// hoster packages bring the API calls.
package ownlogin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Account is what a hoster says about a login.
type Account struct {
	Premium bool
	Expiry  time.Time
	// TrafficLeft and TrafficMax are bytes. TrafficMax is 0 where the hoster
	// names no total.
	TrafficLeft, TrafficMax int64
}

// Status is what the hoster made of the stored login the last time it was
// asked.
type Status struct {
	// Checked is set once the hoster has answered for the login.
	Checked bool
	// Refused is the hoster turning the login down, and Err says why.
	Refused bool
	Err     string
	// Captcha is a refusal that lasts until the user solves a captcha at the
	// hoster.
	Captcha bool
	// Activate and NewAddress are refusals that last until the user opens
	// the link the hoster mailed, as in LoginError.
	Activate, NewAddress bool
	// Code is a refusal that lasts until the user enters the one-time code
	// of a second factor, and says why.
	Code CodeAsk
	// Handed is a login KnightLoader's own client could not use and gave to
	// JDownloader (see Handback).
	Handed  bool
	Premium bool
	Expiry  time.Time
	// TrafficLeft and TrafficMax are bytes, as in Account.
	TrafficLeft, TrafficMax int64
}

// CodeAsk is why a login waits for the one-time code of a second factor.
type CodeAsk string

const (
	// CodeNeeded is a login the hoster takes only with a code.
	CodeNeeded CodeAsk = "needed"
	// CodeWrong is a code the hoster turned down.
	CodeWrong CodeAsk = "wrong"
	// CodeEnded is a session opened with a code that the hoster has ended.
	CodeEnded CodeAsk = "ended"
)

// The refusals of a login that signs in with a second factor. A hoster's
// LoginError wraps the first two, and Login makes the third.
var (
	ErrNeedsCode = errors.New("the login asks for a two-factor code")
	ErrWrongCode = errors.New("the hoster turned the two-factor code down")
	ErrNewCode   = errors.New("the hoster ended the session, and signing in again takes a new two-factor code")
)

// LoginError is the hoster turning the login down. The same login would be
// turned down again, and hosters block addresses that keep trying.
type LoginError struct {
	Msg string
	// Captcha is set when the hoster wants a captcha solved first, which
	// only the user can do.
	Captcha bool
	// Activate is an account the hoster wants activated through the link
	// of its welcome mail, and NewAddress a sign-in from an address the
	// hoster wants confirmed through a link it mailed.
	Activate, NewAddress bool
	// Err is ErrNeedsCode, ErrWrongCode or ErrNewCode for a login waiting
	// for a code.
	Err error
}

func (e *LoginError) Error() string { return e.Msg }

func (e *LoginError) Unwrap() error { return e.Err }

// codeAsk is the code a refusal waits for, "" for none.
func codeAsk(err error) CodeAsk {
	switch {
	case errors.Is(err, ErrWrongCode):
		return CodeWrong
	case errors.Is(err, ErrNewCode):
		return CodeEnded
	case errors.Is(err, ErrNeedsCode):
		return CodeNeeded
	}
	return ""
}

// Signer is how a hoster's API signs a login in.
type Signer struct {
	// Login signs in, with the one-time code of a second factor where code
	// is set, and returns the token the other calls go with, "" for a hoster
	// that takes the password on every call, and the account.
	Login func(ctx context.Context, user, password, code string) (string, Account, error)
	// Account reads the account behind a token, which also proves the token
	// still works. A hoster without tokens leaves it nil.
	Account func(ctx context.Context, token string) (Account, error)
	// Reason is the cause a failure on the login is reported and weighed
	// with (see Handback.Fail).
	Reason func(error) core.Reason
	// Expired reports whether an error of Account means the hoster has
	// forgotten the token.
	Expired func(error) bool
	// Age is how long a token is used before signing in afresh, 0 for as
	// long as the hoster takes it. A token opened with a code is kept as
	// long as the hoster takes it, since signing in afresh needs a new code.
	Age time.Duration
	// None is the error a download without a stored login fails with.
	None error
}

// Session is a signed-in login.
type Session struct {
	Token   string
	Account Account
	// User and Password are the login it was signed in with, for a hoster
	// that takes the password on every call.
	User, Password string
	at             time.Time
}

// Login is the one stored login of a hoster and what the hoster said about
// it.
type Login struct {
	Handback

	// Checked is called when the hoster's word on the login changes what it
	// can download. Nil calls nothing.
	Checked func()

	// Saved hears the token of a session opened with a code whenever it
	// changes, "" once there is none, so it can be kept across a restart and
	// handed back through RestoreSession. Nil keeps it in memory only.
	Saved func(user, password, token string)

	s Signer

	// smu serialises signing in, so downloads that start together share one
	// sign-in.
	smu sync.Mutex

	mu      sync.Mutex
	user    string
	pass    string
	current Session
	status  Status
	benched time.Time
	// coded is a login the hoster takes only with a code, which cannot be
	// signed in again without the user.
	coded bool
	// asked is the refusal a login waiting for a code is answered with
	// until the user enters one.
	asked error
}

func NewLogin(s Signer) *Login { return &Login{s: s} }

// SetLogin stores the login downloads go out on, "" for none, and reports
// whether it differs from the one before. A different login starts unchecked.
func (l *Login) SetLogin(user, password string) bool {
	l.mu.Lock()
	if user == l.user && password == l.pass {
		l.mu.Unlock()
		return false
	}
	l.user, l.pass = user, password
	l.current, l.status, l.benched = Session{}, Status{}, time.Time{}
	l.coded, l.asked = false, nil
	l.mu.Unlock()
	l.Reclaim()
	return true
}

// Ready reports whether the stored login can be expected to download: there
// is one, the hoster has not turned it down or called it free, and it is
// neither benched nor with JDownloader. It takes no lock but the login's, so
// the app may ask while holding its own.
func (l *Login) Ready() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.user != "" && !l.status.Refused && (!l.status.Checked || l.status.Premium) &&
		time.Now().After(l.benched) && !l.Handed()
}

// Status is what the hoster made of the stored login.
func (l *Login) Status() Status {
	l.mu.Lock()
	st := l.status
	l.mu.Unlock()
	st.Handed = l.Handed()
	return st
}

// Check asks the hoster about the stored login and returns its answer. A
// login with JDownloader comes back once the answer reads, a refusal
// included, and a login whose answer does not read goes there.
func (l *Login) Check(ctx context.Context) Status {
	_, err := l.Session(ctx, false)
	return l.settle(err)
}

// SignIn signs the stored login in afresh with the one-time code of its
// second factor and returns the hoster's answer. The code is used once and
// kept nowhere.
func (l *Login) SignIn(ctx context.Context, code string) Status {
	l.smu.Lock()
	l.mu.Lock()
	user, pass := l.user, l.pass
	l.mu.Unlock()
	var err error
	if user == "" {
		err = l.s.None
	} else {
		_, err = l.signIn(ctx, user, pass, code)
	}
	l.smu.Unlock()
	return l.settle(err)
}

// settle weighs what a check came to, as Check describes.
func (l *Login) settle(err error) Status {
	var refused *LoginError
	if err == nil || errors.As(err, &refused) {
		l.Reclaim()
	} else {
		l.Fail(err, l.s.Reason(err))
	}
	return l.Status()
}

// Session returns a working session, the one in hand unless fresh is set or
// it is too old or the hoster has forgotten its token.
func (l *Login) Session(ctx context.Context, fresh bool) (Session, error) {
	l.smu.Lock()
	defer l.smu.Unlock()
	l.mu.Lock()
	user, pass, cur, coded, asked := l.user, l.pass, l.current, l.coded, l.asked
	l.mu.Unlock()
	if user == "" {
		return Session{}, l.s.None
	}
	// Without a new code the hoster would answer the same, and hosters
	// lock accounts that keep trying.
	if asked != nil {
		return Session{}, asked
	}
	if cur.Token != "" && (coded || !fresh && (l.s.Age == 0 || time.Since(cur.at) < l.s.Age)) {
		acct, err := l.s.Account(ctx, cur.Token)
		if err == nil {
			cur.Account = acct
			l.record(user, pass, cur, nil, false)
			return cur, nil
		}
		if !l.s.Expired(err) {
			return Session{}, err
		}
	}
	if coded {
		err := &LoginError{Msg: ErrNewCode.Error(), Err: ErrNewCode}
		l.record(user, pass, Session{}, err, false)
		return Session{}, err
	}
	return l.signIn(ctx, user, pass, "")
}

// signIn signs user and pass in with code, "" for none. The caller holds
// smu.
func (l *Login) signIn(ctx context.Context, user, pass, code string) (Session, error) {
	token, acct, err := l.s.Login(ctx, user, pass, code)
	s := Session{Token: token, Account: acct, User: user, Password: pass, at: time.Now()}
	l.record(user, pass, s, err, code != "")
	if err != nil {
		return Session{}, err
	}
	return s, nil
}

// record keeps what signing in with user and pass came to, unless the login
// was changed meanwhile. coded is a sign-in the hoster took a code for.
func (l *Login) record(user, pass string, s Session, err error, coded bool) {
	var refused *LoginError
	if err != nil && !errors.As(err, &refused) {
		return
	}
	l.mu.Lock()
	if l.user != user || l.pass != pass {
		l.mu.Unlock()
		return
	}
	before, token := l.status, l.current.Token
	if refused != nil {
		ask := codeAsk(err)
		l.current, l.asked = Session{}, nil
		if ask != "" {
			l.coded, l.asked = true, err
		}
		l.status = Status{Checked: true, Refused: true, Err: err.Error(), Captcha: refused.Captcha,
			Activate: refused.Activate, NewAddress: refused.NewAddress, Code: ask}
	} else {
		if coded {
			l.coded = true
		}
		l.asked = nil
		l.current = s
		a := s.Account
		l.status = Status{Checked: true, Premium: a.Premium, Expiry: a.Expiry,
			TrafficLeft: a.TrafficLeft, TrafficMax: a.TrafficMax}
	}
	changed := before.Checked != l.status.Checked || before.Refused != l.status.Refused || before.Premium != l.status.Premium ||
		before.Code != l.status.Code
	keep := l.coded && l.current.Token != token
	token = l.current.Token
	l.mu.Unlock()
	if keep && l.Saved != nil {
		l.Saved(user, pass, token)
	}
	if changed && l.Checked != nil {
		l.Checked()
	}
}

// RestoreSession takes up a session a code opened for the stored login
// before, as Saved heard it. The next check asks the hoster whether it still
// holds, and a session it has ended asks for a new code.
func (l *Login) RestoreSession(token string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.user == "" {
		return
	}
	l.current = Session{Token: token, User: l.user, Password: l.pass, at: time.Now()}
	l.coded, l.asked = true, nil
}

// Bench takes the login out of routing for d, so the next links go another
// way until it is tried again.
func (l *Login) Bench(d time.Duration) {
	l.mu.Lock()
	l.benched = time.Now().Add(d)
	l.mu.Unlock()
	if l.Checked != nil {
		l.Checked()
	}
}

// SizeText writes a size in binary units, as the hosters do.
func SizeText(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 3 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGT"[i])
}
