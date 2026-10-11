package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestAFolderThatDoesNotAnswerStallsNothing: looking a download's folder up
// walks it on disk, and a network mount can hang there. The dispatcher, which
// holds a.mu, gives up after folderLookupWait, the task waits, and the next
// pass starts it once the answer is in.
func TestAFolderThatDoesNotAnswerStallsNothing(t *testing.T) {
	a, _ := diskApp(t, func(*settings.Settings) {})
	release := make(chan struct{})
	a.folders.lookup = func(string) *dataFolderError {
		<-release
		return nil
	}
	queueSized(a, "d1", 0)
	start := time.Now()
	running, waiting := dispatchNow(a)
	if took := time.Since(start); took > folderLookupWait+time.Second {
		t.Fatalf("the dispatcher held the lock for %s on a folder that does not answer", took)
	}
	if running["d1"] || waiting["d1"] == "" {
		t.Fatalf("d1 running=%v waiting=%q, want it waiting while its folder is looked up", running["d1"], waiting["d1"])
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.Lock()
		on := a.active["d1"]
		a.mu.Unlock()
		if on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("d1 never started once its folder answered")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
