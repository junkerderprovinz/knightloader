package bridge

// The pieces of ambient clipboard watching that touch no OS API: deciding
// whether a piece of clipboard text is worth forwarding, and remembering what
// was already forwarded so the same content is not resubmitted forever.
//
// Kept apart from clipboard.go (which reads the actual OS clipboard and is
// compiled only with -tags bridgeclipboard) so this logic is covered by the
// ordinary `go test ./...` pass: nothing here needs a real clipboard, a
// display server, or any of xclip/xsel/wl-clipboard/pbpaste to be present, all
// of which a CI runner or a container build has no reason to have.

import (
	"crypto/sha256"
	"strings"
)

// clipboardRingSize bounds how many recently-forwarded clipboard hashes are
// remembered. Small on purpose: its only two jobs are to stop the same
// clipboard content being resubmitted on every poll tick, and to stop
// KnightLoader's own "copy link" writing a link back into the clipboard from
// being read back in as if the user had pasted it from somewhere else — a
// handful of entries covers both without growing unbounded over a bridge that
// runs for weeks.
const clipboardRingSize = 32

// clipboardRing is a small fixed-size set of recently-seen content hashes,
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

// remember records h, evicting the oldest entry once the ring is full.
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

// clipboardHash names one snapshot of clipboard content, for the ring above
// and for the unchanged-since-last-poll check ahead of it.
func clipboardHash(text string) [32]byte { return sha256.Sum256([]byte(text)) }

// extractClipboardLinks pulls http(s) and magnet links out of arbitrary
// clipboard content.
//
// Deliberately narrow rather than internal/linkscan's full prose scanner: a
// whole page or paragraph copied by accident — selecting a bit too much text
// before Ctrl-C is an easy slip — is exactly what must not queue every link
// it happens to contain with nobody watching to notice. So only a line that
// is essentially just a link qualifies; prose that merely contains one does
// not, even though a deliberate paste of the same text into the collector
// would still catch it there. Ambient watching trades recall for safety on
// purpose, because nobody is looking at the result to catch a mistake before
// it queues.
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
