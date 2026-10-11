package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/secret"
)

func newKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// armed is a guard under key with a password and a confirmed second factor.
func armed(t *testing.T, dir string, key *appkey.Key) (*Guard, string) {
	t.Helper()
	g, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	totp, _, err := g.BeginTwoFactor("KnightLoader", "test")
	if err != nil {
		t.Fatal(err)
	}
	code, err := secret.TOTPCode(totp, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmTwoFactor(code); err != nil {
		t.Fatal(err)
	}
	return g, totp
}

func copyAuthFile(t *testing.T, from string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(from, FileName))
	if err != nil {
		t.Fatal(err)
	}
	to := t.TempDir()
	if err := os.WriteFile(filepath.Join(to, FileName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return to
}

func TestACopiedDataFolderWithoutTheKeyOpensNothing(t *testing.T) {
	dir := t.TempDir()
	key := newKey(t)
	g, totp := armed(t, dir, key)

	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var s stored
	_ = json.Unmarshal(raw, &s)
	if s.Key != "" || s.Hash != "" || s.TOTP != "" || len(s.Recovery) > 0 {
		t.Fatal("auth.json hands out the session key, the hash, the TOTP secret or the recovery codes without the app key")
	}
	if bytes.Contains(raw, []byte(totp)) || bytes.Contains(raw, []byte(argonPrefix)) {
		t.Fatal("auth.json holds the TOTP secret or the password hash in the clear")
	}

	copied := copyAuthFile(t, dir)
	if _, err := Open(copied); !errors.Is(err, appkey.ErrMissing) {
		t.Fatalf("opening a sealed copy without the key = %v, want ErrMissing", err)
	}
	if _, err := OpenWithKey(copied, newKey(t)); !errors.Is(err, appkey.ErrWrongKey) {
		t.Fatalf("opening a sealed copy with another key = %v, want ErrWrongKey", err)
	}
	after, err := os.ReadFile(filepath.Join(copied, FileName))
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("a refused open replaced the sealed file")
	}

	// With the key the copy is the same lock: its sessions are the original's.
	same, err := OpenWithKey(copied, key)
	if err != nil {
		t.Fatal(err)
	}
	if !same.Valid(g.Issue()) || !same.Check("correct-horse") || !same.TwoFactorEnabled() {
		t.Fatal("the sealed file does not open to the same lock with its own key")
	}
}

func TestABcryptHashBecomesArgon2idAtTheNextSignIn(t *testing.T) {
	dir := t.TempDir()
	plain, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session := plain.Issue()
	if !strings.HasPrefix(string(plain.hash), "$2") {
		t.Fatalf("without a key the hash is %q, want bcrypt as before", plain.hash)
	}

	key := newKey(t)
	g, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, FileName)); !appkey.IsSealed(string(raw)) {
		t.Fatal("the first start with a key left auth.json in the clear")
	}
	if !g.Valid(session) {
		t.Fatal("sealing auth.json ended the sessions issued before it")
	}
	if !strings.HasPrefix(string(g.hash), "$2") {
		t.Fatal("the hash changed before anybody signed in")
	}
	if g.Check("wrong-horse") {
		t.Fatal("a wrong password verified against the bcrypt hash")
	}
	if !strings.HasPrefix(string(g.hash), "$2") {
		t.Fatal("a failed sign-in replaced the hash")
	}
	if !g.Check("correct-horse") {
		t.Fatal("the bcrypt hash from before the key no longer verifies")
	}
	if !strings.HasPrefix(string(g.hash), argonPrefix) {
		t.Fatalf("after a sign-in the hash is %q, want Argon2id", g.hash)
	}

	again, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(again.hash), argonPrefix) {
		t.Fatal("the Argon2id hash was not written")
	}
	if !again.Check("correct-horse") || again.Check("wrong-horse") {
		t.Fatal("the Argon2id hash does not tell the right password from a wrong one")
	}
}

func TestAnArgon2idHashNeedsItsKey(t *testing.T) {
	key := newKey(t)
	h, err := hashPassword(key, "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(key, "correct-horse", h) {
		t.Fatal("the hash does not verify with its own key")
	}
	if verifyPassword(nil, "correct-horse", h) || verifyPassword(newKey(t), "correct-horse", h) {
		t.Fatal("the hash verifies without the key it was peppered with")
	}
	if verifyPassword(key, "correct-horse", []byte(argonPrefix+"19456$2$1$$")) {
		t.Fatal("a damaged hash verified")
	}
}

func TestWithoutAKeyAuthJSONStaysAsItWas(t *testing.T) {
	dir := t.TempDir()
	armed(t, dir, nil)
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("auth.json without a key is not plain JSON: %v", err)
	}
	if !strings.HasPrefix(s.Hash, "$2") || s.Key == "" || s.TOTP == "" {
		t.Fatalf("auth.json without a key changed shape: %s", raw)
	}
	if !NeedsSealing(dir) {
		t.Fatal("a plain auth.json does not report that a key would seal it")
	}
}
