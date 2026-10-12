package accounts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

func testKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func copyDir(t *testing.T, from string, names ...string) string {
	t.Helper()
	to := t.TempDir()
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(from, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, n), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return to
}

func TestASealedKeyringOpensNothingWithoutTheAppKey(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("realdebrid", "secret-api-key"); err != nil {
		t.Fatal(err)
	}
	ring, err := os.ReadFile(filepath.Join(dir, KeyringName))
	if err != nil {
		t.Fatal(err)
	}
	if len(ring) == 32 || !appkey.IsSealed(string(ring)) {
		t.Fatal(".keyring holds the credential key in the clear")
	}

	copied := copyDir(t, dir, KeyringName, "accounts.json")
	if _, err := Open(copied); !errors.Is(err, appkey.ErrMissing) {
		t.Fatalf("opening a sealed copy without the key = %v, want ErrMissing", err)
	}
	if _, err := OpenWithKey(copied, testKey(t)); !errors.Is(err, appkey.ErrWrongKey) {
		t.Fatalf("opening a sealed copy with another key = %v, want ErrWrongKey", err)
	}
	if after, _ := os.ReadFile(filepath.Join(copied, KeyringName)); !bytes.Equal(after, ring) {
		t.Fatal("a refused open replaced the sealed key")
	}
	if dirs, _ := filepath.Glob(filepath.Join(copied, KeyringName+".damaged-*")); len(dirs) > 0 {
		t.Fatal("a sealed key without its app key was set aside as damaged")
	}

	back, err := OpenWithKey(copied, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := back.Get("realdebrid"); err != nil || got != "secret-api-key" {
		t.Fatalf("with the app key the copy reads %q, %v", got, err)
	}
}

func TestAPlainKeyringIsSealedInPlace(t *testing.T) {
	dir := t.TempDir()
	plain, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Set("realdebrid", "secret-api-key"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, KeyringName)); len(b) != 32 {
		t.Fatalf("without an app key .keyring is %d bytes, want the bare 32", len(b))
	}

	key := testKey(t)
	s, err := OpenWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, KeyringName)); !appkey.IsSealed(string(b)) {
		t.Fatal("the first open with an app key left .keyring in the clear")
	}
	if got, err := s.Get("realdebrid"); err != nil || got != "secret-api-key" {
		t.Fatalf("after sealing the credential reads %q, %v", got, err)
	}
}
