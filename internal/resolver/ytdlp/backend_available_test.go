package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// spawnLogEnv names the file the "slowstart" fake writes a line to each time
// it starts.
const spawnLogEnv = "KL_YTDLP_SPAWN_LOG"

func TestAYtdlpThatStartsSlowlyIsTakenOnceItAnswersAndNotStartedAgain(t *testing.T) {
	log := filepath.Join(t.TempDir(), "spawns")
	t.Setenv(spawnLogEnv, log)
	b := fakeYtdlpBackend(t, "slowstart")
	previous, previousSlow := availableTimeout, availableSlowTimeout
	availableTimeout, availableSlowTimeout = 200*time.Millisecond, 20*time.Second
	defer func() { availableTimeout, availableSlowTimeout = previous, previousSlow }()

	if b.Available() {
		t.Fatal("a yt-dlp slower than the quick check was taken before it answered")
	}
	deadline := time.Now().Add(20 * time.Second)
	for !b.Available() {
		if time.Now().After(deadline) {
			t.Fatal("a yt-dlp that answers late was never taken")
		}
		time.Sleep(50 * time.Millisecond)
	}
	spawns := func() int {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "start")
	}
	before := spawns()
	for range 3 {
		if !b.Available() {
			t.Fatal("a yt-dlp that answered once was given up on")
		}
	}
	if after := spawns(); after != before {
		t.Errorf("yt-dlp was started %d more times after it had answered", after-before)
	}
}

// The fake yt-dlp sleeps for a minute in "hang" mode, like a binary that
// starts and never answers.
func TestAvailableDoesNotWaitForeverOnABinaryThatNeverAnswers(t *testing.T) {
	t.Setenv(probeHelperEnv, "hang")
	b := NewBackend(os.Args[0], t.TempDir(), func(string, core.Update) {})

	// Restored afterwards, since other tests spawn the same helper.
	previous, previousSlow := availableTimeout, availableSlowTimeout
	availableTimeout, availableSlowTimeout = 300*time.Millisecond, 300*time.Millisecond
	defer func() { availableTimeout, availableSlowTimeout = previous, previousSlow }()

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
