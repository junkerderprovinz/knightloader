package apitoken

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateReturnsTheSecretExactlyOnce(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := s.Create("my phone")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("Create returned no secret")
	}
	if !strings.HasPrefix(secret, "kl_") {
		t.Errorf("secret = %q, want the kl_ prefix", secret)
	}
	if tok.ID == "" || tok.Name != "my phone" {
		t.Errorf("token = %+v", tok)
	}
	if tok.LastUsed != nil {
		t.Error("LastUsed set on a token that has never authenticated anything")
	}

	for _, got := range s.List() {
		if got.ID != tok.ID {
			continue
		}
		return
	}
	t.Fatal("the new token is not in List")
}

func TestCheckAcceptsExactlyTheIssuedSecret(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := s.Create("script")
	if err != nil {
		t.Fatal(err)
	}

	got, ok := s.Check(secret)
	if !ok || got.ID != tok.ID {
		t.Fatalf("Check(secret) = %+v, %v; want the issued token, true", got, ok)
	}
	if got.LastUsed == nil {
		t.Error("Check did not stamp LastUsed on first use")
	}

	for _, wrong := range []string{"", "kl_not-it", secret + "x", secret[:len(secret)-1]} {
		if _, ok := s.Check(wrong); ok {
			t.Errorf("Check(%q) = true, want a near-miss refused", wrong)
		}
	}
}

func TestRevokeStopsAuthenticatingWithoutTouchingOtherTokens(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lost, lostSecret, err := s.Create("lost phone")
	if err != nil {
		t.Fatal(err)
	}
	kept, keptSecret, err := s.Create("desktop script")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Revoke(lost.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Check(lostSecret); ok {
		t.Error("the revoked token still authenticates")
	}
	if got, ok := s.Check(keptSecret); !ok || got.ID != kept.ID {
		t.Errorf("revoking one token broke another: Check(kept) = %+v, %v", got, ok)
	}
	for _, tok := range s.List() {
		if tok.ID == lost.ID {
			t.Error("the revoked token is still in List")
		}
	}
}

func TestRevokeUnknownIDIsAnError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke("does-not-exist"); err != ErrNotFound {
		t.Errorf("Revoke(unknown) = %v, want ErrNotFound", err)
	}
}

func TestRevokeAllClearsEveryTokenAndSurvivesAReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, secretA, err := s.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	_, secretB, err := s.Create("laptop")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Errorf("List() after RevokeAll = %d entries, want 0", len(s.List()))
	}
	for _, secret := range []string{secretA, secretB} {
		if _, ok := s.Check(secret); ok {
			t.Errorf("Check(%q) still authenticates after RevokeAll", secret)
		}
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 0 {
		t.Errorf("after reopening, List() = %d entries, want 0", len(s2.List()))
	}

	if _, _, err := s.Create("new phone"); err != nil {
		t.Errorf("Create after RevokeAll: %v", err)
	}
}

func TestRevokeAllOnAnEmptyStoreIsANoOp(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAll(); err != nil {
		t.Errorf("RevokeAll on an empty store = %v, want nil", err)
	}
}

func TestEmptyNameRefused(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("   "); err != ErrEmptyName {
		t.Errorf("Create(whitespace) = %v, want ErrEmptyName", err)
	}
}

func TestTokensSurviveAReload(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := s1.Create("phone")
	if err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Check(secret)
	if !ok || got.ID != tok.ID {
		t.Fatalf("after reopening the store, Check(secret) = %+v, %v", got, ok)
	}
}

func TestMaxTokensRefusesRatherThanGrowingForever(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxTokens; i++ {
		if _, _, err := s.Create("device"); err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
	}
	if _, _, err := s.Create("one too many"); err != ErrTooMany {
		t.Errorf("Create past the cap = %v, want ErrTooMany", err)
	}
}

// TestLastUsedFlushIsThrottledButAlwaysCorrectInMemory checks that the disk
// copy may lag by staleAfter while the in-memory value is always current.
func TestLastUsedFlushIsThrottledButAlwaysCorrectInMemory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := s.Create("busy client")
	if err != nil {
		t.Fatal(err)
	}

	first, _ := s.Check(secret)
	second, _ := s.Check(secret)
	if !second.LastUsed.After(*first.LastUsed) && !second.LastUsed.Equal(*first.LastUsed) {
		t.Errorf("in-memory LastUsed went backwards: %v then %v", first.LastUsed, second.LastUsed)
	}

	// The first Check always flushes, since LastUsed was nil before it.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range s2.List() {
		if got.ID == tok.ID && got.LastUsed == nil {
			t.Error("the first-ever use of a token was not flushed to disk")
		}
	}
}

func TestCheckOnAFreshStoreDoesNotPanic(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Check("kl_anything"); ok {
		t.Error("an empty store authenticated something")
	}
	if len(s.List()) != 0 {
		t.Error("a fresh store already has tokens")
	}
}

func TestOpenOnAnUnreadableFileStartsEmptyRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Error("a garbled tokens.json produced tokens from nowhere")
	}
	if _, _, err := s.Create("recovered"); err != nil {
		t.Errorf("Create after a garbled load: %v", err)
	}
}
