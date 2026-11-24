package auth

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// The folders in ../access/testdata were written by 2.9.5 with the password
// below, the second factor on and three API tokens, once sealed with fixtureKey
// and once without an app key.
const (
	fixturePassword = "correct horse battery"
	fixtureKey      = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

func readFixture(t *testing.T, sealed bool) *Old {
	t.Helper()
	dir, key := filepath.Join("..", "access", "testdata", "plain"), (*appkey.Key)(nil)
	if sealed {
		dir = filepath.Join("..", "access", "testdata", "sealed")
		var err error
		if key, err = appkey.Parse(fixtureKey); err != nil {
			t.Fatal(err)
		}
	}
	o, err := Read(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// oldCookie signs a session the way 2.9.5 issued them.
func oldCookie(o *Old, expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10) + "." + rand.Text()
	msg := payload
	if o.s.Epoch != 0 {
		msg += "|" + strconv.FormatUint(o.s.Epoch, 10)
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(sign(o.key, msg))
}

func TestTheOldPasswordStillChecks(t *testing.T) {
	for _, sealed := range []bool{true, false} {
		o := readFixture(t, sealed)
		if !o.CheckPassword(o.Hash(), fixturePassword) {
			t.Errorf("sealed %v: the password 2.9.5 set does not check", sealed)
		}
		if o.CheckPassword(o.Hash(), fixturePassword+"!") {
			t.Errorf("sealed %v: a wrong password checks", sealed)
		}
	}
}

func TestAnArgonHashNeedsItsKey(t *testing.T) {
	o := readFixture(t, true)
	o.appKey = nil
	if o.CheckPassword(o.Hash(), fixturePassword) {
		t.Error("a peppered hash checked without the app key")
	}
}

func TestAnOldSessionCountsUntilItRunsOut(t *testing.T) {
	o := readFixture(t, true)
	now := time.Now()
	cookie := oldCookie(o, now.Add(time.Hour))
	exp, key, ok := o.Session(cookie, now)
	if !ok || exp.Unix() != now.Add(time.Hour).Unix() || key == "" {
		t.Fatalf("Session = %v %q %v, want the cookie's expiry", exp, key, ok)
	}
	if _, _, ok := o.Session(cookie, now.Add(2*time.Hour)); ok {
		t.Error("an expired cookie still counts")
	}
	if _, _, ok := o.Session(cookie+"x", now); ok {
		t.Error("a cookie with a changed signature counts")
	}
}

func TestAnOldSessionOfAnotherEpochOrSignedOutDoesNotCount(t *testing.T) {
	o := readFixture(t, false)
	now := time.Now()
	cookie := oldCookie(o, now.Add(time.Hour))
	o.s.Epoch++
	if _, _, ok := o.Session(cookie, now); ok {
		t.Error("a cookie of the epoch before counts")
	}
	o.s.Epoch--
	o.s.Revoked = map[string]int64{revocationKey(cookie): now.Add(time.Hour).Unix()}
	if _, _, ok := o.Session(cookie, now); ok {
		t.Error("a signed-out cookie counts")
	}
}

func TestTheSecondFactorAndARecoveryCodeComeAcross(t *testing.T) {
	o := readFixture(t, false)
	totp, err := o.TwoFactor()
	if err != nil || len(totp) != 20 {
		t.Fatalf("TwoFactor = %d bytes, %v, want the 20-byte secret", len(totp), err)
	}
	hashes := o.Recovery()
	if len(hashes) != 8 {
		t.Fatalf("%d recovery codes, want 8", len(hashes))
	}
	if !o.RecoveryMatches(hashes[0], "mgx3a-2xpnu") || !o.RecoveryMatches(hashes[0], "MGX3A2XPNU") {
		t.Error("the first recovery code does not match its hash")
	}
	if o.RecoveryMatches(hashes[1], "mgx3a-2xpnu") {
		t.Error("a recovery code matches another code's hash")
	}
}

func TestThePasskeyHandleIsTheOneThePasskeysWereMadeWith(t *testing.T) {
	o := readFixture(t, true)
	if len(o.PasskeyHandle()) != 32 || string(o.PasskeyHandle()) != string(readFixture(t, true).PasskeyHandle()) {
		t.Error("the handle is not a stable 32-byte value")
	}
}

func TestRemoveTakesAuthJSONAway(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "access", "testdata", "plain", FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := Read(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Remove(); err != nil {
		t.Fatal(err)
	}
	if again, err := Read(dir, nil); err != nil || again.Hash() != "" {
		t.Errorf("after Remove the folder reads as %q, %v, want no password", again.Hash(), err)
	}
	if err := o.Remove(); err != nil {
		t.Errorf("a second Remove failed: %v", err)
	}
}
