//go:build bridgeclipboard

// Ambient clipboard watching, compiled in only with -tags bridgeclipboard.
//
// The tag is the point, not a style choice: cmd/knightloader is one binary
// serving both the ordinary server and, behind the -bridge flag, this bridge
// package — main.go imports internal/bridge unconditionally, so anything this
// package imports at package scope ships in the container image's binary too,
// whether or not -bridge is ever passed at runtime. A container has no
// legitimate access to a user's OS clipboard at all, and "it's gated behind a
// flag" is a runtime property that says nothing about what shipped in the
// image — an audit of the binary would still find clipboard-reading code
// inside a headless server. The Dockerfile's plain `go build ./cmd/knightloader`
// carries none of this file; only an explicit -tags bridgeclipboard build
// does, which nothing in this repo's own container build passes.
package bridge

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// clipboardPollInterval paces how often the OS clipboard is read. A var, not
// a const, so a test can shrink it rather than sit through it for real.
var clipboardPollInterval = 1500 * time.Millisecond

// WatchClipboard polls the OS clipboard and forwards anything that looks like
// a hoster link to the remote, exactly as if it had been pasted into
// Click'n'Load. It returns when ctx is done.
//
// Off by default: nothing calls this unless the bridge was started with
// -bridge-clipboard, because a background poller reading the clipboard is
// exactly the kind of thing that must be asked for, not assumed.
//
// atotto/clipboard shells out to pbpaste/xclip/xsel/wl-paste, or on Windows
// calls user32 through syscall — never cgo, which is why the server module
// can build CGO_ENABLED=0 static regardless of whether this file is compiled
// into a given binary at all.
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
			continue // unchanged since the last tick
		}
		lastHash, haveLast = h, true
		if ring.seenBefore(h) {
			// Either this exact content was already forwarded (the user
			// re-copied the same link), or it is KnightLoader's own "copy
			// link" writing a link this instance already has back into the
			// clipboard — either way, re-adding it is not the point of
			// watching.
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
