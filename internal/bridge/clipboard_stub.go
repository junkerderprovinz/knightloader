//go:build !bridgeclipboard

package bridge

import (
	"context"
	"log"
)

// WatchClipboard only logs in a build without clipboard support, so
// -bridge-clipboard does not silently do nothing.
func (b *Bridge) WatchClipboard(ctx context.Context) {
	log.Printf("bridge: -bridge-clipboard was set, but this build was not compiled with clipboard support (build with -tags bridgeclipboard)")
}
