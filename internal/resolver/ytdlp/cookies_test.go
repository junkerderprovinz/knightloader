package ytdlp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
