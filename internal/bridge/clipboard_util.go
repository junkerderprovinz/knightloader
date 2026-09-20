package bridge

// The parts of clipboard watching that need no OS clipboard live here, so the
// ordinary test run covers them without the bridgeclipboard tag.

import (
	"crypto/sha256"
	"strings"
)

// clipboardRingSize bounds how many forwarded clipboard hashes are
// remembered, enough to skip repeats and KnightLoader's own "copy link".
const clipboardRingSize = 32

// clipboardRing is a small fixed-size set of recently seen content hashes,
// oldest evicted first.
type clipboardRing struct {
	seen  map[[32]byte]struct{}
	order [][32]byte
}

func newClipboardRing() *clipboardRing {
	return &clipboardRing{seen: make(map[[32]byte]struct{}, clipboardRingSize)}
}

func (r *clipboardRing) seenBefore(h [32]byte) bool {
	_, ok := r.seen[h]
	return ok
}

func (r *clipboardRing) remember(h [32]byte) {
	if r.seenBefore(h) {
		return
	}
	r.order = append(r.order, h)
	r.seen[h] = struct{}{}
	if len(r.order) > clipboardRingSize {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.seen, oldest)
	}
}

func clipboardHash(text string) [32]byte { return sha256.Sum256([]byte(text)) }

// extractClipboardLinks returns the lines of text that are nothing but an
// http(s) or magnet link. Prose that merely contains a link is ignored, since
// nobody reviews what ambient watching queues and a page copied by accident
// must not add every link on it.
func extractClipboardLinks(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		isLink := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "magnet:?")
		if isLink && !strings.ContainsAny(line, " \t") {
			out = append(out, line)
		}
	}
	return out
}
