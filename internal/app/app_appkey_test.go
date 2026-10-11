package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

func openWithKey(t *testing.T, dir string, key *appkey.Key) *App {
	t.Helper()
	a, err := NewWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAddingAnAppKeyKeepsCopiesOnceAndSealsInPlace(t *testing.T) {
	dir := t.TempDir()
	a := openWithKey(t, dir, nil)
	if err := a.Auth.SetPassword("", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	s.Reconnect.Username = "admin"
	s.Reconnect.Password = "router-secret"
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.Close()
	before, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	key, _ := appkey.Generate()
	a = openWithKey(t, dir, key)
	if !a.Auth.Check("correct-horse") || a.Settings.Get().Reconnect.Password != "router-secret" {
		t.Fatal("the first start with a key lost the password or the router password")
	}
	a.Close()

	copies, _ := filepath.Glob(filepath.Join(dir, appkey.CopiesPrefix+"*"))
	if len(copies) != 1 {
		t.Fatalf("%d folders of copies, want 1", len(copies))
	}
	if kept, _ := os.ReadFile(filepath.Join(copies[0], "settings.json")); string(kept) != string(before) {
		t.Error("the copy of settings.json is not the file as it was")
	}
	for _, name := range []string{"auth.json", ".keyring"} {
		if _, err := os.Stat(filepath.Join(copies[0], name)); err != nil {
			t.Errorf("no copy of %s: %v", name, err)
		}
	}
	if now, _ := os.ReadFile(filepath.Join(dir, "settings.json")); strings.Contains(string(now), "router-secret") {
		t.Error("settings.json still holds the router password in the clear")
	}

	a = openWithKey(t, dir, key)
	a.Close()
	if again, _ := filepath.Glob(filepath.Join(dir, appkey.CopiesPrefix+"*")); len(again) != 1 {
		t.Fatalf("a later start with the same key made more copies: %d folders", len(again))
	}

	if _, err := NewWithKey(dir, nil); !errors.Is(err, appkey.ErrMissing) {
		t.Fatalf("a start without the key = %v, want ErrMissing", err)
	}
}
