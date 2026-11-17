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

func TestTheMarkNotToUseTheBrowserIsKeptPerSite(t *testing.T) {
	s := openStore(t)
	if s.Disconnected("patreon") {
		t.Fatal("a site starts out disconnected")
	}
	if err := s.SetDisconnected("patreon", true); err != nil {
		t.Fatal(err)
	}
	if !s.Disconnected("patreon") || s.Disconnected("youtube") {
		t.Errorf("patreon %v, youtube %v; want only patreon disconnected", s.Disconnected("patreon"), s.Disconnected("youtube"))
	}
	if err := s.SetDisconnected("patreon", false); err != nil {
		t.Fatal(err)
	}
	if s.Disconnected("patreon") {
		t.Error("the mark did not clear")
	}
	if err := s.SetDisconnected("example", true); err == nil {
		t.Error("a site that is not on offer was marked")
	}
}

func TestASiteIsAddedUntilItIsRemoved(t *testing.T) {
	s := openStore(t)
	if s.Added("instagram") {
		t.Fatal("a site starts out added")
	}
	if err := s.SetAdded("instagram", true); err != nil {
		t.Fatal(err)
	}
	if !s.Added("instagram") || s.Added("patreon") {
		t.Errorf("instagram %v, patreon %v; want only instagram added", s.Added("instagram"), s.Added("patreon"))
	}
	if err := s.SetAdded("instagram", false); err != nil {
		t.Fatal(err)
	}
	if s.Added("instagram") {
		t.Error("removing kept the site")
	}
	if err := s.SetAdded("example", true); err == nil {
		t.Error("a site that is not on offer was added")
	}
}

func TestTheRowsAreTakenOverOnce(t *testing.T) {
	s := openStore(t)
	if s.RowsTakenOver() {
		t.Fatal("a new store says the rows were taken over")
	}
	if err := s.SetRowsTakenOver(); err != nil {
		t.Fatal(err)
	}
	if !s.RowsTakenOver() {
		t.Error("the mark did not stick")
	}
}

func TestEachBrowserSaysForItselfWhetherItReadsASite(t *testing.T) {
	s := openStore(t)
	if got := s.Readers("patreon"); len(got) != 0 {
		t.Fatalf("a site starts out read by %v", got)
	}
	for _, app := range []string{"browser-a", "browser-b"} {
		if err := s.SetReader("patreon", app, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetReader("patreon", "browser-a", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Readers("patreon"), ","); got != "browser-b" {
		t.Errorf("patreon is read by %q, want browser-b alone", got)
	}
	if got := s.Readers("youtube"); len(got) != 0 {
		t.Errorf("youtube is read by %v, want nobody", got)
	}
	if err := s.SetReader("example", "browser-a", true); err == nil {
		t.Error("a site that is not on offer got a reader")
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	a, err := accounts.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(a)
}
