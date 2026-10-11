package appkey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustGenerate(t *testing.T) *Key {
	t.Helper()
	k, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestParseTakesSixtyFourHexDigits(t *testing.T) {
	k := mustGenerate(t)
	back, err := Parse(" " + strings.ToUpper(k.Hex()) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if back.Hex() != k.Hex() {
		t.Fatal("a key read back is not the key written")
	}
	for _, bad := range []string{"", "abc", k.Hex()[:62], k.Hex() + "00", strings.Repeat("zz", 32)} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestFromEnvWithoutTheVariableIsNoKey(t *testing.T) {
	t.Setenv(EnvVar, "")
	k, err := FromEnv()
	if k != nil || err != nil {
		t.Fatalf("FromEnv() = %v, %v; want no key and no error", k, err)
	}
	t.Setenv(EnvVar, "not hex")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a malformed variable was taken as no key")
	}
}

func TestSealedValueOpensOnlyWithItsKeyAndPurpose(t *testing.T) {
	k := mustGenerate(t)
	sealed, err := k.SealString("settings", "router-password")
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(sealed) || strings.Contains(sealed, "router-password") {
		t.Fatalf("sealed value %q is not sealed", sealed)
	}
	if got, err := k.OpenString("settings", sealed); err != nil || got != "router-password" {
		t.Fatalf("OpenString = %q, %v", got, err)
	}
	if _, err := k.OpenString("auth.json", sealed); !errors.Is(err, ErrWrongKey) {
		t.Errorf("opened under another purpose: %v", err)
	}
	if _, err := mustGenerate(t).OpenString("settings", sealed); !errors.Is(err, ErrWrongKey) {
		t.Errorf("opened with another key: %v", err)
	}
	var none *Key
	if _, err := none.OpenString("settings", sealed); !errors.Is(err, ErrMissing) {
		t.Errorf("opened without a key: %v", err)
	}
}

func TestKeepCopiesCopiesOnlyWhatExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"hash":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := KeepCopies(dir, []string{"auth.json", "settings.json"}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != CopiesPrefix+"20260102-030405" {
		t.Fatalf("copies went to %s", got)
	}
	if b, err := os.ReadFile(filepath.Join(got, "auth.json")); err != nil || string(b) != `{"hash":"x"}` {
		t.Errorf("auth.json copy = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(got, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("a file that did not exist was copied: %v", err)
	}

	none, err := KeepCopies(t.TempDir(), []string{"auth.json"}, time.Now())
	if err != nil || none != "" {
		t.Errorf("KeepCopies with nothing to copy = %q, %v", none, err)
	}
}
