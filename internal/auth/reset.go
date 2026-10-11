package auth

// A forgotten password is reset with a code that the embedding shows only
// where reading takes access to the machine: the process's own output, or a
// file in the data directory. Whoever can read either could delete auth.json
// outright, so the code grants nothing new, and somebody who only reaches the
// HTTP port never sees it.

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// ResetCodeTTL is how long a code can be used. Asking again does not
	// extend it.
	ResetCodeTTL = 10 * time.Minute
	// ResetCodeTries is how many wrong guesses a code survives.
	ResetCodeTries = 5
	// resetCodeGap is the least time between two new codes. With the tries it
	// bounds a guesser to five attempts a minute against forty bits, and the
	// output to one code a minute however often somebody asks.
	resetCodeGap = time.Minute

	resetCodeLen = 8
	// resetAlphabet has no 0, O, 1 or I, which look alike in a log window. Its
	// 32 symbols take five bits of a random byte each without bias.
	resetAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	// noResetCode stands in for the code while none is live, so a guess costs
	// the same comparison either way. It has the length of a code and symbols
	// no code has.
	noResetCode = "00000000"
)

// ErrNothingToReset refuses a reset on an instance without a password.
var ErrNothingToReset = errors.New("no password is set on this instance")

// resetCode is the one code that can be live at a time. It is kept in memory
// like a pending enrolment, so a restart cancels a reset that was under way.
type resetCode struct {
	code    string
	expires time.Time
	tries   int
}

func (c *resetCode) live(now time.Time) bool {
	return c.code != "" && now.Before(c.expires)
}

// spend reports whether typed is the live code, which is then gone. A wrong
// guess costs one try, and the last one takes the code with it.
func (c *resetCode) spend(typed string, now time.Time) bool {
	live := c.live(now)
	want := noResetCode
	if live {
		want = c.code
	}
	// The code is shown in capitals with a dash in the middle.
	typed = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(typed))
	match := subtle.ConstantTimeCompare([]byte(typed), []byte(want)) == 1
	if !live || match {
		*c = resetCode{}
		return live && match
	}
	if c.tries--; c.tries == 0 {
		*c = resetCode{}
	}
	return false
}

func newResetCode() (string, error) {
	buf := make([]byte, resetCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = resetAlphabet[b&31]
	}
	return string(buf), nil
}

// StartReset mints a reset code and returns it for the caller to show. It
// returns "" when there is nothing new to show: a code is still live, or the
// last one was minted less than resetCodeGap ago. The caller answers the same
// in both cases, so asking tells nobody whether a reset is under way.
func (g *Guard) StartReset() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.hash) == 0 {
		return "", ErrNothingToReset
	}
	now := time.Now()
	if g.reset.live(now) || now.Sub(g.resetMinted) < resetCodeGap {
		return "", nil
	}
	code, err := newResetCode()
	if err != nil {
		return "", err
	}
	g.reset = resetCode{code: code, expires: now.Add(ResetCodeTTL), tries: ResetCodeTries}
	g.resetMinted = now
	return code, nil
}

// CancelReset drops a code that could not be shown, so the next request mints
// one instead of waiting out a code nobody can read.
func (g *Guard) CancelReset() {
	g.mu.Lock()
	g.reset = resetCode{}
	g.resetMinted = time.Time{}
	g.mu.Unlock()
}

// FinishReset sets next as the password when code is the live reset code. It
// also removes the second factor, since whoever lost the password may have
// lost the authenticator with it, and ends every session.
//
// A wrong code, an expired one, a spent one and no code at all are the same
// ErrCodeRejected. A password that is too short is refused before the code is
// looked at and costs no try.
func (g *Guard) FinishReset(code, next string) error {
	if len(next) < 8 {
		return ErrTooShort
	}
	// bcrypt would refuse it after the code is spent.
	if len(next) > 72 {
		return bcrypt.ErrPasswordTooLong
	}
	g.mu.Lock()
	enabled := len(g.hash) > 0
	ok := g.reset.spend(code, time.Now())
	g.mu.Unlock()
	if !enabled {
		return ErrNothingToReset
	}
	if !ok {
		return ErrCodeRejected
	}
	// Hashed only for a code that was right, so the route cannot be made to
	// spend a bcrypt per request.
	h, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.hash = h
	g.totp = ""
	g.recovery = nil
	g.pending = nil
	g.endSessionsLocked()
	g.mu.Unlock()
	return g.flush()
}
