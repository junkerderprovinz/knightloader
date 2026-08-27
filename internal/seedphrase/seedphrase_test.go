package seedphrase

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The phrase is the only thing standing between two instances finding each
// other and not, so a secret that does not survive the round trip is the
// one failure this package must never have. Run enough times to catch a
// bit-level mistake that only shows for some inputs rather than all.
func TestRoundTrip(t *testing.T) {
	for i := 0; i < 2000; i++ {
		secret, phrase, err := New()
		if err != nil {
			t.Fatalf("New() = %v", err)
		}
		if got := len(strings.Fields(phrase)); got != WordCount {
			t.Fatalf("phrase has %d words, want %d: %q", got, WordCount, phrase)
		}
		back, err := Decode(phrase)
		if err != nil {
			t.Fatalf("Decode(%q) = %v", phrase, err)
		}
		if !bytes.Equal(secret, back) {
			t.Fatalf("round trip changed the secret:\n in: %x\nout: %x\nphrase: %q", secret, back, phrase)
		}
	}
}

// Encode has to be a pure function of the secret - the phrase shown when
// somebody re-opens the page months later must be the same one they wrote
// down.
func TestEncodeIsStable(t *testing.T) {
	secret := make([]byte, SecretLen)
	for i := range secret {
		secret[i] = byte(i * 7)
	}
	first, err := Encode(secret)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		again, err := Encode(secret)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("Encode is not stable: %q then %q", first, again)
		}
	}
}

// An all-zero secret is the input most likely to expose an off-by-one in
// the bit packing, because every bit that should be clear is clear - a
// stray set bit has nothing to hide behind.
func TestKnownVector(t *testing.T) {
	secret := make([]byte, SecretLen)
	phrase, err := Encode(secret)
	if err != nil {
		t.Fatal(err)
	}
	// The canonical BIP39 all-zero 128-bit vector.
	want := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if phrase != want {
		t.Fatalf("Encode(all zero) =\n  %q\nwant\n  %q", phrase, want)
	}
	back, err := Decode(phrase)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, secret) {
		t.Fatalf("Decode(all-zero vector) = %x, want %x", back, secret)
	}
}

// A mistyped word that happens to land on another real word is the failure
// the checksum exists for: every word is valid, the phrase is not.
func TestChecksumCatchesASwappedWord(t *testing.T) {
	_, phrase, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(phrase)
	// Replace the first word with a different real word, keeping length and
	// validity so only the checksum can object.
	replacement := "zebra"
	if got[0] == replacement {
		replacement = "zoo"
	}
	got[0] = replacement

	_, err = Decode(strings.Join(got, " "))
	if err == nil {
		t.Fatal("Decode accepted a phrase with a substituted word")
	}
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("Decode error = %v, want ErrChecksum", err)
	}
}

// A word that is not in the list at all must say WHICH word, or the person
// holding a twelve-word phrase has to find the typo by bisection.
func TestUnknownWordIsNamedWithItsPosition(t *testing.T) {
	_, phrase, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(phrase)
	got[6] = "recieve" // a real-looking misspelling, not in the list

	_, err = Decode(strings.Join(got, " "))
	if err == nil {
		t.Fatal("Decode accepted a phrase containing a non-word")
	}
	msg := err.Error()
	if !strings.Contains(msg, "recieve") {
		t.Errorf("error does not name the offending word: %q", msg)
	}
	if !strings.Contains(msg, "7") {
		t.Errorf("error does not name the word's position (7): %q", msg)
	}
}

func TestWrongWordCount(t *testing.T) {
	_, phrase, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(phrase)

	for _, c := range []struct {
		name  string
		words []string
	}{
		{"one short", got[:WordCount-1]},
		{"one too many", append(append([]string{}, got...), got[0])},
		{"empty", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode(strings.Join(c.words, " ")); err == nil {
				t.Fatalf("Decode accepted %d words", len(c.words))
			}
		})
	}
}

// This arrives from a paste or from somebody typing what was read to them,
// so the shapes below are what real input looks like - not edge cases.
func TestDecodeNormalisesInput(t *testing.T) {
	secret, phrase, err := New()
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Fields(phrase)

	for _, c := range []struct {
		name  string
		input string
	}{
		{"as generated", phrase},
		{"upper case", strings.ToUpper(phrase)},
		{"mixed case", strings.Title(phrase)}, //nolint:staticcheck // deprecated, fine for ASCII test input
		{"leading and trailing space", "   " + phrase + "   \n"},
		{"newlines between words", strings.Join(words, "\n")},
		{"double spaces", strings.Join(words, "  ")},
		{"tabs", strings.Join(words, "\t")},
	} {
		t.Run(c.name, func(t *testing.T) {
			back, err := Decode(c.input)
			if err != nil {
				t.Fatalf("Decode(%q) = %v", c.input, err)
			}
			if !bytes.Equal(back, secret) {
				t.Fatalf("Decode(%q) = %x, want %x", c.input, back, secret)
			}
		})
	}
}

func TestEncodeRejectsWrongSecretLength(t *testing.T) {
	for _, n := range []int{0, 1, SecretLen - 1, SecretLen + 1, 32} {
		if _, err := Encode(make([]byte, n)); err == nil {
			t.Errorf("Encode accepted a %d-byte secret", n)
		}
	}
}

// The embedded wordlist is load-bearing: swap a word and every phrase ever
// written down decodes to a different secret, silently. Pinning its hash
// means a change to that file has to be a deliberate act with a test edit
// beside it, not something that rides along in an unrelated commit.
func TestWordlistIsTheOfficialOne(t *testing.T) {
	const official = "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda"
	sum := sha256.Sum256([]byte(wordlistFile))
	if got := hex.EncodeToString(sum[:]); got != official {
		t.Fatalf("wordlist hash = %s, want the official BIP39 English list %s", got, official)
	}
	if len(words) != 2048 {
		t.Fatalf("wordlist has %d words, want 2048", len(words))
	}
	if words[0] != "abandon" || words[len(words)-1] != "zoo" {
		t.Fatalf("wordlist bounds = %q..%q, want abandon..zoo", words[0], words[len(words)-1])
	}
}

// Two secrets differing in a single bit must not produce phrases that
// differ only in ways a person would overlook - a sanity check that the
// encoding actually spreads the input across the words.
func TestNeighbouringSecretsLookDifferent(t *testing.T) {
	a := make([]byte, SecretLen)
	b := make([]byte, SecretLen)
	b[SecretLen-1] = 1

	pa, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	if pa == pb {
		t.Fatal("two different secrets encoded to the same phrase")
	}
}
