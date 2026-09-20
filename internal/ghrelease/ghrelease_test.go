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

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// TestRedirectsStayOnGitHubsOwnHosts: browser_download_url always redirects,
// so the hop that delivers the bytes is never the first one checked.
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
			t.Errorf("%s: followed %s, which is not a GitHub release host", c.name, c.to)
		}
	}

	// This CheckRedirect replaces httpx's, so it bounds the chain itself.
	if err := check(&http.Request{URL: mustURL(t, "https://github.com/x")}, make([]*http.Request, maxRedirects)); err == nil {
		t.Errorf("followed hop %d; the chain is meant to stop at %d", maxRedirects+1, maxRedirects)
	}
}

// TestFetchRefusesAnAssetURLOffGitHub: the first hop is checked before any
// request is made.
func TestFetchRefusesAnAssetURLOffGitHub(t *testing.T) {
	_, err := Fetch(context.Background(), Asset{Name: "yt-dlp", URL: "https://evil.example.com/yt-dlp"}, io.Discard, 1<<20)
	if err == nil {
		t.Fatal("fetched from evil.example.com")
	}
	if !strings.Contains(err.Error(), "not a GitHub host") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

func TestFetchRefusesPlainHTTP(t *testing.T) {
	_, err := Fetch(context.Background(), Asset{Name: "yt-dlp", URL: "http://github.com/yt-dlp"}, io.Discard, 1<<20)
	if err == nil {
		t.Fatal("fetched over plain http")
	}
}

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
		if !strings.Contains(err.Error(), "100") || !strings.Contains(err.Error(), "4096") {
			t.Fatalf("the refusal names neither size: %v", err)
		}
	})

	t.Run("an asset with no reported size is not size-checked", func(t *testing.T) {
		if _, err := copyBounded(io.Discard, strings.NewReader(body), Asset{Name: "yt-dlp"}, 1<<20); err != nil {
			t.Fatalf("an unsized asset was refused: %v", err)
		}
	})
}

func TestGitHubsOwnWordsSurvive(t *testing.T) {
	got := trimOneLine(`{"message":"API rate limit exceeded for 203.0.113.7.","documentation_url":"https://docs.github.com/"}`)
	if got != "API rate limit exceeded for 203.0.113.7." {
		t.Fatalf("the message was mangled: %q", got)
	}
	// A proxy's HTML error page still comes back as one line.
	got = trimOneLine("<html>\n<body>\nblocked by policy\n</body>\n</html>")
	if strings.Contains(got, "\n") {
		t.Fatalf("a multi-line body stayed multi-line: %q", got)
	}
}

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
