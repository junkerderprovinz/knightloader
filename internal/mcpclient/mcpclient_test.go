package mcpclient

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAClientSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Empty() {
		t.Fatal("a new store is not empty")
	}
	c, err := s.Add("claude-code", "tok1")
	if err != nil {
		t.Fatal(err)
	}
	s.Touch(c.ID, time.Now())

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.ByToken("tok1")
	if !ok || got.ID != c.ID || got.Kind != "claude-code" || got.LastCall == nil {
		t.Fatalf("after a reopen ByToken = %+v, %v", got, ok)
	}
}

func TestATokenBelongsToOneClient(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.Add("cursor", "tok1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("zed", "tok1"); !errors.Is(err, ErrTokenInUse) {
		t.Errorf("a second client on the same token: %v", err)
	}
	if _, err := s.Add("", "tok2"); !errors.Is(err, ErrNoKind) {
		t.Errorf("a client without a kind: %v", err)
	}
}

func TestRemovingTheLastClientEmptiesTheStore(t *testing.T) {
	s, _ := Open(t.TempDir())
	c, _ := s.Add("cursor", "tok1")
	got, err := s.Remove(c.ID)
	if err != nil || got.TokenID != "tok1" {
		t.Fatalf("Remove = %+v, %v", got, err)
	}
	if !s.Empty() {
		t.Error("the store is not empty after its only client went")
	}
	if _, err := s.Remove(c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing it twice: %v", err)
	}
}

func TestTouchWritesAtMostOncePerMinute(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	c, _ := s.Add("cursor", "tok1")
	now := time.Now()
	s.Touch(c.ID, now)
	path := filepath.Join(dir, fileName)
	before, _ := os.ReadFile(path)
	s.Touch(c.ID, now.Add(10*time.Second))
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a second call within the minute wrote the file again")
	}
	if got, _ := s.ByToken("tok1"); !got.LastCall.Equal(now.Add(10 * time.Second).UTC()) {
		t.Errorf("the list in memory has %v, want the latest call", got.LastCall)
	}
}

func TestADamagedFileClosesTheEndpointRatherThanTheServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Empty() {
		t.Error("a damaged file left clients behind")
	}
}
