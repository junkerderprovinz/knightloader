//go:build bridgeclipboard

package bridge

// This file only exists in the -tags bridgeclipboard build, same as
// clipboard.go itself — see its package comment for why. It does not touch a
// real clipboard: a CI runner is headless and has none of xclip, xsel or
// wl-clipboard installed, which is precisely the clipboard.Unsupported case
// this test drives, deterministically, on every platform ci.yml runs.

import (
	"context"
	"testing"
	"time"

	"github.com/atotto/clipboard"
)

// TestWatchClipboardReturnsPromptlyWhenUnsupported pins the graceful-exit
// branch every platform without a clipboard takes (a headless CI runner, a
// container someone runs this build in anyway, a Linux box with none of
// xclip/xsel/wl-clipboard installed): WatchClipboard must return on its own
// rather than block forever pretending to poll something that is not there.
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

// TestWatchClipboardStopsOnContextCancel pins the shutdown path for the one
// platform this suite can actually poll on: cancelling ctx must stop the loop
// within roughly one poll interval, not leave it running past the caller's
// own lifetime.
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
