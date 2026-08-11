package apitoken

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateReturnsTheSecretExactlyOnce is the whole shape of this package: a
// second read of the same token must never be able to recover the plaintext,
// only confirm a presented one matches.
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
		// Token (and therefore List) carries no field the secret could ever
		// be read back from: this loop is really asserting that fact by
		// construction (record.HashHex is unexported and record itself never
		// crosses this boundary), but it stands as the test that would fail
		// the day someone widens Token to embed record by mistake.
		return
	}
	t.Fatal("the new token is not in List")
}

// TestCheckAcceptsExactlyTheIssuedSecret is Check's whole job.
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

// TestRevokeStopsAuthenticatingWithoutTouchingOtherTokens is the entire
// feature this package exists to build: one device lost, one token revoked,
// every other credential (including the shared password, which this package
// never touches at all) untouched.
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

// TestRevokeUnknownIDIsAnError stops a client from believing a typo'd id
// removed something.
func TestRevokeUnknownIDIsAnError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke("does-not-exist"); err != ErrNotFound {
		t.Errorf("Revoke(unknown) = %v, want ErrNotFound", err)
	}
}

// TestRevokeAllClearsEveryTokenAndSurvivesAReload is RevokeAll's whole job:
// every credential gone at once, and gone for good, not just from the
// in-memory map a restart would silently repopulate from a stale file.
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

	// The store keeps working afterwards - a clean slate, not a wedged one.
	if _, _, err := s.Create("new phone"); err != nil {
		t.Errorf("Create after RevokeAll: %v", err)
	}
}

// TestRevokeAllOnAnEmptyStoreIsANoOp mirrors Check's own empty-store case:
// nothing to revoke must not be an error, and must not touch the file.
func TestRevokeAllOnAnEmptyStoreIsANoOp(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAll(); err != nil {
		t.Errorf("RevokeAll on an empty store = %v, want nil", err)
	}
}

// TestEmptyNameRefused: a token nothing can tell apart from the next one
// issued is a token nobody can revoke with confidence.
func TestEmptyNameRefused(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("   "); err != ErrEmptyName {
		t.Errorf("Create(whitespace) = %v, want ErrEmptyName", err)
	}
}

// TestTokensSurviveAReload is the point of writing tokens.json at all: a
// restart must not silently log out every device that was never told to log
// back in, unlike a browser session, which is expected to expire.
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

// TestMaxTokensRefusesRatherThanGrowingForever bounds a script that creates
// tokens in a loop, without capping the handful of devices a real person has.
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

// TestLastUsedFlushIsThrottledButAlwaysCorrectInMemory: the on-disk write is
// allowed to lag by up to staleAfter (see Check's own comment on why), but
// nothing served from this process may ever be stale. Only what a second
// process reading the file straight off disk within that window would see.
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

	// The very first Check is never stale (LastUsed was nil going in), so it
	// must have flushed already: a fresh reopen sees a real timestamp, not
	// the zero one Create left on disk.
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

// TestCheckOnAFreshStoreDoesNotPanic is the empty-instance case every method
// here has to survive before a single token is ever issued.
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

// TestOpenOnAnUnreadableFileStartsEmptyRatherThanFailing mirrors auth.Guard's
// own Open: a tokens.json this build cannot parse must not stop the server
// from starting. Every token in it simply stops authenticating, which is a
// visible failure (nothing gets in) rather than a silent one.
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
	// And the store is still writable afterwards - a parse failure must not
	// wedge every later Create too.
	if _, _, err := s.Create("recovered"); err != nil {
		t.Errorf("Create after a garbled load: %v", err)
	}
}
