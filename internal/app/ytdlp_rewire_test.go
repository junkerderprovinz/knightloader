package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// hangingYtdlpStub answers --version and otherwise runs until it is killed,
// like a yt-dlp in the middle of a download.
func hangingYtdlpStub(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ytdlp-stub")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 2026.01.01; exit 0; fi\nexec sleep 60\n"
	if runtime.GOOS == "windows" {
		bin += ".bat"
		script = "@echo off\r\nif \"%~1\"==\"--version\" (echo 2026.01.01& exit /b 0)\r\nping -n 61 127.0.0.1 >nul\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if exec.Command(bin, "--version").Run() != nil {
		t.Skip("the yt-dlp stub is not executable here")
	}
	return bin
}

// Saving an account, switching JD on and updating yt-dlp all rewire the
// backends. A pause or a removal after that has to reach the yt-dlp that is
// already downloading.
func TestAYtdlpDownloadCanStillBeStoppedAfterARewire(t *testing.T) {
	t.Setenv("KL_YTDLP", hangingYtdlpStub(t))
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.backendFor("ytdlp").Download("task-1", "https://media.example/watch?v=1", nil, 0)
	a.rewireBackends()

	halter, ok := a.backendFor("ytdlp").(interface{ Halt(string) bool })
	if !ok {
		t.Fatal("yt-dlp tasks are not routed to the yt-dlp backend after the rewire")
	}
	if !halter.Halt("task-1") {
		t.Fatal("the backend yt-dlp tasks go to after the rewire does not know the running download")
	}
}
