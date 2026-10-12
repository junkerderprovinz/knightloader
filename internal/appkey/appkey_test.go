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

func copiesIn(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, CopiesPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func madeCopies(t *testing.T, made time.Time) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := KeepCopies(dir, []string{"auth.json"}, made); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCopiesStayForSevenDaysWithoutAFailure(t *testing.T) {
	made := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := madeCopies(t, made)

	if until, ok := CopiesUntil(dir, made); !ok || !until.Equal(made.Add(CopiesKept)) {
		t.Fatalf("CopiesUntil = %v, %v; want %v", until, ok, made.Add(CopiesKept))
	}
	if removed, err := SweepCopies(dir, made.Add(CopiesKept-time.Minute)); err != nil || len(removed) > 0 || len(copiesIn(t, dir)) != 1 {
		t.Fatalf("the copies went before seven days: %v, %v", removed, err)
	}
	if removed, err := SweepCopies(dir, made.Add(CopiesKept)); err != nil || len(removed) != 1 || len(copiesIn(t, dir)) != 0 {
		t.Fatalf("the copies stayed after seven days: %v, %v", removed, err)
	}
	if _, ok := CopiesUntil(dir, made); ok {
		t.Fatal("CopiesUntil still reports copies that are gone")
	}
}

func TestAFailureKeepsTheCopiesForAnotherSevenDays(t *testing.T) {
	made := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dir := madeCopies(t, made)
	failed := made.Add(5 * 24 * time.Hour)

	k := mustGenerate(t)
	k.OnFailure(func(error) {
		if err := NoteFailure(dir, failed); err != nil {
			t.Error(err)
		}
	})
	if _, err := k.OpenString("settings", "kl-sealed:v1:not-what-it-claims"); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("OpenString = %v", err)
	}

	if removed, _ := SweepCopies(dir, made.Add(CopiesKept)); len(removed) > 0 {
		t.Fatal("the copies went seven days after they were made despite a failure in between")
	}
	if until, _ := CopiesUntil(dir, made); !until.Equal(failed.Add(CopiesKept)) {
		t.Fatalf("CopiesUntil = %v after a failure, want %v", until, failed.Add(CopiesKept))
	}
	if removed, _ := SweepCopies(dir, failed.Add(CopiesKept)); len(removed) != 1 {
		t.Fatal("the copies stayed seven days after the failure")
	}
}

func TestAForeignOpenIsNotReportedAsAFailure(t *testing.T) {
	k := mustGenerate(t)
	reported := false
	k.OnFailure(func(error) { reported = true })
	if _, err := k.OpenForeign("backup", []byte("not sealed by this key at all")); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("OpenForeign = %v", err)
	}
	if reported {
		t.Fatal("an upload that does not open was counted against this install's own data")
	}
}
