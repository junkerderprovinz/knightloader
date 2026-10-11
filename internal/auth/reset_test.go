package auth

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

const resetPassword = "a-new-long-password"

var resetCodeShape = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{8}$`)

// askedFor returns a Guard with a password and the reset code just minted
// for it.
func askedFor(t *testing.T) (*Guard, string, string) {
	t.Helper()
	g, dir := locked(t)
	code, err := g.StartReset()
	if err != nil {
		t.Fatal(err)
	}
	if !resetCodeShape.MatchString(code) {
		t.Fatalf("StartReset minted %q, want eight symbols of the alphabet", code)
	}
	return g, dir, code
}

func TestThereIsNothingToResetWithoutAPassword(t *testing.T) {
	g, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if code, err := g.StartReset(); !errors.Is(err, ErrNothingToReset) || code != "" {
		t.Fatalf("StartReset with no password = %q, %v, want ErrNothingToReset", code, err)
	}
	if err := g.FinishReset("ABCDEFGH", resetPassword); !errors.Is(err, ErrNothingToReset) {
		t.Fatalf("FinishReset with no password = %v, want ErrNothingToReset", err)
	}
	if g.Enabled() {
		t.Error("a reset on an open instance set a password")
	}
}

func TestTheResetCodeSetsThePassword(t *testing.T) {
	g, dir, code := askedFor(t)

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the right code was refused: %v", err)
	}
	if !g.Check(resetPassword) {
		t.Error("the new password does not sign in")
	}
	if g.Check("correct-horse") {
		t.Error("the old password still signs in")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Check(resetPassword) {
		t.Error("the new password did not reach auth.json")
	}
}

func TestAResetEndsEverySession(t *testing.T) {
	g, _, code := askedFor(t)
	before := g.Issue()

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatal(err)
	}
	if g.Valid(before) {
		t.Error("a session from before the reset is still valid")
	}
}

// TestAResetSwitchesTheSecondFactorOff: whoever lost the password may have
// lost the authenticator with it, and reading the code took more than either.
func TestAResetSwitchesTheSecondFactorOff(t *testing.T) {
	g, dir := locked(t)
	sec, _, err := g.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(codeNow(t, sec)); err != nil {
		t.Fatal(err)
	}
	code, err := g.StartReset()
	if err != nil {
		t.Fatal(err)
	}

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatal(err)
	}
	if g.TwoFactorEnabled() || g.RecoveryLeft() != 0 {
		t.Error("the second factor outlived the reset")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.TwoFactorEnabled() || again.RecoveryLeft() != 0 {
		t.Error("the second factor is still in auth.json")
	}
}

func TestAWrongResetCodeChangesNothing(t *testing.T) {
	g, _, code := askedFor(t)
	session := g.Issue()
	wrong := "2222-2222"
	if strings.ReplaceAll(wrong, "-", "") == code {
		wrong = "3333-3333"
	}

	if err := g.FinishReset(wrong, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("a wrong code = %v, want ErrCodeRejected", err)
	}
	if !g.Check("correct-horse") || g.Check(resetPassword) {
		t.Error("a wrong code changed the password")
	}
	if !g.Valid(session) {
		t.Error("a wrong code ended a session")
	}
}

func TestAResetCodeWorksOnce(t *testing.T) {
	g, _, code := askedFor(t)
	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatal(err)
	}

	if err := g.FinishReset(code, "another-long-password"); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the second use of a code = %v, want ErrCodeRejected", err)
	}
	if !g.Check(resetPassword) {
		t.Error("the second use of the code changed the password")
	}
}

func TestAResetCodeExpires(t *testing.T) {
	g, _, code := askedFor(t)
	g.reset.expires = time.Now().Add(-time.Second)

	if err := g.FinishReset(code, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("an expired code = %v, want ErrCodeRejected", err)
	}
	if !g.Check("correct-horse") {
		t.Error("an expired code changed the password")
	}
}

func TestWrongGuessesSpendTheResetCode(t *testing.T) {
	g, _, code := askedFor(t)
	for range ResetCodeTries {
		if err := g.FinishReset("not-the-code", resetPassword); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("a wrong guess = %v, want ErrCodeRejected", err)
		}
	}

	if err := g.FinishReset(code, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the code after %d wrong guesses = %v, want ErrCodeRejected", ResetCodeTries, err)
	}
}

func TestTheLastTryStillTakesTheRightCode(t *testing.T) {
	g, _, code := askedFor(t)
	for range ResetCodeTries - 1 {
		_ = g.FinishReset("not-the-code", resetPassword)
	}

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the right code on the last try was refused: %v", err)
	}
}

// TestAskingAgainNeitherReplacesNorExtendsTheCode: a second request must not
// take the code away from whoever is typing it, and must not give a guesser
// more time or more tries with it.
func TestAskingAgainNeitherReplacesNorExtendsTheCode(t *testing.T) {
	g, _, code := askedFor(t)
	_ = g.FinishReset("not-the-code", resetPassword)
	expires, tries := g.reset.expires, g.reset.tries

	again, err := g.StartReset()
	if err != nil || again != "" {
		t.Fatalf("asking while a code is live = %q, %v, want nothing new", again, err)
	}
	if !g.reset.expires.Equal(expires) || g.reset.tries != tries {
		t.Error("asking again changed how long or how often the code can be tried")
	}
	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the first code stopped working: %v", err)
	}
}

// TestANewCodeIsMintedAtMostOnceAMinute: without the gap, somebody who burns
// the five tries could ask for the next five at once, and fill the log with
// codes while doing it.
func TestANewCodeIsMintedAtMostOnceAMinute(t *testing.T) {
	g, _, _ := askedFor(t)
	for range ResetCodeTries {
		_ = g.FinishReset("not-the-code", resetPassword)
	}

	if code, err := g.StartReset(); err != nil || code != "" {
		t.Fatalf("asking right after a code was spent = %q, %v, want nothing new", code, err)
	}
	g.resetMinted = time.Now().Add(-resetCodeGap)
	code, err := g.StartReset()
	if err != nil || !resetCodeShape.MatchString(code) {
		t.Fatalf("asking a minute later = %q, %v, want a new code", code, err)
	}
	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the new code was refused: %v", err)
	}
}

func TestAPasswordThatIsTooShortKeepsTheCodeAndItsTries(t *testing.T) {
	g, _, code := askedFor(t)
	for range ResetCodeTries + 1 {
		if err := g.FinishReset(code, "short"); !errors.Is(err, ErrTooShort) {
			t.Fatalf("a short password = %v, want ErrTooShort", err)
		}
	}

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the code did not survive a password that was too short: %v", err)
	}
}

func TestAPasswordBcryptWouldRefuseKeepsTheCode(t *testing.T) {
	g, _, code := askedFor(t)
	if err := g.FinishReset(code, strings.Repeat("a", 73)); err == nil {
		t.Fatal("a 73 byte password was accepted")
	}

	if err := g.FinishReset(code, resetPassword); err != nil {
		t.Fatalf("the code did not survive a password that was too long: %v", err)
	}
}

func TestTheResetCodeIsAcceptedAsTyped(t *testing.T) {
	g, _, code := askedFor(t)
	typed := " " + strings.ToLower(code[:4]) + " - " + strings.ToLower(code[4:]) + " "

	if err := g.FinishReset(typed, resetPassword); err != nil {
		t.Fatalf("%q was refused for the code %q: %v", typed, code, err)
	}
}

// TestNoCodeIsNoCode: the stand-in a guess is compared with while nothing is
// live must not open anything, typed or empty.
func TestNoCodeIsNoCode(t *testing.T) {
	g, _ := locked(t)
	for _, typed := range []string{"", noResetCode, "0000-0000"} {
		if err := g.FinishReset(typed, resetPassword); !errors.Is(err, ErrCodeRejected) {
			t.Errorf("FinishReset(%q) with no code asked for = %v, want ErrCodeRejected", typed, err)
		}
	}
	if !g.Check("correct-horse") {
		t.Error("a reset without a code changed the password")
	}
}

func TestARestartCancelsAResetUnderWay(t *testing.T) {
	_, dir, code := askedFor(t)
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := again.FinishReset(code, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("a code from before the restart = %v, want ErrCodeRejected", err)
	}
}

func TestACodeThatCouldNotBeShownMakesRoomForTheNext(t *testing.T) {
	g, _, first := askedFor(t)
	g.CancelReset()

	second, err := g.StartReset()
	if err != nil || !resetCodeShape.MatchString(second) {
		t.Fatalf("asking after a cancelled code = %q, %v, want a new code", second, err)
	}
	if first == second {
		t.Fatalf("the cancelled code %q was minted again", first)
	}
	if err := g.FinishReset(first, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the cancelled code = %v, want ErrCodeRejected", err)
	}
}

func TestResetCodesDiffer(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		code, err := newResetCode()
		if err != nil {
			t.Fatal(err)
		}
		if !resetCodeShape.MatchString(code) {
			t.Fatalf("newResetCode() = %q, want eight symbols of the alphabet", code)
		}
		if seen[code] {
			t.Fatalf("newResetCode() repeated %q within 64 codes of forty bits", code)
		}
		seen[code] = true
	}
}
