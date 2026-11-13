package sitelogin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

func TestTheStoreKeepsOneSealedSessionPerSite(t *testing.T) {
	dir := t.TempDir()
	a, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(a)
	const secret = "PATREON-SESSION-never-in-the-clear"
	if err := s.Set("patreon", jar(jarLine(".patreon.com", "session_id", secret, now))); err != nil {
		t.Fatal(err)
	}
	if got := s.Session("patreon"); !strings.Contains(got, secret) {
		t.Errorf("Session(patreon) = %q, want the stored jar", got)
	}
	if got := s.Session("youtube"); got != "" {
		t.Errorf("Session(youtube) = %q, want nothing", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("accounts.json holds the session in the clear:\n%s", raw)
	}
	if err := s.Set("patreon", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.Session("patreon"); got != "" {
		t.Errorf("Session after clearing = %q, want nothing", got)
	}
}

func TestTheStoreRefusesASiteItDoesNotKnow(t *testing.T) {
	a, err := accounts.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := NewStore(a).Set("example", "jar"); err == nil {
		t.Error("Set for an unknown site succeeded")
	}
}
