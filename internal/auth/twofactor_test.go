package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/secret"
)

// locked is a Guard in the only state a second factor is allowed to exist in:
// with a password already set.
func locked(t *testing.T) (*Guard, string) {
	t.Helper()
	dir := t.TempDir()
	g, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	return g, dir
}

func codeNow(t *testing.T, sec string) string {
	t.Helper()
	c, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestSecondFactorNeedsAFirstOne. GlimStone: a login gains a way IN, never a way
// INSTEAD. A second factor on an instance with no password protects nothing and
// only adds a way to be locked out, so the guard refuses to start the enrolment
// at all rather than leaving the card to remember.
func TestSecondFactorNeedsAFirstOne(t *testing.T) {
	g, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.BeginTwoFactor("KnightLoader", "this instance"); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("BeginTwoFactor with no password = %v, want ErrNoPassword", err)
	}
	if g.TwoFactorEnabled() {
		t.Error("the factor reads as armed on an instance with no password")
	}
}

// TestAHalfFinishedEnrolmentIsNotArmed is the rule about the status line, tested
// where the status line reads from. Showing a QR code must change nothing the
// authority answers with, or the card claims a protection that is not there and
// the next login refuses a code nobody has yet.
func TestAHalfFinishedEnrolmentIsNotArmed(t *testing.T) {
	g, dir := locked(t)
	sec, uri, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if sec == "" || !strings.Contains(uri, sec) {
		t.Fatalf("BeginTwoFactor gave secret %q and uri %q", sec, uri)
	}
	if g.TwoFactorEnabled() {
		t.Error("the factor reads as armed after nothing but a QR code was shown")
	}
	// And nothing reached the file either, so a restart in the middle leaves no
	// half-armed instance behind.
	var s stored
	b, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.TOTP != "" {
		t.Errorf("auth.json holds a secret after an unconfirmed enrolment: %q", s.TOTP)
	}
}

func TestEnrolmentArmsOnlyOnAConfirmedCode(t *testing.T) {
	g, dir := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor("000000"); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("ConfirmTwoFactor with a wrong code = %v, want ErrCodeRejected", err)
	}
	if g.TwoFactorEnabled() {
		t.Fatal("a refused code armed the factor anyway")
	}
	codes, err := g.ConfirmTwoFactor(codeNow(t, sec))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != secret.RecoveryCodeCount {
		t.Errorf("got %d recovery codes, want %d - the card promises this number before it shows them",
			len(codes), secret.RecoveryCodeCount)
	}
	if !g.TwoFactorEnabled() {
		t.Fatal("the factor is not armed after a confirmed code")
	}
	if g.RecoveryLeft() != secret.RecoveryCodeCount {
		t.Errorf("RecoveryLeft = %d, want %d", g.RecoveryLeft(), secret.RecoveryCodeCount)
	}

	// It survives a restart, which is the only reason it is on disk at all.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !again.TwoFactorEnabled() {
		t.Error("the factor is off again after a restart")
	}
	if !again.CheckSecond(codeNow(t, sec)) {
		t.Error("the reopened guard does not accept a code from the enrolled secret")
	}
}

// TestTheSecretIsNeverHandedBackOnceItIsArmed. It is shown exactly once, during
// the enrolment, and the screen that shows it says so. A second Begin while the
// factor is on would quietly hand out a fresh one and be a way to read the
// armed instance's secret out of an open session.
func TestTheSecretIsNeverHandedBackOnceItIsArmed(t *testing.T) {
	g, _ := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.BeginTwoFactor("KnightLoader", "this instance"); !errors.Is(err, ErrTwoFactorArmed) {
		t.Fatalf("BeginTwoFactor on an armed instance = %v, want ErrTwoFactorArmed", err)
	}
}

// TestTurningItOffCostsTheSameProofAsUsingIt. GlimStone states this as stricter
// than the ordinary confirmation rule and for a different reason: not regret,
// but a session somebody walked away from.
func TestTurningItOffCostsTheSameProofAsUsingIt(t *testing.T) {
	g, _ := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "000000", "correct-horse"} {
		if err := g.DisableTwoFactor(bad); !errors.Is(err, ErrCodeRejected) {
			t.Errorf("DisableTwoFactor(%q) = %v, want ErrCodeRejected", bad, err)
		}
	}
	if !g.TwoFactorEnabled() {
		t.Fatal("the factor came off without a code")
	}
	if err := g.DisableTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	if g.TwoFactorEnabled() {
		t.Error("the factor is still armed after a valid code turned it off")
	}
	if g.RecoveryLeft() != 0 {
		t.Errorf("RecoveryLeft = %d after turning the factor off, want 0 - a stale sheet of codes is a way in nobody remembers granting", g.RecoveryLeft())
	}
}

// TestARecoveryCodeWorksOnceAndThenIsGone. It is the way back in when the phone
// is lost, so it has to work; it is written on paper, so it must not work twice.
func TestARecoveryCodeWorksOnceAndThenIsGone(t *testing.T) {
	g, dir := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	codes, err := g.ConfirmTwoFactor(codeNow(t, sec))
	if err != nil {
		t.Fatal(err)
	}
	if !g.CheckSecond(codes[2]) {
		t.Fatal("a recovery code was refused the first time")
	}
	if g.RecoveryLeft() != secret.RecoveryCodeCount-1 {
		t.Errorf("RecoveryLeft = %d after one was spent, want %d", g.RecoveryLeft(), secret.RecoveryCodeCount-1)
	}
	if g.CheckSecond(codes[2]) {
		t.Error("the same recovery code was accepted twice")
	}
	// Spending one is a write, not only a change in memory: a code used just
	// before a restart must not come back.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.CheckSecond(codes[2]) {
		t.Error("a spent recovery code came back after a restart")
	}
	if again.RecoveryLeft() != secret.RecoveryCodeCount-1 {
		t.Errorf("RecoveryLeft after a restart = %d, want %d", again.RecoveryLeft(), secret.RecoveryCodeCount-1)
	}
	// And it turns the factor off, which is the other half of "a way back in".
	if err := again.DisableTwoFactor(codes[4]); err != nil {
		t.Errorf("DisableTwoFactor with a recovery code: %v", err)
	}
}

// TestRemovingThePasswordTakesTheSecondFactorWithIt. The factor hangs off the
// password; leaving it armed against a password that no longer exists would
// leave an instance that asks for a code and has nothing to add it to.
func TestRemovingThePasswordTakesTheSecondFactorWithIt(t *testing.T) {
	g, _ := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("correct-horse", ""); err != nil {
		t.Fatal(err)
	}
	if g.TwoFactorEnabled() {
		t.Error("the second factor is still armed after the password it guards was removed")
	}
}

// TestChangingThePasswordKeepsTheSecondFactor is the other side of the rule
// above, and the one that would be easy to get wrong by clearing on every
// SetPassword: rotating a password is not a reason to make somebody re-enrol a
// phone, and a factor that quietly switched itself off there would be a
// protection somebody believes in and does not have.
func TestChangingThePasswordKeepsTheSecondFactor(t *testing.T) {
	g, _ := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("correct-horse", "another-long-one"); err != nil {
		t.Fatal(err)
	}
	if !g.TwoFactorEnabled() {
		t.Error("changing the password disarmed the second factor")
	}
	if !g.CheckSecond(codeNow(t, sec)) {
		t.Error("the enrolled secret stopped working when the password changed")
	}
}

// TestClearTwoFactorIsTheWayBackIn is the documented escape hatch: somebody who
// can reach the data directory can turn the factor off without a code, because
// that same person could delete auth.json outright. It is the answer to the one
// question this screen has to answer for a tool with no second human in it -
// see cmd/knightloader's -reset-2fa flag, which is this method with a main()
// around it.
func TestClearTwoFactorIsTheWayBackIn(t *testing.T) {
	g, dir := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	if err := g.ClearTwoFactor(); err != nil {
		t.Fatal(err)
	}
	if g.TwoFactorEnabled() {
		t.Fatal("ClearTwoFactor left the factor armed")
	}
	// The password is untouched, which is the whole point: this is a way past
	// the second factor, never a way past the first one.
	if !g.Check("correct-horse") {
		t.Error("ClearTwoFactor removed the password as well")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.TwoFactorEnabled() {
		t.Error("the factor came back after a restart, so nothing was written")
	}
	if !again.Check("correct-horse") {
		t.Error("the password did not survive ClearTwoFactor")
	}
}

// TestCheckSecondAnswersNoWhileTheFactorIsOff. Nothing may be accepted as a
// second factor on an instance that has none, or an empty code would be a valid
// answer to a question nobody asked.
func TestCheckSecondAnswersNoWhileTheFactorIsOff(t *testing.T) {
	g, _ := locked(t)
	for _, s := range []string{"", "000000", "abcde-fghij"} {
		if g.CheckSecond(s) {
			t.Errorf("CheckSecond(%q) said yes while the factor is off", s)
		}
	}
}
