package ytdlp

import (
	"os"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The fake yt-dlp sleeps for a minute in "hang" mode, like a binary that
// starts and never answers.
func TestAvailableDoesNotWaitForeverOnABinaryThatNeverAnswers(t *testing.T) {
	t.Setenv(probeHelperEnv, "hang")
	b := NewBackend(os.Args[0], t.TempDir(), func(string, core.Update) {})

	// Restored afterwards, since other tests spawn the same helper.
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
		// Generous for a loaded CI box, still far below the fake's minute.
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("Available took %s, so the ceiling is not the thing that ended it", elapsed)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Available never returned: the spawn has no ceiling on it")
	}
}
