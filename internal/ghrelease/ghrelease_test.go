package ghrelease

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// mustURL keeps the redirect cases below to one line each.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// The redirect rule is the whole reason this package exists rather than a
// two-line http.Get: GitHub's browser_download_url is ALWAYS a 302, so the hop
// that actually delivers the bytes is never the one the first check saw. A
// CheckRedirect that let an off-allowlist hop through would enforce the
// allowlist on a request that returns no bytes at all.
func TestRedirectsStayOnGitHubsOwnHosts(t *testing.T) {
	check := client(MetaTimeout).CheckRedirect
	if check == nil {
		t.Fatal("the client has no CheckRedirect, so redirects are followed anywhere")
	}

	cases := []struct {
		name string
		to   string
		via  int
		ok   bool
	}{
		{"the CDN hop a real download takes", "https://objects.githubusercontent.com/x", 1, true},
		{"the newer asset host", "https://release-assets.githubusercontent.com/x", 1, true},
		{"github itself", "https://github.com/x", 1, true},
		{"somewhere else entirely", "https://evil.example.com/yt-dlp", 1, false},
		{"a host that merely ends in the right words", "https://githubusercontent.com.evil.example/x", 1, false},
		{"an https downgrade to plain http", "http://github.com/x", 1, false},
	}
	for _, c := range cases {
		via := make([]*http.Request, c.via)
		err := check(&http.Request{URL: mustURL(t, c.to)}, via)
		if c.ok && err != nil {
			t.Errorf("%s: refused %s: %v", c.name, c.to, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: FOLLOWED %s, which is not a GitHub release host", c.name, c.to)
		}
	}

	// The hop chain is bounded independently of the host check, because this
	// package supplies its own CheckRedirect and therefore replaces httpx's.
	if err := check(&http.Request{URL: mustURL(t, "https://github.com/x")}, make([]*http.Request, maxRedirects)); err == nil {
		t.Errorf("followed hop %d; the chain is meant to stop at %d", maxRedirects+1, maxRedirects)
	}
}

// The first hop is checked before the request is made at all, so a release JSON
// that named a download host of its own choosing never reaches the network.
func TestFetchRefusesAnAssetURLOffGitHub(t *testing.T) {
	_, err := Fetch(context.Background(), Asset{Name: "yt-dlp", URL: "https://evil.example.com/yt-dlp"}, io.Discard, 1<<20)
	if err == nil {
		t.Fatal("fetched from evil.example.com")
	}
	if !strings.Contains(err.Error(), "not a GitHub host") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

// A plain-http asset URL is refused for the same reason and by the same check:
// the downloaded bytes are made executable and run, so a hop anyone on the path
// can rewrite is not a hop this takes.
func TestFetchRefusesPlainHTTP(t *testing.T) {
	_, err := Fetch(context.Background(), Asset{Name: "yt-dlp", URL: "http://github.com/yt-dlp"}, io.Discard, 1<<20)
	if err == nil {
		t.Fatal("fetched over plain http")
	}
}

// The three size refusals, exercised where they live - see copyBounded's own
// comment for why they cannot be reached through a test server.
func TestDownloadSizeRules(t *testing.T) {
	body := strings.Repeat("a", 100)

	t.Run("exactly the cap is not over it", func(t *testing.T) {
		var out bytes.Buffer
		n, err := copyBounded(&out, strings.NewReader(body), Asset{Name: "yt-dlp", Size: 100}, 100)
		if err != nil || n != 100 || out.Len() != 100 {
			t.Fatalf("n=%d len=%d err=%v", n, out.Len(), err)
		}
	})

	t.Run("one byte over the cap is refused", func(t *testing.T) {
		_, err := copyBounded(io.Discard, strings.NewReader(body), Asset{Name: "yt-dlp"}, 99)
		if err == nil || !strings.Contains(err.Error(), "cap") {
			t.Fatalf("a body over the cap was accepted: %v", err)
		}
	})

	t.Run("a truncated download names both numbers", func(t *testing.T) {
		_, err := copyBounded(io.Discard, strings.NewReader(body), Asset{Name: "yt-dlp", Size: 4096}, 1<<20)
		if err == nil {
			t.Fatal("a 100 byte answer to a 4096 byte asset was accepted")
		}
		// Both figures, because "the digest does not match" reads as tampering
		// while "downloaded 100 bytes, the release says 4096" reads as the
		// dropped connection it almost always is.
		if !strings.Contains(err.Error(), "100") || !strings.Contains(err.Error(), "4096") {
			t.Fatalf("the refusal names neither size: %v", err)
		}
	})

	t.Run("an asset with no reported size is not size-checked", func(t *testing.T) {
		// GitHub always reports one, but a release JSON that omitted it must
		// not make every download fail against a zero.
		if _, err := copyBounded(io.Discard, strings.NewReader(body), Asset{Name: "yt-dlp"}, 1<<20); err != nil {
			t.Fatalf("an unsized asset was refused: %v", err)
		}
	})
}

// GitHub's refusals are the sentence the settings page shows, so the one thing
// that must survive the trip is its own wording: "API rate limit exceeded" and
// "Not Found" mean entirely different things to whoever is reading the card.
func TestGitHubsOwnWordsSurvive(t *testing.T) {
	got := trimOneLine(`{"message":"API rate limit exceeded for 203.0.113.7.","documentation_url":"https://docs.github.com/"}`)
	if got != "API rate limit exceeded for 203.0.113.7." {
		t.Fatalf("the message was mangled: %q", got)
	}
	// Not JSON at all - a proxy's HTML error page, say. It still has to come
	// back as one line rather than as forty lines of markup in a settings card.
	got = trimOneLine("<html>\n<body>\nblocked by policy\n</body>\n</html>")
	if strings.Contains(got, "\n") {
		t.Fatalf("a multi-line body stayed multi-line: %q", got)
	}
}

// Exact match, never a prefix: one yt-dlp release carries yt-dlp, yt-dlp.exe,
// yt-dlp_linux, yt-dlp_linux.zip and yt-dlp_linux_aarch64 side by side, and a
// loose match picks a different program than the one that was checksummed.
func TestFindIsExact(t *testing.T) {
	rel := Release{Assets: []Asset{
		{Name: "yt-dlp"},
		{Name: "yt-dlp.exe"},
		{Name: "yt-dlp_linux"},
		{Name: "yt-dlp_linux.zip"},
		{Name: "yt-dlp_linux_aarch64"},
	}}
	for _, want := range []string{"yt-dlp", "yt-dlp_linux", "yt-dlp_linux_aarch64"} {
		got, ok := rel.Find(want)
		if !ok || got.Name != want {
			t.Errorf("Find(%q) = %q, %v", want, got.Name, ok)
		}
	}
	if _, ok := rel.Find("yt-dlp_macos"); ok {
		t.Error("Find matched an asset this release does not have")
	}
}
