package auth

// A forgotten password is reset with a code that the embedding shows only
// where reading takes access to the machine: the process's own output, or a
// file in the data directory. Somebody who only reaches the HTTP port never
// sees it. Whoever can read that output can reset the password and take the
// instance over, though, even with no other access at all: a read-only log
// viewer such as Dozzle, or a log shipper and everybody who reads what it
// collects. The manual tells the operator to keep that access to people who
// may own the instance.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// ResetCodeTTL is how long a code can be used. Asking again does not
	// extend it.
	ResetCodeTTL = 10 * time.Minute
	// resetCodeGap is the least time between two new codes, so asking cannot
	// fill the output.
	resetCodeGap = time.Minute

	// ResetCodeLen is twelve symbols, sixty bits. Wrong guesses do not spend a
	// code, since a code that a few guesses killed would let anybody who
	// reaches the page keep the owner from ever using one, and sixty bits
	// leave nothing to find in ten minutes at any rate HTTP allows.
	ResetCodeLen = 12
	// resetAlphabet has no 0, O, 1 or I, which look alike in a log window. Its
	// 32 symbols take five bits of a random byte each without bias.
	resetAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	// noResetCode stands in for the code while none is live, so a guess costs
	// the same comparison either way. It has the length of a code and symbols
	// no code has.
	noResetCode = "000000000000"
)

// ErrNothingToReset refuses a reset on an instance without a password.
var ErrNothingToReset = errors.New("no password is set on this instance")

// resetCode is the one code that can be live at a time. It is kept in memory
// like a pending enrolment, so a restart cancels a reset that was under way.
type resetCode struct {
	code    string
	expires time.Time
}

func (c resetCode) live(now time.Time) bool {
	return c.code != "" && now.Before(c.expires)
}

// matches reports whether typed is the live code, as the log shows it or
// without its dashes, in either case.
func (c resetCode) matches(typed string, now time.Time) bool {
	live := c.live(now)
	want := noResetCode
	if live {
		want = c.code
	}
	typed = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(typed))
	return subtle.ConstantTimeCompare([]byte(typed), []byte(want)) == 1 && live
}

func newResetCode() (string, error) {
	buf := make([]byte, ResetCodeLen)
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
	g.reset = resetCode{code: code, expires: now.Add(ResetCodeTTL)}
	g.resetMinted = now
	return code, nil
}

// ResetPending reports whether a reset code is live.
func (g *Guard) ResetPending() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.reset.live(time.Now())
}

// CancelReset drops a code that could not be shown, so the next request mints
// one instead of waiting out a code nobody can read.
func (g *Guard) CancelReset() {
	g.mu.Lock()
	g.reset = resetCode{}
	g.resetMinted = time.Time{}
	g.mu.Unlock()
}

// FinishReset sets next as the password when code is the live reset code,
// and returns the credentials a session for the caller is issued against.
//
// The reset leaves whoever holds the code alone in the instance: the second
// factor goes, since the authenticator may have gone with the password, every
// session ends and the signing key is replaced, so a copy of auth.json taken
// earlier signs nothing. auth.json is written before anything changes in
// memory, and a failed write leaves the old state and the code in place.
// alsoEnd runs while nothing can be minted (see Mint), for the credentials
// this package does not keep.
//
// A wrong code, an expired one and no code at all are the same
// ErrCodeRejected. A password the hash would refuse is refused before the code
// is looked at.
func (g *Guard) FinishReset(code, next string, alsoEnd func()) (Credentials, error) {
	if len(next) < 8 {
		return Credentials{}, ErrTooShort
	}
	if len(next) > 72 {
		return Credentials{}, bcrypt.ErrPasswordTooLong
	}
	g.mu.RLock()
	enabled := len(g.hash) > 0
	ok := g.reset.matches(code, time.Now())
	g.mu.RUnlock()
	if !enabled {
		return Credentials{}, ErrNothingToReset
	}
	if !ok {
		return Credentials{}, ErrCodeRejected
	}
	// Hashed only for a code that was right, so the route cannot be made to
	// spend a bcrypt per request.
	h, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return Credentials{}, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return Credentials{}, err
	}

	g.minting.Lock()
	defer g.minting.Unlock()
	g.mu.Lock()
	// Another request may have used the code while this one hashed.
	if !g.reset.matches(code, time.Now()) {
		g.mu.Unlock()
		return Credentials{}, ErrCodeRejected
	}
	if err := writeState(g.path, stored{Hash: string(h), Key: hex.EncodeToString(key), Epoch: g.epoch + 1}); err != nil {
		g.mu.Unlock()
		return Credentials{}, err
	}
	g.hash = h
	g.key = key
	g.totp = ""
	g.recovery = nil
	g.pending = nil
	g.endSessionsLocked()
	g.reset = resetCode{}
	g.gen++
	c := Credentials{gen: g.gen, enabled: true}
	g.mu.Unlock()

	if alsoEnd != nil {
		alsoEnd()
	}
	return c, nil
}
