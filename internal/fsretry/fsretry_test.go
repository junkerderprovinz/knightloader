package fsretry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestARenameWaitsForAReaderToLetGo(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to replace a file another handle has open")
	}
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new"), filepath.Join(dir, "state.json")
	for _, p := range []string{from, to} {
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// What a scanner does to a file it has just seen written.
	held, err := os.Open(to)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		held.Close()
	}()
	if err := Rename(from, to); err != nil {
		t.Fatalf("Rename gave up while the file was held for a moment: %v", err)
	}
	if got, _ := os.ReadFile(to); string(got) != from {
		t.Errorf("%s holds %q, want the renamed file", to, got)
	}
}

func TestReplaceLeavesOnlyTheNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "new" {
		t.Fatalf("the file holds %q, %v", b, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the folder holds %d files, want the one replaced", len(entries))
	}
	if info, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
}
