package app

// The hoster icon's own two risky halves: what a host string is allowed to be
// before it becomes a URL and a filename, and what a host is allowed to answer
// with before those bytes are served from this instance's own origin.

import (
	"net/http"
	"testing"
)

func TestNormaliseIconHostAcceptsWhatThePageActuallyHas(t *testing.T) {
	for in, want := range map[string]string{
		"rapidgator.net":                       "rapidgator.net",
		"https://torbox.app/":                  "torbox.app",
		"http://EXAMPLE.COM/where/to/get-key":  "example.com",
		"https://user@host.example.org:8443/x": "host.example.org",
		"  1fichier.com  ":                     "1fichier.com",
	} {
		if got := normaliseIconHost(in); got != want {
			t.Errorf("normaliseIconHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// A host string reaches this from a stored settings file and ends up in both a
// URL and (before the hash) a filename, so everything that is not plainly a
// hostname is refused rather than cleaned up and used anyway.
func TestNormaliseIconHostRefusesEverythingElse(t *testing.T) {
	for _, in := range []string{
		"",
		"localhost",        // no dot: not a public host, and not what this is for
		"../../etc/passwd", // a path, not a host
		".leading.dot.com",
		"trailing.dot.com.",
		"host name.com",       // a space
		"höst.com",            // not ASCII: an IDN belongs punycoded before it gets here
		"host.com\nX-Evil: 1", // a header injection attempt
	} {
		if got := normaliseIconHost(in); got != "" {
			t.Errorf("normaliseIconHost(%q) = %q, want it refused", in, got)
		}
	}
}

// The allowlist is what stands between "a host answered" and "this instance
// serves those bytes from its own origin". SVG is the one worth naming: it is
// an image everywhere else in the world and a scriptable document here.
func TestTheIconAllowlistLeavesOutTheScriptableOnes(t *testing.T) {
	for _, ct := range []string{"image/svg+xml", "text/html", "application/octet-stream", "image/svg"} {
		if _, ok := iconTypes[ct]; ok {
			t.Errorf("iconTypes accepts %q, want it refused", ct)
		}
	}
	for _, ct := range []string{"image/png", "image/x-icon", "image/vnd.microsoft.icon", "image/jpeg", "image/gif", "image/webp"} {
		if _, ok := iconTypes[ct]; !ok {
			t.Errorf("iconTypes refuses %q, want it accepted - that is what a favicon usually is", ct)
		}
	}
	// Every accepted type maps onto a file extension, because that extension
	// becomes part of a filename on disk. An empty one would write a dotfile.
	for ct, ext := range iconTypes {
		if ext == "" {
			t.Errorf("iconTypes[%q] has no extension", ct)
		}
	}
}

// http.DetectContentType is the second half of the check (a host that answers
// application/octet-stream for a real icon is common), so what it reports for
// the shapes this build accepts has to line up with the allowlist's own keys.
func TestSniffingAgreesWithTheAllowlist(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "0123456789012345678901234567890123456789")
	if got := http.DetectContentType(png); got != "image/png" {
		t.Fatalf("DetectContentType(png) = %q, want image/png", got)
	}
	if _, ok := iconTypes[http.DetectContentType(png)]; !ok {
		t.Error("a sniffed PNG is not in the allowlist")
	}
}
