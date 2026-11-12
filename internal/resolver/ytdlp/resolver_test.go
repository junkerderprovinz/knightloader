package ytdlp

import "testing"

func TestALinkPinnedToYtdlpReachesItOnAHostItLeavesToAFileHoster(t *testing.T) {
	r := Resolver{
		ExcludeHosts: map[string]bool{"archive.org": true},
		Leave:        func(host string) bool { return host == "archive.org" },
	}
	const item = "https://archive.org/details/BigBuckBunny_124"
	if r.Match(item) {
		t.Errorf("Match(%s) = true, want the host left to its file hoster", item)
	}
	if !r.TakesPinned(item) {
		t.Errorf("TakesPinned(%s) = false, want a pinned link taken", item)
	}
	for _, u := range []string{"ftp://archive.org/x", "https://", "not a link"} {
		if r.TakesPinned(u) {
			t.Errorf("TakesPinned(%s) = true, want what is no web address refused", u)
		}
	}
}
