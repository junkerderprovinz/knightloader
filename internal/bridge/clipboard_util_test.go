package bridge

import (
	"strconv"
	"testing"
)

// TestExtractClipboardLinksFindsLinkLines pins the narrow, line-shaped match:
// a link on its own line is caught, a link embedded in a sentence is not,
// blank lines and CRLF are tolerated, and magnet links count too.
func TestExtractClipboardLinksFindsLinkLines(t *testing.T) {
	text := "https://host.example/one.bin\r\n" +
		"\r\n" +
		"Check out this file: https://host.example/two.bin it's great\n" +
		"magnet:?xt=urn:btih:abcdef\n" +
		"   https://host.example/three.bin   \n" +
		"not a link at all"
	got := extractClipboardLinks(text)
	want := []string{
		"https://host.example/one.bin",
		"magnet:?xt=urn:btih:abcdef",
		"https://host.example/three.bin",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestExtractClipboardLinksIgnoresProse guards the whole reason this scanner
// is narrower than internal/linkscan's: a page or a paragraph copied by
// accident must not queue every link buried in it with nobody watching.
func TestExtractClipboardLinksIgnoresProse(t *testing.T) {
	text := "I was reading an article at https://news.example/story and thought\n" +
		"you might like it. See also https://news.example/related for more."
	if got := extractClipboardLinks(text); len(got) != 0 {
		t.Errorf("got %v, want none — every link here is inside a sentence", got)
	}
}

// TestExtractClipboardLinksIgnoresPlainText pins that ordinary copied text
// (a password, a note, a sentence) produces nothing to forward.
func TestExtractClipboardLinksIgnoresPlainText(t *testing.T) {
	if got := extractClipboardLinks("correct horse battery staple"); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// TestClipboardRingDeduplicatesAndEvicts pins the ring's two jobs: an
// already-seen hash is reported seen, and once it holds clipboardRingSize
// entries the oldest one is forgotten to make room for a new one — the ring
// is a recency window, not an ever-growing memory of every clipboard the
// bridge has ever watched.
func TestClipboardRingDeduplicatesAndEvicts(t *testing.T) {
	r := newClipboardRing()
	first := clipboardHash("https://host.example/first")
	if r.seenBefore(first) {
		t.Fatal("a fresh ring already reports the first hash as seen")
	}
	r.remember(first)
	if !r.seenBefore(first) {
		t.Fatal("remember did not make seenBefore report true")
	}

	// Filling the ring past its capacity with distinct entries must evict the
	// oldest one, not the one this test cares about keeping fresh. strconv.Itoa
	// rather than a 26-letter cycle: remember() no-ops on a repeat without
	// moving it to the front, so cycling through fewer than clipboardRingSize
	// distinct values would never actually fill the ring at all.
	for i := 0; i < clipboardRingSize+5; i++ {
		r.remember(clipboardHash(strconv.Itoa(i)))
	}
	if r.seenBefore(first) {
		t.Error("the ring kept the first hash after being filled well past its capacity; it should have evicted it")
	}
	if len(r.order) > clipboardRingSize {
		t.Errorf("ring holds %d entries, want at most %d", len(r.order), clipboardRingSize)
	}
}

// TestClipboardHashIsStableAndDistinguishes is a sanity pin, not a test of
// sha256 itself: the same text must hash the same way twice (or the
// unchanged-since-last-poll check in WatchClipboard would resubmit every
// tick), and different text must hash differently (or two different links
// pasted back to back would collapse into "already seen").
func TestClipboardHashIsStableAndDistinguishes(t *testing.T) {
	a1 := clipboardHash("https://host.example/a")
	a2 := clipboardHash("https://host.example/a")
	b := clipboardHash("https://host.example/b")
	if a1 != a2 {
		t.Error("the same text hashed two different ways")
	}
	if a1 == b {
		t.Error("two different texts hashed the same way")
	}
}
