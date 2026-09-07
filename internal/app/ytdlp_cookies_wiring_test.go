package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// TestYtdlpBackendIsHandedTheCookieJars guards the one line that makes the
// cookie feature reachable.
//
// CookieStore is complete and tested on its own, and Backend.Cookies is nil
// until rewireBackends hands it over. A nil hook means "no opinion", so
// dropping that line would leave every download behaving exactly as before
// while the settings page cheerfully accepted jars that nothing ever read -
// a feature that is present, tested, and dead. Only the wiring can be asserted
// here, and only the wiring is.
func TestYtdlpBackendIsHandedTheCookieJars(t *testing.T) {
	dir := t.TempDir()
	// A stub that answers --version, because rewireBackends only builds the
	// backend when Available() succeeds and the real binary is not on every
	// machine this runs on.
	bin := filepath.Join(dir, "ytdlp-stub")
	script := "#!/bin/sh\necho 2026.01.01\n"
	if runtime.GOOS == "windows" {
		bin += ".bat"
		script = "@echo 2026.01.01\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if exec.Command(bin, "--version").Run() != nil {
		t.Skip("the stub is not executable here, and this test is about wiring, not about shells")
	}
	t.Setenv("KL_YTDLP", bin)

	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.mu.Lock()
	yb, _ := a.ytdlp.(*ytdlp.Backend)
	a.mu.Unlock()
	if yb == nil {
		t.Fatal("no yt-dlp backend was built although the stub answers --version; this test cannot see the wiring it exists for")
	}
	if yb.Cookies == nil {
		t.Fatal("Backend.Cookies is nil, so every stored cookie jar is unreachable and the setting is decoration")
	}

	const jar = "# Netscape HTTP Cookie File\n.example.test\tTRUE\t/\tFALSE\t0\tsid\tsecret\n"
	if err := a.Accounts.SetCredential(ytdlp.CookieService, "example.test", accounts.Credential{APIKey: jar}); err != nil {
		t.Fatal(err)
	}
	if got := yb.Cookies("https://example.test/watch?v=1"); got != jar {
		t.Fatalf("the hook returned %q for a host with a stored jar, want the jar back", got)
	}
}
