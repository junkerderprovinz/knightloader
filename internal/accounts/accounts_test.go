package accounts

import (
	"crypto/rand"
	"encoding/base64"
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

// sealLegacy writes a value the way the pre-Credential code did: the whole
// plaintext is the secret itself, not a JSON object. There is no longer a
// public path that produces this shape - Set has sealed a Credential since
// the catalogue rewrite - so the migration test builds it by hand to stand in
// for an accounts.json that predates this package understanding Credential.
func sealLegacy(t *testing.T, s *Store, service, plaintext string) {
	t.Helper()
	g, err := s.gcm()
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	ct := g.Seal(nonce, nonce, []byte(plaintext), nil)
	s.data[service] = base64.StdEncoding.EncodeToString(ct)
	if err := s.flush(); err != nil {
		t.Fatal(err)
	}
}

// TestLegacyPlainStringReadsAsAPIKey is the migration this package promises:
// a secret written before Credential existed reads back as {"apiKey": <it>}
// on every subsequent read, and reading it must never rewrite accounts.json -
// a process killed between reading the old shape and writing the new one
// would leave a file neither format can parse.
func TestLegacyPlainStringReadsAsAPIKey(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const legacy = "torbox-secret-key-123"
	sealLegacy(t, s, "torbox", legacy)

	// Both the old accessor and the new one see it as an API-key credential.
	if got, err := s.Get("torbox"); err != nil || got != legacy {
		t.Fatalf("Get = %q, %v; want %q, nil", got, err, legacy)
	}
	cred, err := s.GetCredential("torbox", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Credential{APIKey: legacy}); cred != want {
		t.Fatalf("GetCredential = %+v, want %+v", cred, want)
	}

	// Reading it must not rewrite accounts.json into the new shape.
	before, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Get("torbox"); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("accounts.json changed on read:\nbefore: %s\nafter:  %s", before, after)
	}

	// A fresh process re-opening the same directory sees the same thing: the
	// interpretation happens on every read, not once inside Open.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s2.Get("torbox"); err != nil || got != legacy {
		t.Fatalf("after reopen Get = %q, %v; want %q, nil", got, err, legacy)
	}
}

// TestCredentialRedactMergeRoundTrip exercises the same redact/merge
// convention reconnect.Config uses: Redacted hides secrets for display,
// WithSecretsFrom puts back whichever ones a caller sent back unchanged, and
// a caller that genuinely edited or cleared a field is never overridden.
func TestCredentialRedactMergeRoundTrip(t *testing.T) {
	full := Credential{APIKey: "abc123", Username: "alice", Password: "hunter2"}

	redacted := full.Redacted()
	if redacted.APIKey != Redacted || redacted.Password != Redacted {
		t.Fatalf("Redacted() = %+v, want secrets replaced by %q", redacted, Redacted)
	}
	if redacted.Username != full.Username {
		t.Fatalf("Redacted() changed Username to %q, want it left alone", redacted.Username)
	}

	// Sent back unchanged, the placeholder resolves to the original secrets.
	if merged := redacted.WithSecretsFrom(full); merged != full {
		t.Fatalf("WithSecretsFrom(unchanged) = %+v, want %+v", merged, full)
	}

	// A retyped API key and a cleared password must both stick - the new key,
	// and a genuinely empty password rather than the old one coming back.
	edited := Credential{APIKey: "new-key", Username: "alice", Password: ""}
	if merged := edited.WithSecretsFrom(full); merged != edited {
		t.Fatalf("WithSecretsFrom(edited) = %+v, want %+v", merged, edited)
	}

	// The same round trip through the store: redact what Get returns, "save"
	// it back unchanged, and read the original secrets back out.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCredential("rapidgator", "", full); err != nil {
		t.Fatal(err)
	}
	shown, err := s.GetCredential("rapidgator", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCredential("rapidgator", "", shown.Redacted().WithSecretsFrom(full)); err != nil {
		t.Fatal(err)
	}
	roundTripped, err := s.GetCredential("rapidgator", "")
	if err != nil {
		t.Fatal(err)
	}
	if roundTripped != full {
		t.Fatalf("round trip through Store = %+v, want %+v", roundTripped, full)
	}
}

// TestTwoAccountsForOneServiceDoNotCollide is the second stored-value gap the
// catalogue rewrite closes: one opaque string per service could not hold two
// accounts for the same host (two Rapidgator logins, say). SetCredential's
// account id must keep them apart from each other, from the service's own
// default (unnamed) account, and from a same-named account on a different
// service.
func TestTwoAccountsForOneServiceDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := Credential{Username: "alice", Password: "alice-pw"}
	second := Credential{Username: "bob", Password: "bob-pw"}

	if err := s.SetCredential("rapidgator", "alice", first); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCredential("rapidgator", "bob", second); err != nil {
		t.Fatal(err)
	}

	if got, err := s.GetCredential("rapidgator", "alice"); err != nil || got != first {
		t.Fatalf("GetCredential(alice) = %+v, %v; want %+v, nil", got, err, first)
	}
	if got, err := s.GetCredential("rapidgator", "bob"); err != nil || got != second {
		t.Fatalf("GetCredential(bob) = %+v, %v; want %+v, nil", got, err, second)
	}

	// The default (unnamed) account is a third slot, untouched by either.
	if def, err := s.GetCredential("rapidgator", ""); err != nil || !def.IsZero() {
		t.Fatalf("default account = %+v, %v; want zero value - only named accounts were set", def, err)
	}

	if ids := s.AccountIDs("rapidgator"); len(ids) != 2 || ids[0] != "alice" || ids[1] != "bob" {
		t.Fatalf("AccountIDs = %v, want [alice bob]", ids)
	}

	// A same-named account on a different service is a different slot too -
	// the service id is part of the key, not only the account id.
	if other, err := s.GetCredential("otherhost", "alice"); err != nil || !other.IsZero() {
		t.Fatalf("otherhost/alice = %+v, %v; want zero value", other, err)
	}

	// Deleting one account must not disturb the other.
	if err := s.SetCredential("rapidgator", "alice", Credential{}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetCredential("rapidgator", "bob"); err != nil || got != second {
		t.Fatalf("after deleting alice, bob = %+v, %v; want %+v, nil", got, err, second)
	}
	if got, err := s.GetCredential("rapidgator", "alice"); err != nil || !got.IsZero() {
		t.Fatalf("after delete, alice = %+v, %v; want zero", got, err)
	}
}
