package ytdlp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

func newCookieStore(t *testing.T) *CookieStore {
	t.Helper()
	a, err := accounts.Open(t.TempDir())
	if err != nil {
		t.Fatalf("accounts.Open: %v", err)
	}
	return NewCookieStore(a)
}

func TestCookieStoreRoundTripsOneJarPerHost(t *testing.T) {
	s := newCookieStore(t)
	const jar = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\tabc\n"
	if err := s.Set("youtube.com", jar); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Text("https://www.youtube.com/watch?v=x"); got != jar {
		t.Errorf("Text = %q, want the jar verbatim", got)
	}
	if hosts := s.Hosts(); len(hosts) != 1 || hosts[0] != "youtube.com" {
		t.Errorf("Hosts = %v, want [youtube.com]", hosts)
	}
	if got := s.Text("https://vimeo.com/1"); got != "" {
		t.Errorf("Text for an unrelated host = %q, want empty", got)
	}
}

func TestCookieStoreSealsTheJarOnDisk(t *testing.T) {
	dir := t.TempDir()
	a, err := accounts.Open(dir)
	if err != nil {
		t.Fatalf("accounts.Open: %v", err)
	}
	const secret = "top-secret-session"
	if err := NewCookieStore(a).Set("youtube.com", "SID\t"+secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("accounts.json holds the session in the clear:\n%s", raw)
	}
}

func TestCookieStoreWalksUpToTheParentDomain(t *testing.T) {
	s := newCookieStore(t)
	if err := s.Set("youtube.com", "jar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for _, u := range []string{
		"https://music.youtube.com/watch?v=x",
		"https://www.youtube.com/watch?v=x",
		"https://m.youtube.com/watch?v=x",
	} {
		if got := s.Text(u); got != "jar" {
			t.Errorf("Text(%q) = %q, want the parent domain's jar", u, got)
		}
	}
}

func TestCookieStoreMostSpecificHostWins(t *testing.T) {
	s := newCookieStore(t)
	if err := s.Set("youtube.com", "site-wide"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("music.youtube.com", "music-only"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Text("https://music.youtube.com/watch?v=x"); got != "music-only" {
		t.Errorf("Text = %q, want the subdomain's own jar", got)
	}
}

func TestCookieStoreNormalisesTheHost(t *testing.T) {
	s := newCookieStore(t)
	if err := s.Set("  WWW.YouTube.com ", "jar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if hosts := s.Hosts(); len(hosts) != 1 || hosts[0] != "youtube.com" {
		t.Errorf("Hosts = %v, want the normalised [youtube.com]", hosts)
	}
}

func TestCookieStoreRemoveClearsTheEntry(t *testing.T) {
	s := newCookieStore(t)
	if err := s.Set("youtube.com", "jar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Remove("youtube.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := s.Text("https://youtube.com/x"); got != "" {
		t.Errorf("Text after Remove = %q, want empty", got)
	}
	if hosts := s.Hosts(); len(hosts) != 0 {
		t.Errorf("Hosts after Remove = %v, want none", hosts)
	}
}

func TestCookieStoreOnANilStoreAnswersNothing(t *testing.T) {
	var s *CookieStore
	if got := s.Text("https://youtube.com/x"); got != "" {
		t.Errorf("Text on a nil store = %q, want empty", got)
	}
	if hosts := s.Hosts(); hosts != nil {
		t.Errorf("Hosts on a nil store = %v, want nil", hosts)
	}
	if err := s.Set("youtube.com", "jar"); err == nil {
		t.Errorf("Set on a nil store returned no error")
	}
}

func TestWriteCookieFileIsPrivateAndRemovable(t *testing.T) {
	dir := t.TempDir()
	const jar = "SID\tabc\n"
	path, cleanup, err := writeCookieFile(dir, jar)
	if err != nil {
		t.Fatalf("writeCookieFile: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if string(body) != jar {
		t.Errorf("file holds %q, want the jar verbatim", body)
	}
	if runtime.GOOS != "windows" {
		// Windows has no POSIX modes.
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("mode = %04o, want 0600 - this file is a live session", mode)
		}
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file survived cleanup (stat err = %v)", err)
	}
	// A second call must be harmless.
	cleanup()
}

// Error strings reach the task, the log and the diagnostics bundle.
func TestCookieFileErrorsCarryNothingAboutTheJar(t *testing.T) {
	const secret = "top-secret-session"
	_, _, err := writeCookieFile(filepath.Join(t.TempDir(), "no", "such", "dir"), "SID\t"+secret)
	if err == nil {
		t.Fatalf("writeCookieFile into a missing directory returned no error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error carries the jar: %v", err)
	}
}
