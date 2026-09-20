package bridge

import (
	"strconv"
	"testing"
)

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

func TestExtractClipboardLinksIgnoresProse(t *testing.T) {
	text := "I was reading an article at https://news.example/story and thought\n" +
		"you might like it. See also https://news.example/related for more."
	if got := extractClipboardLinks(text); len(got) != 0 {
		t.Errorf("got %v, want none; every link here is inside a sentence", got)
	}
}

func TestExtractClipboardLinksIgnoresPlainText(t *testing.T) {
	if got := extractClipboardLinks("correct horse battery staple"); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

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

	// The values have to be distinct, since remember ignores a repeat.
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
