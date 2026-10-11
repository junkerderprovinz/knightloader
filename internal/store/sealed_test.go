package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

const (
	archivePassword  = "archive-secret-7q"
	downloadPassword = "download-secret-3k"
)

func withPasswords(id string) *core.Task {
	return &core.Task{ID: id, Name: "file.rar", CreatedAt: time.Now(), Password: archivePassword, DownloadPassword: downloadPassword}
}

func newKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func openAt(t *testing.T, path string, key *appkey.Key) *Store {
	t.Helper()
	s, err := OpenWithKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func onlyTask(t *testing.T, s *Store) *core.Task {
	t.Helper()
	all, err := s.All()
	if err != nil || len(all) != 1 {
		t.Fatalf("All = %d task(s), %v; want one", len(all), err)
	}
	return all[0]
}

// stored is what the password columns hold, past the store's own reading.
func stored(t *testing.T, s *Store, id string) (string, string) {
	t.Helper()
	var p, d string
	if err := s.db.QueryRow(`SELECT password, download_password FROM tasks WHERE id=?`, id).Scan(&p, &d); err != nil {
		t.Fatal(err)
	}
	return p, d
}

func TestACopiedDatabaseWithoutTheKeyShowsNoPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	key := newKey(t)
	s := openAt(t, path, key)
	if err := s.Save(withPasswords("t1")); err != nil {
		t.Fatal(err)
	}
	if got := onlyTask(t, s); got.Password != archivePassword || got.DownloadPassword != downloadPassword {
		t.Fatalf("with the key the task reads %q and %q", got.Password, got.DownloadPassword)
	}
	s.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(archivePassword)) || bytes.Contains(raw, []byte(downloadPassword)) {
		t.Fatal("the database file holds a task password in the clear")
	}

	copied := filepath.Join(t.TempDir(), "tasks.db")
	if err := os.WriteFile(copied, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []*appkey.Key{nil, newKey(t)} {
		other := openAt(t, copied, k)
		got := onlyTask(t, other)
		other.Close()
		if got.Password != "" || got.DownloadPassword != "" {
			t.Fatalf("a copy opened without its key reads %q and %q", got.Password, got.DownloadPassword)
		}
	}
}

func TestPasswordsStoredBeforeTheKeyAreSealedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	plain := openAt(t, path, nil)
	if err := plain.Save(withPasswords("t1")); err != nil {
		t.Fatal(err)
	}
	if err := plain.Save(&core.Task{ID: "t2", Name: "open.bin", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if p, d := stored(t, plain, "t1"); p != archivePassword || d != downloadPassword {
		t.Fatalf("without a key the columns hold %q and %q, want the passwords as they are", p, d)
	}
	plain.Close()

	s := openAt(t, path, newKey(t))
	defer s.Close()
	p, d := stored(t, s, "t1")
	if !appkey.IsSealed(p) || !appkey.IsSealed(d) {
		t.Fatalf("after the first open with a key the columns hold %q and %q", p, d)
	}
	if p, d := stored(t, s, "t2"); p != "" || d != "" {
		t.Fatalf("a task without passwords got %q and %q", p, d)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range all {
		if task.ID == "t1" && (task.Password != archivePassword || task.DownloadPassword != downloadPassword) {
			t.Fatalf("after sealing the task reads %q and %q", task.Password, task.DownloadPassword)
		}
	}
}

// Sealing rewrites the rows, which leaves the old values on SQLite's free pages
// and in the free space of the pages themselves. The file is rebuilt, so a copy
// of it taken afterwards holds no password in the clear.
func TestNoPlainPasswordIsLeftInTheFileAfterSealing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	plain := openAt(t, path, nil)
	for _, id := range []string{"t1", "t2", "t3"} {
		if err := plain.Save(withPasswords(id)); err != nil {
			t.Fatal(err)
		}
	}
	plain.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(archivePassword)) {
		t.Fatal("the file holds no password before sealing, so this test would prove nothing")
	}

	openAt(t, path, newKey(t)).Close()
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{archivePassword, downloadPassword} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("the file still holds %q in the clear after sealing", secret)
		}
	}
}
