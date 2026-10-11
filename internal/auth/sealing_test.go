package auth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

// TestADamagedAuthJSONIsNotNoPassword: a file that does not read must stop
// the start, since reading it as empty opens the instance to anybody.
func TestADamagedAuthJSONIsNotNoPassword(t *testing.T) {
	key := newKey(t)
	plain, sealed := lockedFile(t, nil), lockedFile(t, key)
	for _, c := range []struct {
		name string
		file []byte
		key  *appkey.Key
	}{
		{"a plain file cut short", plain[:len(plain)/2], nil},
		{"a plain file cut short, started with a key", plain[:len(plain)/2], key},
		{"an empty file", nil, nil},
		{"a sealed file cut short", sealed[:len(sealed)/2], key},
		{"a sealed file holding something other than the state", sealedText(t, key, "not json"), key},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			if err := os.WriteFile(path, c.file, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := OpenWithKey(dir, c.key)
			if err == nil {
				t.Fatal("the start went ahead on a damaged auth.json")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "backup") {
				t.Errorf("the refusal does not name the file and the way out: %v", err)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(after, c.file) {
				t.Error("a refused start replaced the damaged file")
			}
		})
	}
}

func TestASealedFileThatDoesNotOpenSaysWhichCasesThereAre(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), lockedFile(t, newKey(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenWithKey(dir, newKey(t))
	if !errors.Is(err, appkey.ErrWrongKey) || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("a start with another key = %v, want ErrWrongKey that names a damaged file as the other case", err)
	}
}

func sealedText(t *testing.T, key *appkey.Key, plain string) []byte {
	t.Helper()
	s, err := key.SealString(sealPurpose, plain)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(s)
}
