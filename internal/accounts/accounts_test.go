package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "torbox-secret-key-123"
	if err := s.Set("torbox", secret); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("torbox"); got != secret {
		t.Fatalf("Get = %q, want %q", got, secret)
	}

	// Reopen: the secret persists and still decrypts under the stored keyring.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Get("torbox"); got != secret {
		t.Fatalf("after reopen Get = %q, want %q", got, secret)
	}

	// The on-disk file must hold ciphertext only, never the plaintext.
	b, _ := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if strings.Contains(string(b), secret) {
		t.Fatal("plaintext secret leaked into accounts.json")
	}

	// Clearing removes it.
	if err := s2.Set("torbox", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Get("torbox"); got != "" {
		t.Fatalf("after clear Get = %q, want empty", got)
	}
}
