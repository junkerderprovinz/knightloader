//go:build bridgeclipboard

// Clipboard watching is compiled in only with -tags bridgeclipboard. The
// server and the bridge are one binary, so a runtime flag alone would still
// ship clipboard-reading code in the container image; the container build
// does not pass the tag.
package bridge

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// clipboardPollInterval is a var so a test can shorten it.
var clipboardPollInterval = 1500 * time.Millisecond

// WatchClipboard polls the OS clipboard and forwards anything that looks like
// a hoster link to the remote, as if it had come through Click'n'Load. It
// runs only with -bridge-clipboard and returns when ctx is done.
//
// atotto/clipboard shells out to pbpaste, xclip, xsel or wl-paste, or calls
// user32 on Windows, so the build stays CGO_ENABLED=0.
func (b *Bridge) WatchClipboard(ctx context.Context) {
	if clipboard.Unsupported {
		log.Printf("bridge: clipboard watching was requested, but no clipboard is available on this system (xclip, xsel or wl-clipboard missing?)")
		return
	}
	log.Printf("bridge: watching the clipboard for hoster links")

	ring := newClipboardRing()
	ticker := time.NewTicker(clipboardPollInterval)
	defer ticker.Stop()

	var lastHash [32]byte
	var haveLast bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		text, err := clipboard.ReadAll()
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		h := clipboardHash(text)
		if haveLast && h == lastHash {
			continue
		}
		lastHash, haveLast = h, true
		if ring.seenBefore(h) {
			// Already forwarded, or KnightLoader's own "copy link" putting a
			// known link back on the clipboard.
			continue
		}

		urls := extractClipboardLinks(text)
		if len(urls) == 0 {
			continue
		}
		ring.remember(h)
		b.AddLinksCnL(urls, "Clipboard", nil)
	}
}
