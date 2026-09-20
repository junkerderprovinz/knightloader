//go:build bridgeclipboard

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/atotto/clipboard"
)

// TestWatchClipboardReturnsPromptlyWhenUnsupported covers a headless runner
// without xclip, xsel or wl-clipboard, where WatchClipboard has to return
// rather than block.
func TestWatchClipboardReturnsPromptlyWhenUnsupported(t *testing.T) {
	if !clipboard.Unsupported {
		t.Skip("this runner has a working clipboard; the branch under test only runs without one")
	}

	b := &Bridge{remote: "http://127.0.0.1:0", timeout: time.Second}
	done := make(chan struct{})
	go func() {
		b.WatchClipboard(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchClipboard did not return promptly on an unsupported system")
	}
}

func TestWatchClipboardStopsOnContextCancel(t *testing.T) {
	if clipboard.Unsupported {
		t.Skip("no clipboard on this runner; nothing to poll")
	}
	orig := clipboardPollInterval
	clipboardPollInterval = 10 * time.Millisecond
	defer func() { clipboardPollInterval = orig }()

	b := &Bridge{remote: "http://127.0.0.1:0", timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.WatchClipboard(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchClipboard did not stop after its context was cancelled")
	}
}
