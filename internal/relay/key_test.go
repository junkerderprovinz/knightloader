package relay

import (
	"encoding/hex"
	"strings"
	"testing"
)

// The whole point of deriving: what an instance sends to the relay must not
// be the thing a person holds. If the key ever equalled the secret, the
// relay operator would be able to reconstruct anyone's phrase.
func TestDeriveKeyIsNotTheSecret(t *testing.T) {
	secret := []byte("0123456789abcdef")
	key := DeriveKey(secret)
	if strings.Contains(key, hex.EncodeToString(secret)) {
		t.Fatal("the derived key contains the secret verbatim")
	}
	if key == string(secret) {
		t.Fatal("the derived key IS the secret")
	}
}

// Two instances given the same phrase have to land in the same group, on
// every machine, on every run - this is the one property the feature rests
// on.
func TestDeriveKeyIsDeterministic(t *testing.T) {
	secret := []byte("0123456789abcdef")
	first := DeriveKey(secret)
	for i := 0; i < 100; i++ {
		if again := DeriveKey(secret); again != first {
			t.Fatalf("DeriveKey is not deterministic: %q then %q", first, again)
		}
	}
}

func TestDeriveKeyDiffersPerSecret(t *testing.T) {
	a := DeriveKey([]byte("0123456789abcdef"))
	b := DeriveKey([]byte("0123456789abcdeg")) // one byte apart
	if a == b {
		t.Fatal("two different secrets derived the same relay key")
	}
}

// The relay refuses keys below minKeyLength, so a derived key that fell
// under it would make the feature unusable the moment it shipped.
func TestDeriveKeyClearsTheRelayMinimum(t *testing.T) {
	key := DeriveKey([]byte("0123456789abcdef"))
	if len(key) < minKeyLength {
		t.Fatalf("derived key is %d characters, relay requires at least %d", len(key), minKeyLength)
	}
	if len(key) != 64 {
		t.Fatalf("derived key is %d characters, expected 64 (hex of SHA-256)", len(key))
	}
	if _, err := hex.DecodeString(key); err != nil {
		t.Fatalf("derived key is not valid hex: %v", err)
	}
}
