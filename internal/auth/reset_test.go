package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

const resetPassword = "a-new-long-password"

var resetCodeShape = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{12}$`)

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
		t.Fatalf("StartReset minted %q, want twelve symbols of the alphabet", code)
	}
	return g, dir, code
}

func finish(g *Guard, code, next string) error {
	_, err := g.FinishReset(code, next, nil)
	return err
}

func TestThereIsNothingToResetWithoutAPassword(t *testing.T) {
	g, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if code, err := g.StartReset(); !errors.Is(err, ErrNothingToReset) || code != "" {
		t.Fatalf("StartReset with no password = %q, %v, want ErrNothingToReset", code, err)
	}
	if err := finish(g, "ABCDEFGHJKLM", resetPassword); !errors.Is(err, ErrNothingToReset) {
		t.Fatalf("FinishReset with no password = %v, want ErrNothingToReset", err)
	}
	if g.Enabled() {
		t.Error("a reset on an open instance set a password")
	}
}

func TestTheResetCodeSetsThePassword(t *testing.T) {
	g, dir, code := askedFor(t)

	c, err := g.FinishReset(code, resetPassword, nil)
	if err != nil {
		t.Fatalf("the right code was refused: %v", err)
	}
	if !g.Check(resetPassword) {
		t.Error("the new password does not sign in")
	}
	if g.Check("correct-horse") {
		t.Error("the old password still signs in")
	}
	if tok, ok := g.IssueFor(c); !ok || !g.Valid(tok) {
		t.Error("the caller who reset the password gets no session")
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

	if err := finish(g, code, resetPassword); err != nil {
		t.Fatal(err)
	}
	if g.Valid(before) {
		t.Error("a session from before the reset is still valid")
	}
}

// TestAnOldCopyOfAuthJSONSignsNothingAfterAReset: whoever copied the file
// before the reset holds the key that signed sessions, and can write any
// epoch they like beside it.
func TestAnOldCopyOfAuthJSONSignsNothingAfterAReset(t *testing.T) {
	g, dir, code := askedFor(t)
	old, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatal(err)
	}

	var copied stored
	if err := json.Unmarshal(old, &copied); err != nil {
		t.Fatal(err)
	}
	copied.Epoch = g.Epoch()
	forged := t.TempDir()
	if err := writeState(filepath.Join(forged, "auth.json"), copied, nil); err != nil {
		t.Fatal(err)
	}
	thief, err := Open(forged)
	if err != nil {
		t.Fatal(err)
	}
	if g.Valid(thief.Issue()) {
		t.Error("a session signed with the key from before the reset is valid")
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

	if err := finish(g, code, resetPassword); err != nil {
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
	wrong := strings.Repeat("2", ResetCodeLen)
	if wrong == code {
		wrong = strings.Repeat("3", ResetCodeLen)
	}

	if err := finish(g, wrong, resetPassword); !errors.Is(err, ErrCodeRejected) {
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
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatal(err)
	}

	if err := finish(g, code, "another-long-password"); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the second use of a code = %v, want ErrCodeRejected", err)
	}
	if !g.Check(resetPassword) {
		t.Error("the second use of the code changed the password")
	}
}

func TestAResetCodeExpires(t *testing.T) {
	g, _, code := askedFor(t)
	g.reset.expires = time.Now().Add(-time.Second)

	if err := finish(g, code, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("an expired code = %v, want ErrCodeRejected", err)
	}
	if !g.Check("correct-horse") {
		t.Error("an expired code changed the password")
	}
}

// TestWrongGuessesLeaveTheCodeAlone: if guesses could spend the code, anybody
// who reaches the page could keep the owner from ever using one.
func TestWrongGuessesLeaveTheCodeAlone(t *testing.T) {
	g, _, code := askedFor(t)
	for range 100 {
		if err := finish(g, "not-the-code", resetPassword); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("a wrong guess = %v, want ErrCodeRejected", err)
		}
	}

	if err := finish(g, code, resetPassword); err != nil {
		t.Fatalf("the code after a hundred wrong guesses was refused: %v", err)
	}
}

// TestAskingAgainNeitherReplacesNorExtendsTheCode: a second request must not
// take the code away from whoever is typing it, and must not give a guesser
// more time with it.
func TestAskingAgainNeitherReplacesNorExtendsTheCode(t *testing.T) {
	g, _, code := askedFor(t)
	expires := g.reset.expires

	again, err := g.StartReset()
	if err != nil || again != "" {
		t.Fatalf("asking while a code is live = %q, %v, want nothing new", again, err)
	}
	if !g.reset.expires.Equal(expires) {
		t.Error("asking again changed how long the code can be used")
	}
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatalf("the first code stopped working: %v", err)
	}
}

// TestANewCodeIsMintedAtMostOnceAMinute: asking must not fill the output.
func TestANewCodeIsMintedAtMostOnceAMinute(t *testing.T) {
	g, _, _ := askedFor(t)
	g.reset.expires = time.Now().Add(-time.Second)

	if code, err := g.StartReset(); err != nil || code != "" {
		t.Fatalf("asking right after a code ran out = %q, %v, want nothing new", code, err)
	}
	g.resetMinted = time.Now().Add(-resetCodeGap)
	code, err := g.StartReset()
	if err != nil || !resetCodeShape.MatchString(code) {
		t.Fatalf("asking a minute later = %q, %v, want a new code", code, err)
	}
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatalf("the new code was refused: %v", err)
	}
}

func TestAPasswordThatIsTooShortKeepsTheCode(t *testing.T) {
	g, _, code := askedFor(t)
	if err := finish(g, code, "short"); !errors.Is(err, ErrTooShort) {
		t.Fatalf("a short password = %v, want ErrTooShort", err)
	}

	if err := finish(g, code, resetPassword); err != nil {
		t.Fatalf("the code did not survive a password that was too short: %v", err)
	}
}

func TestAPasswordBcryptWouldRefuseKeepsTheCode(t *testing.T) {
	g, _, code := askedFor(t)
	if err := finish(g, code, strings.Repeat("a", 73)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("a 73 byte password = %v, want ErrTooLong", err)
	}

	if err := finish(g, code, resetPassword); err != nil {
		t.Fatalf("the code did not survive a password that was too long: %v", err)
	}
}

func TestAResetCountsCharactersNotBytes(t *testing.T) {
	g, _, code := askedFor(t)
	if err := finish(g, code, "äöüßäöüéäöü"); !errors.Is(err, ErrTooShort) {
		t.Fatalf("eleven characters in twenty-two bytes = %v, want ErrTooShort", err)
	}
	if err := finish(g, code, "äöüßäöüéäöüß"); err != nil {
		t.Fatalf("twelve characters were refused: %v", err)
	}
}

func TestTheResetCodeIsAcceptedAsTyped(t *testing.T) {
	g, _, code := askedFor(t)
	typed := " " + strings.ToLower(code[:4]) + " - " + strings.ToLower(code[4:8]) + "-" + code[8:] + " "

	if err := finish(g, typed, resetPassword); err != nil {
		t.Fatalf("%q was refused for the code %q: %v", typed, code, err)
	}
}

// TestNoCodeIsNoCode: the stand-in a guess is compared with while nothing is
// live must not open anything, typed or empty.
func TestNoCodeIsNoCode(t *testing.T) {
	g, _ := locked(t)
	for _, typed := range []string{"", noResetCode, "0000-0000-0000"} {
		if err := finish(g, typed, resetPassword); !errors.Is(err, ErrCodeRejected) {
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

	if err := finish(again, code, resetPassword); !errors.Is(err, ErrCodeRejected) {
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
	if err := finish(g, first, resetPassword); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the cancelled code = %v, want ErrCodeRejected", err)
	}
}

// TestAResetThatCannotBeWrittenChangesNothing: applied in memory alone, the
// reset would end the sessions at once and give the old password back at the next
// start.
func TestAResetThatCannotBeWrittenChangesNothing(t *testing.T) {
	g, _, code := askedFor(t)
	session := g.Issue()
	path := g.path
	g.path = t.TempDir()

	if err := finish(g, code, resetPassword); err == nil {
		t.Fatal("a reset that could not be written succeeded")
	}
	if !g.Check("correct-horse") || g.Check(resetPassword) {
		t.Error("the password changed in memory without reaching the file")
	}
	if !g.Valid(session) {
		t.Error("a reset that could not be written ended the sessions")
	}
	g.path = path
	if err := finish(g, code, resetPassword); err != nil {
		t.Errorf("the code did not survive a reset that could not be written: %v", err)
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
			t.Fatalf("newResetCode() = %q, want twelve symbols of the alphabet", code)
		}
		if seen[code] {
			t.Fatalf("newResetCode() repeated %q within 64 codes of sixty bits", code)
		}
		seen[code] = true
	}
}

// TestASessionIsIssuedOnlyForThePasswordItWasCheckedAgainst: a sign-in that
// checked the old password and issues its session after a change or a reset
// would let the old password in once more.
func TestASessionIsIssuedOnlyForThePasswordItWasCheckedAgainst(t *testing.T) {
	g, _, code := askedFor(t)
	checked, ok := g.CheckPassword("correct-horse")
	if !ok {
		t.Fatal("the password was refused")
	}
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatal(err)
	}
	if tok, ok := g.IssueFor(checked); ok || g.Valid(tok) {
		t.Error("a check made before the reset was issued a session after it")
	}

	checked, _ = g.CheckPassword(resetPassword)
	if err := g.SetPassword(resetPassword, "a-third-password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.IssueFor(checked); ok {
		t.Error("a check made before a password change was issued a session after it")
	}
}

// TestAChangeCheckedAgainstAResetPasswordIsRefused: somebody changing the
// password with the old one while the owner resets it must not have the last
// word.
func TestAChangeCheckedAgainstAResetPasswordIsRefused(t *testing.T) {
	g, _, code := askedFor(t)
	checked, ok := g.CheckPassword("correct-horse")
	if !ok {
		t.Fatal("the password was refused")
	}
	if err := finish(g, code, resetPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := g.changeFrom(checked, "the-intruders-password", nil); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("a change checked before the reset = %v, want ErrWrongPassword", err)
	}
	if !g.Check(resetPassword) {
		t.Error("the change checked before the reset replaced the reset password")
	}
}

// TestSignOutsDuringAResetLeaveTheNewPasswordOnDisk: a sign-out writes
// auth.json too, and one that read the state before the reset must not write
// it back over the reset, or cut the file short so the next start reads no
// password.
func TestSignOutsDuringAResetLeaveTheNewPasswordOnDisk(t *testing.T) {
	for _, withKey := range []bool{false, true} {
		name := "without an app key"
		var key *appkey.Key
		if withKey {
			name, key = "with an app key", newKey(t)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			template := lockedFile(t, key)
			var oldBack, refused, open atomic.Int32
			rounds := make(chan int)
			var workers sync.WaitGroup
			for range 8 {
				workers.Go(func() {
					for range rounds {
						dir := t.TempDir()
						if err := os.WriteFile(filepath.Join(dir, FileName), template, 0o600); err != nil {
							t.Error(err)
							return
						}
						resetWhileSigningOut(t, dir, key)
						g, err := OpenWithKey(dir, key)
						switch {
						case err != nil:
							refused.Add(1)
						case !g.Enabled():
							open.Add(1)
						case g.Check("correct-horse") || !g.Check(resetPassword):
							oldBack.Add(1)
						}
					}
				})
			}
			for i := range 300 {
				rounds <- i
			}
			close(rounds)
			workers.Wait()
			if oldBack.Load()+refused.Load()+open.Load() > 0 {
				t.Errorf("of 300 resets, %d brought the old password back, %d left a file that refuses to start, %d left no password",
					oldBack.Load(), refused.Load(), open.Load())
			}
		})
	}
}

// lockedFile is auth.json with the password correct-horse, sealed under key
// when there is one.
func lockedFile(t *testing.T, key *appkey.Key) []byte {
	t.Helper()
	dir := t.TempDir()
	g, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// resetWhileSigningOut resets the password in dir while four browsers sign
// in and out again as fast as they can.
func resetWhileSigningOut(t *testing.T, dir string, key *appkey.Key) {
	g, err := OpenWithKey(dir, key)
	if err != nil {
		t.Error(err)
		return
	}
	code, err := g.StartReset()
	if err != nil {
		t.Error(err)
		return
	}
	done := make(chan struct{})
	var browsers sync.WaitGroup
	for range 4 {
		browsers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = g.Revoke(g.Issue())
			}
		})
	}
	if _, err := g.FinishReset(code, resetPassword, nil); err != nil {
		t.Error(err)
	}
	close(done)
	browsers.Wait()
}

// TestNothingIsMintedWhileAResetEndsTheOtherCredentials: a token or passkey
// created by a request that started before the reset must be either removed
// by it or refused after it, never created in between.
func TestNothingIsMintedWhileAResetEndsTheOtherCredentials(t *testing.T) {
	g, _, code := askedFor(t)
	var ended, minted atomic.Bool
	result := make(chan error, 1)

	_, err := g.FinishReset(code, resetPassword, func() {
		go func() {
			result <- g.Mint(func() bool { return !ended.Load() }, func() error {
				minted.Store(true)
				return nil
			})
		}()
		time.Sleep(50 * time.Millisecond)
		ended.Store(true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrStale) || minted.Load() {
		t.Errorf("a mint started while the reset ended the credentials = %v, minted %v; want ErrStale", err, minted.Load())
	}
}
