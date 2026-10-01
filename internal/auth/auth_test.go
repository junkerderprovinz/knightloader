package auth

import (
	"encoding/base64"
	"strconv"
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

func TestRevokeAllEndsEverySession(t *testing.T) {
	g, dir := locked(t)
	s := g.Issue()
	if err := g.RevokeAll(); err != nil {
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
