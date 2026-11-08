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
	Premium bool
	Expiry  time.Time
	// TrafficLeft and TrafficMax are bytes, as in Account.
	TrafficLeft, TrafficMax int64
}

// LoginError is the hoster turning the login down. The same login would be
// turned down again, and hosters block addresses that keep trying.
type LoginError struct {
	Msg string
	// Captcha is set when the hoster wants a captcha solved first, which
	// only the user can do.
	Captcha bool
}

func (e *LoginError) Error() string { return e.Msg }

// Signer is how a hoster's API signs a login in.
type Signer struct {
	// Login signs in and returns the token the other calls go with, "" for a
	// hoster that takes the password on every call, and the account.
	Login func(ctx context.Context, user, password string) (string, Account, error)
	// Account reads the account behind a token, which also proves the token
	// still works. A hoster without tokens leaves it nil.
	Account func(ctx context.Context, token string) (Account, error)
	// Expired reports whether an error of Account means the hoster has
	// forgotten the token.
	Expired func(error) bool
	// Age is how long a token is used before signing in afresh, 0 for as
	// long as the hoster takes it.
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
	// Checked is called when the hoster's word on the login changes what it
	// can download. Nil calls nothing.
	Checked func()

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
}

func NewLogin(s Signer) *Login { return &Login{s: s} }

// SetLogin stores the login downloads go out on, "" for none, and reports
// whether it differs from the one before. A different login starts unchecked.
func (l *Login) SetLogin(user, password string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if user == l.user && password == l.pass {
		return false
	}
	l.user, l.pass = user, password
	l.current, l.status, l.benched = Session{}, Status{}, time.Time{}
	return true
}

// Ready reports whether the stored login can be expected to download: there
// is one, the hoster has not turned it down or called it free, and it is not
// benched. It takes no lock but the login's, so the app may ask while holding
// its own.
func (l *Login) Ready() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.user != "" && !l.status.Refused && (!l.status.Checked || l.status.Premium) && time.Now().After(l.benched)
}

// Status is what the hoster made of the stored login.
func (l *Login) Status() Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

// Check asks the hoster about the stored login and returns its answer.
func (l *Login) Check(ctx context.Context) Status {
	_, _ = l.Session(ctx, false)
	return l.Status()
}

// Session returns a working session, the one in hand unless fresh is set or
// it is too old or the hoster has forgotten its token.
func (l *Login) Session(ctx context.Context, fresh bool) (Session, error) {
	l.smu.Lock()
	defer l.smu.Unlock()
	l.mu.Lock()
	user, pass, cur := l.user, l.pass, l.current
	l.mu.Unlock()
	if user == "" {
		return Session{}, l.s.None
	}
	if !fresh && cur.Token != "" && (l.s.Age == 0 || time.Since(cur.at) < l.s.Age) {
		acct, err := l.s.Account(ctx, cur.Token)
		if err == nil {
			cur.Account = acct
			l.record(user, pass, cur, nil)
			return cur, nil
		}
		if !l.s.Expired(err) {
			return Session{}, err
		}
	}
	token, acct, err := l.s.Login(ctx, user, pass)
	s := Session{Token: token, Account: acct, User: user, Password: pass, at: time.Now()}
	l.record(user, pass, s, err)
	if err != nil {
		return Session{}, err
	}
	return s, nil
}

// record keeps what signing in with user and pass came to, unless the login
// was changed meanwhile.
func (l *Login) record(user, pass string, s Session, err error) {
	var refused *LoginError
	if err != nil && !errors.As(err, &refused) {
		return
	}
	l.mu.Lock()
	if l.user != user || l.pass != pass {
		l.mu.Unlock()
		return
	}
	before := l.status
	if refused != nil {
		l.current = Session{}
		l.status = Status{Checked: true, Refused: true, Err: err.Error(), Captcha: refused.Captcha}
	} else {
		l.current = s
		a := s.Account
		l.status = Status{Checked: true, Premium: a.Premium, Expiry: a.Expiry,
			TrafficLeft: a.TrafficLeft, TrafficMax: a.TrafficMax}
	}
	changed := before.Checked != l.status.Checked || before.Refused != l.status.Refused || before.Premium != l.status.Premium
	l.mu.Unlock()
	if changed && l.Checked != nil {
		l.Checked()
	}
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
