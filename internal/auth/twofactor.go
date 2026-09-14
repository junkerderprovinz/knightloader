package auth

// The second factor: a six-digit code from an authenticator app, standing
// BESIDE the password and never in its place.
//
// ---------------------------------------------------------------------------
// WHAT IS DIFFERENT HERE FROM EVERY TUTORIAL ON THE SUBJECT, and it decides the
// whole shape of this file: KnightLoader has no user accounts. There is one
// password for the instance, and there is nobody else to unlock anything. So
// the failure this screen is actually about - somebody locking themselves out
// of their own downloader - has no administrator to appeal to, and every part
// of the design below is an answer to that:
//
//   - the enrolment writes NOTHING until a code has been confirmed, so a QR code
//     shown and then abandoned cannot leave a half-armed instance behind;
//   - eight single-use recovery codes are minted at the same moment the factor
//     is armed, shown once, and stored only as HMACs;
//   - and, because paper gets lost too, ClearTwoFactor is the way back that
//     needs no code at all. It is reachable from the command line
//     (`knightloader -reset-2fa`) and, one layer below that, by deleting two
//     fields from auth.json with the app stopped. Both of those need write
//     access to the data directory, which is the same access that could delete
//     auth.json outright and remove the password with it - so this grants
//     nothing that was not already granted, and it is documented rather than
//     hidden. Hiding it would only mean the honest answer to a locked-out
//     operator is "restore a backup".
// ---------------------------------------------------------------------------

import (
	"errors"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/secret"
)

var (
	// ErrNoPassword refuses a second factor on an instance that has no first
	// one. It would guard nothing and could only lock somebody out.
	ErrNoPassword = errors.New("set a password before adding a second factor")
	// ErrTwoFactorArmed refuses a second enrolment while one is in force. The
	// secret is shown exactly once; handing out a fresh one from an open session
	// would be a way to read the armed instance's secret back.
	ErrTwoFactorArmed = errors.New("the second factor is already on")
	// ErrNoEnrolment is a confirmation with nothing to confirm - a restart in
	// the middle, or a browser tab left open past the enrolment window.
	ErrNoEnrolment = errors.New("start the enrolment again")
	// ErrCodeRejected covers a wrong code, a spent recovery code and an empty
	// field. Deliberately one error: telling a caller WHICH of those it was is
	// telling an attacker whether a guess was close.
	ErrCodeRejected = errors.New("that code was not accepted")
)

// enrolmentTTL bounds how long a started enrolment stays confirmable. Long
// enough to find a phone, scan the code and type the six digits; short enough
// that a secret sitting in a process's memory for an afternoon is not a thing
// this app does.
const enrolmentTTL = 10 * time.Minute

// pending is an enrolment that has shown its secret and not yet had a code
// confirmed. In memory only, and that is the design rather than a shortcut: an
// unconfirmed secret is worth nothing, and writing it would create exactly the
// half-armed state the status line must never report.
type pending struct {
	secret  string
	expires time.Time
}

// TwoFactorEnabled reports whether a code is required at the login. This is the
// authority the interface's status line reads - never how far the enrolment has
// got on screen.
func (g *Guard) TwoFactorEnabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totp != ""
}

// RecoveryLeft is how many single-use codes are still unspent. Zero while the
// factor is off.
func (g *Guard) RecoveryLeft() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.recovery)
}

// BeginTwoFactor mints a candidate secret and returns it with the otpauth URI an
// authenticator app scans. Nothing is armed and nothing is written until
// ConfirmTwoFactor accepts a code produced from it.
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

// ConfirmTwoFactor arms the factor if code was produced by the pending secret,
// and returns the recovery codes in plain text. They are returned exactly once:
// only their hashes are kept.
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

	// Minted outside the lock, because it reads crypto/rand eight times and
	// nothing else in this process should be waiting on the guard for that.
	plain, hashed, err := secret.NewRecoveryCodes(key)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	// Re-checked under the lock: two confirmations racing must not both arm, or
	// the second one's sheet of recovery codes would be the only valid one and
	// the first person would be holding paper that opens nothing.
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

// DisableTwoFactor turns the factor off, and costs the same proof as using it: a
// live code from the app, or one of the recovery codes.
//
// Stricter than this app's ordinary "are you sure" windows, and for a different
// reason. Those guard against regret; this guards against a session somebody
// walked away from, where the window and the button are both available to
// whoever sits down next.
func (g *Guard) DisableTwoFactor(proof string) error {
	if !g.TwoFactorEnabled() {
		return nil
	}
	if !g.CheckSecond(proof) {
		return ErrCodeRejected
	}
	return g.ClearTwoFactor()
}

// ClearTwoFactor removes the factor with no code at all. It is the way back in
// for somebody who has lost both the phone and the paper, reachable from the
// command line and by editing the file - see this file's own header for why
// that grants nothing that filesystem access did not already grant.
//
// The password is untouched. This is a way past the SECOND factor and never a
// way past the first.
func (g *Guard) ClearTwoFactor() error {
	g.mu.Lock()
	g.totp = ""
	g.recovery = nil
	g.pending = nil
	g.mu.Unlock()
	return g.flush()
}

// CheckSecond verifies a second-factor answer: a live code from the app, or one
// of the recovery codes, which is then spent.
//
// It answers false while the factor is off. That is not a formality - an empty
// string must never read as a valid answer to a question nobody asked, and the
// login route calls this before it knows whether the instance is armed.
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
	// Found again under the lock rather than trusting the index from the read
	// above: another request may have spent a code in between, which would move
	// every index after it.
	spend := secret.MatchRecoveryCode(key, code, g.recovery)
	if spend < 0 {
		// Somebody else used this exact code first. One use is one use.
		g.mu.Unlock()
		return false
	}
	g.recovery = append(g.recovery[:spend:spend], g.recovery[spend+1:]...)
	g.mu.Unlock()
	// Best-effort: a code that has been accepted has been accepted, and refusing
	// the login because the disk is full would be the wrong answer to that. The
	// cost of a failed write is that the code survives a restart.
	_ = g.flush()
	return true
}
