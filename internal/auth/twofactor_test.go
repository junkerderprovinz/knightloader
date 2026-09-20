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

// locked returns a Guard with a password set, the only state a second factor
// can exist in.
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

// TestAHalfFinishedEnrolmentIsNotArmed checks that showing a QR code changes
// neither what TwoFactorEnabled reports nor the file.
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
		t.Errorf("got %d recovery codes, want %d", len(codes), secret.RecoveryCodeCount)
	}
	if !g.TwoFactorEnabled() {
		t.Fatal("the factor is not armed after a confirmed code")
	}
	if g.RecoveryLeft() != secret.RecoveryCodeCount {
		t.Errorf("RecoveryLeft = %d, want %d", g.RecoveryLeft(), secret.RecoveryCodeCount)
	}

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
		t.Errorf("RecoveryLeft = %d after turning the factor off, want 0", g.RecoveryLeft())
	}
}

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
	if err := again.DisableTwoFactor(codes[4]); err != nil {
		t.Errorf("DisableTwoFactor with a recovery code: %v", err)
	}
}

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

func TestCheckSecondAnswersNoWhileTheFactorIsOff(t *testing.T) {
	g, _ := locked(t)
	for _, s := range []string{"", "000000", "abcde-fghij"} {
		if g.CheckSecond(s) {
			t.Errorf("CheckSecond(%q) said yes while the factor is off", s)
		}
	}
}
