//go:build !bridgeclipboard

// The default build: no OS clipboard access compiled in at all. See
// clipboard.go's package comment for why this is a build tag and not just a
// runtime flag — this file is what an ordinary `go build ./cmd/knightloader`
// (the Dockerfile's own build line) actually links, and it imports nothing
// beyond the standard library.
package bridge

import (
	"context"
	"log"
)

// WatchClipboard is a no-op in this build: ambient clipboard watching was not
// compiled in. -bridge-clipboard still starts this goroutine (main.go does
// not need to know which build it is running in), so the flag degrades to a
// clear log line here rather than silently doing nothing.
func (b *Bridge) WatchClipboard(ctx context.Context) {
	log.Printf("bridge: -bridge-clipboard was set, but this build was not compiled with clipboard support (build with -tags bridgeclipboard)")
}
