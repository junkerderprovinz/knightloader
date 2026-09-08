package ytdlp

import (
	"os"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Available is spawned from app.rewireBackends, which runs on every account
// save and on every sweep tick. Until 2026-09-08 it was a bare exec.Command
// with no context at all, so a yt-dlp that ACCEPTED the exec and then never
// answered - a half-written download, a file a virus scanner is holding open
// mid-scan, a binary on a network mount that has gone away - held both the
// account routes and the upkeep goroutine for as long as it felt like.
//
// The fake here is this package's own test binary re-executed in TestMain's
// "hang" mode (backend_test.go), which sleeps for a minute. Without the
// ceiling this test does not fail, it hangs for that whole minute and then the
// package times out - which is precisely the production failure, reproduced.
func TestAvailableDoesNotWaitForeverOnABinaryThatNeverAnswers(t *testing.T) {
	t.Setenv(probeHelperEnv, "hang")
	b := NewBackend(os.Args[0], t.TempDir(), func(string, core.Update) {})

	// Shrunk so the test does not sit out the real ceiling. Restored rather
	// than left changed: every other test in this package spawns the same
	// helper binary and a 300ms ceiling would start failing them for timing.
	previous := availableTimeout
	availableTimeout = 300 * time.Millisecond
	defer func() { availableTimeout = previous }()

	done := make(chan bool, 1)
	start := time.Now()
	go func() { done <- b.Available() }()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("a yt-dlp that never answered --version was reported as available")
		}
		// Generous against a loaded CI box, and still two orders of magnitude
		// below the minute the fake would otherwise sleep.
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("Available took %s, so the ceiling is not the thing that ended it", elapsed)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Available never returned: the spawn has no ceiling on it")
	}
}
