package auth

// The second factor is a six-digit authenticator code next to the password.
// KnightLoader has no user accounts and so no administrator to unlock a
// locked-out owner, which shapes this file: enrolment writes nothing until a
// code is confirmed, recovery codes are minted when the factor is armed, and
// ClearTwoFactor (knightloader -reset-2fa, or editing auth.json with the app
// stopped) removes it with no code. That needs write access to the data
// directory, which could delete auth.json outright, so it grants nothing new.

import (
	"errors"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/secret"
)

var (
	// ErrNoPassword refuses a second factor on an instance without a
	// password, where it would guard nothing.
	ErrNoPassword = errors.New("set a password before adding a second factor")
	// ErrTwoFactorArmed refuses a second enrolment while one is in force, so
	// an open session cannot read a fresh secret.
	ErrTwoFactorArmed = errors.New("the second factor is already on")
	// ErrNoEnrolment is a confirmation with nothing to confirm, after a
	// restart or once the enrolment window has passed.
	ErrNoEnrolment = errors.New("start the enrolment again")
	// ErrCodeRejected covers a wrong code, a spent recovery code and an empty
	// field alike, so a caller cannot tell how close a guess was.
	ErrCodeRejected = errors.New("that code was not accepted")
)

// enrolmentTTL bounds how long a started enrolment stays confirmable.
const enrolmentTTL = 10 * time.Minute

// pending is an enrolment whose secret has been shown but not confirmed. It
// lives only in memory, so an abandoned enrolment leaves no half-armed state.
type pending struct {
	secret  string
	expires time.Time
}

// TwoFactorEnabled reports whether a code is required at login.
func (g *Guard) TwoFactorEnabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totp != ""
}

// RecoveryLeft is how many single-use codes are still unspent.
func (g *Guard) RecoveryLeft() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.recovery)
}

// BeginTwoFactor mints a candidate secret and returns it with the otpauth URI
// an authenticator app scans. Nothing is armed or written until
// ConfirmTwoFactor accepts a code from it.
func (g *Guard) BeginTwoFactor(issuer, account string) (secretB32, uri string, err error) {
	if !g.Enabled() {
		return "", "", ErrNoPassword
	}
	if g.TwoFactorEnabled() {
		return "", "", ErrTwoFactorArmed
	}
	s, err := secret.NewTOTPSecret()
	if err != nil {
		return "", "", err
	}
	g.mu.Lock()
	g.pending = &pending{secret: s, expires: time.Now().Add(enrolmentTTL)}
	g.mu.Unlock()
	return s, secret.TOTPURI(issuer, account, s), nil
}

// ConfirmTwoFactor arms the factor if code matches the pending secret, and
// returns the recovery codes in plain text. Only their hashes are kept.
func (g *Guard) ConfirmTwoFactor(code string) ([]string, error) {
	g.mu.Lock()
	p := g.pending
	if p == nil || time.Now().After(p.expires) {
		g.pending = nil
		g.mu.Unlock()
		return nil, ErrNoEnrolment
	}
	if g.totp != "" {
		g.mu.Unlock()
		return nil, ErrTwoFactorArmed
	}
	if !secret.ValidTOTP(p.secret, code, time.Now()) {
		g.mu.Unlock()
		return nil, ErrCodeRejected
	}
	key := string(g.key)
	g.mu.Unlock()

	plain, hashed, err := secret.NewRecoveryCodes(key)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	// Two racing confirmations must not both arm, or the first caller's
	// recovery codes would open nothing.
	if g.totp != "" {
		g.mu.Unlock()
		return nil, ErrTwoFactorArmed
	}
	g.totp = p.secret
	g.recovery = hashed
	g.pending = nil
	g.mu.Unlock()

	if err := g.flush(); err != nil {
		return nil, err
	}
	return plain, nil
}

// DisableTwoFactor turns the factor off. It takes the same proof as a login,
// a live code or a recovery code, because the threat is a session somebody
// walked away from.
func (g *Guard) DisableTwoFactor(proof string) error {
	if !g.TwoFactorEnabled() {
		return nil
	}
	if !g.CheckSecond(proof) {
		return ErrCodeRejected
	}
	return g.ClearTwoFactor()
}

// ClearTwoFactor removes the factor without a code, for an owner who has lost
// both the phone and the recovery codes. The password is untouched.
func (g *Guard) ClearTwoFactor() error {
	g.mu.Lock()
	g.totp = ""
	g.recovery = nil
	g.pending = nil
	g.mu.Unlock()
	return g.flush()
}

// CheckSecond verifies a live code or a recovery code, spending the latter.
// It answers false while the factor is off, because the login route calls it
// before knowing whether the instance is armed.
func (g *Guard) CheckSecond(code string) bool {
	g.mu.RLock()
	sec, stored, key := g.totp, g.recovery, string(g.key)
	g.mu.RUnlock()
	if sec == "" {
		return false
	}
	if secret.ValidTOTP(sec, code, time.Now()) {
		return true
	}
	idx := secret.MatchRecoveryCode(key, code, stored)
	if idx < 0 {
		return false
	}

	g.mu.Lock()
	// Match again under the lock: another request may have spent a code in
	// between and shifted the indexes, or spent this one.
	spend := secret.MatchRecoveryCode(key, code, g.recovery)
	if spend < 0 {
		g.mu.Unlock()
		return false
	}
	g.recovery = append(g.recovery[:spend:spend], g.recovery[spend+1:]...)
	g.mu.Unlock()
	// A failed write must not refuse an accepted code; the cost is that the
	// code survives a restart.
	_ = g.flush()
	return true
}
