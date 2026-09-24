package resolver

import "testing"

// Any extension other than a web page counts as a file, so unusual ones (.md,
// .bin, .xyz) do not end up at the media extractor.
func TestDirectMatch(t *testing.T) {
	files := []string{
		"https://example.com/archive.zip",
		"https://example.com/movie.mkv",
		"https://raw.githubusercontent.com/yt-dlp/yt-dlp/master/README.md",
		"https://example.com/firmware.bin",
		"https://example.com/data.xyz",
		"https://example.com/part.r01",
		"https://example.com/deep/path/UPPER.ISO",
	}
	pages := []string{
		"https://youtube.com/watch?v=abc",
		"https://soundcloud.com/artist/track",
		"https://example.com/index.html",
		"https://example.com/download.php",
		"https://example.com/",
		"https://example.com",
		"ftp://example.com/file.zip",
	}
	for _, u := range files {
		if !(Direct{}).Match(u) {
			t.Errorf("Match(%q) = false, want true (it names a file)", u)
		}
	}
	for _, u := range pages {
		if (Direct{}).Match(u) {
			t.Errorf("Match(%q) = true, want false (it is a page or unsupported)", u)
		}
	}
}

// A host Leave names is not claimed even when the path is a file, and the
// fallback leaves it too; every other host is claimed as before.
func TestLeaveKeepsDirectAndTheFallbackOffAHost(t *testing.T) {
	leave := func(host string) bool { return host == "rapidgator.net" }
	direct := Direct{Leave: leave}
	fallback := HTTPFallback{Leave: leave}

	const hoster = "https://rapidgator.net/file/abc/movie.mkv"
	if direct.Match(hoster) || fallback.Match(hoster) {
		t.Errorf("a host Leave names is claimed: direct %v, fallback %v", direct.Match(hoster), fallback.Match(hoster))
	}
	const plain = "https://files.example/movie.mkv"
	if !direct.Match(plain) || !fallback.Match(plain) {
		t.Errorf("an ordinary host is left: direct %v, fallback %v", direct.Match(plain), fallback.Match(plain))
	}
}
