package auth

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionUntil is what Issue would hand out for a session ending at exp, so a
// test can have several distinct sessions without waiting a second for each.
func sessionUntil(g *Guard, exp int64) string {
	e := strconv.FormatInt(exp, 10)
	return e + "." + base64.RawURLEncoding.EncodeToString(g.sign(sessionMessage(e, g.epoch)))
}

func TestChangingThePasswordEndsEarlierSessions(t *testing.T) {
	g, _ := locked(t)
	before := g.Issue()
	if err := g.SetPassword("correct-horse", "battery-staple"); err != nil {
		t.Fatal(err)
	}
	if g.Valid(before) {
		t.Error("a session issued before the password change is still valid")
	}
	if after := g.Issue(); !g.Valid(after) {
		t.Error("a session issued after the password change is not valid")
	}
}

func TestSettingThePasswordAgainDoesNotReviveOldSessions(t *testing.T) {
	g, _ := locked(t)
	before := g.Issue()
	if err := g.SetPassword("correct-horse", ""); err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	if g.Valid(before) {
		t.Error("a session from before the password was removed works again once it is set back")
	}
}

func TestASignedOutSessionStaysSignedOutAfterARestart(t *testing.T) {
	g, dir := locked(t)
	exp := time.Now().Add(time.Hour).Unix()
	gone, kept := sessionUntil(g, exp), sessionUntil(g, exp+1)
	if err := g.Revoke(gone); err != nil {
		t.Fatal(err)
	}
	if g.Valid(gone) {
		t.Error("the signed-out session is still valid")
	}
	if !g.Valid(kept) {
		t.Error("signing out one session ended another")
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Valid(gone) {
		t.Error("the signed-out session is valid again after a restart")
	}
	if !reopened.Valid(kept) {
		t.Error("the other session did not survive the restart")
	}
}

func TestASignedOutSessionStaysOutWhateverItsLastCharacter(t *testing.T) {
	g, _ := locked(t)
	tok := g.Issue()
	if err := g.Revoke(tok); err != nil {
		t.Fatal(err)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for _, c := range alphabet {
		if variant := tok[:len(tok)-1] + string(c); g.Valid(variant) {
			t.Errorf("the signed-out session is accepted again as %q", variant)
		}
	}
}

func TestSessionsIssuedInTheSameSecondAreSignedOutApart(t *testing.T) {
	g, _ := locked(t)
	a, b := g.Issue(), g.Issue()
	if err := g.Revoke(a); err != nil {
		t.Fatal(err)
	}
	if !g.Valid(b) {
		t.Error("signing out one session ended another issued in the same second")
	}
}

// TestParallelSignOutsWriteTheListSafely: each sign-out writes the list while
// the others add to it, which the race detector, or the runtime itself on a
// map, catches when the file is written from the live list.
func TestParallelSignOutsWriteTheListSafely(t *testing.T) {
	g, dir := locked(t)
	exp := time.Now().Add(time.Hour).Unix()
	sessions := make([]string, 64)
	for i := range sessions {
		sessions[i] = sessionUntil(g, exp+int64(i))
	}
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Go(func() {
			if err := g.Revoke(s); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if g.Valid(s) {
			t.Fatal("a session signed out in parallel with others is still valid")
		}
		if reopened.Valid(s) {
			t.Fatal("a session signed out in parallel with others is valid again after a restart")
		}
	}
}

func TestRevokeAllEndsEverySession(t *testing.T) {
	g, dir := locked(t)
	s := g.Issue()
	if err := g.RevokeAll(nil); err != nil {
		t.Fatal(err)
	}
	if g.Valid(s) {
		t.Error("a session survived RevokeAll")
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Valid(s) {
		t.Error("a session from before RevokeAll is valid again after a restart")
	}
}

func TestAFullRevocationListSignsEverybodyOut(t *testing.T) {
	g, _ := locked(t)
	other := g.Issue()
	exp := time.Now().Add(time.Hour).Unix()
	for i := range maxRevoked + 1 {
		if err := g.Revoke(sessionUntil(g, exp+int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if g.Valid(other) {
		t.Error("the list grew past its bound instead of ending every session")
	}
	if n := len(g.revoked); n > maxRevoked {
		t.Errorf("revocation list holds %d entries, bound is %d", n, maxRevoked)
	}
}

func TestSessionsFromBeforeTheEpochStayValid(t *testing.T) {
	g, _ := locked(t)
	g.epoch = 0
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	legacy := exp + "." + base64.RawURLEncoding.EncodeToString(g.sign(exp))
	if !g.Valid(legacy) {
		t.Error("a cookie signed over the expiry alone is refused while the epoch is still zero")
	}
}

func TestAnyPasswordUpToBcryptsLimitIsTaken(t *testing.T) {
	g, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current := ""
	for _, password := range []string{"a", "1234", "äöüßäöüéäöü", "日本語のパスワードです", strings.Repeat("a", 72)} {
		if err := g.SetPassword(current, password); err != nil {
			t.Errorf("%q (%d characters, %d bytes) was refused: %v", password, len([]rune(password)), len(password), err)
		}
		current = password
	}
	for _, password := range []string{strings.Repeat("a", 73), strings.Repeat("ä", 37)} {
		if err := g.SetPassword(current, password); !errors.Is(err, ErrTooLong) {
			t.Errorf("%d bytes = %v, want ErrTooLong", len(password), err)
		}
	}
}

// TestAShortPasswordSignsIn: an owner who picks a short password gets the
// lock they asked for.
func TestAShortPasswordSignsIn(t *testing.T) {
	g, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", "kl"); err != nil {
		t.Fatal(err)
	}
	if !g.Enabled() || !g.Check("kl") {
		t.Fatal("a two character password does not lock and sign in")
	}
	if err := g.SetPassword("kl", "ninechars"); err != nil {
		t.Errorf("changing it to nine characters: %v", err)
	}
}
