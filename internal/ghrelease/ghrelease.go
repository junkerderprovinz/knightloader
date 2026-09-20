// Package ghrelease fetches a third-party GitHub release (yt-dlp) and its
// assets under a host allowlist, since the downloaded program is then run.
//
// internal/update does the same for KnightLoader's own release, where the
// asset names and checksums file are known in advance; here the asset list
// has to be searched. Unlike internal/update, which builds a bare
// http.Client, this package uses httpx's transport so the operator's proxy
// applies, and sends httpx.UserAgent.
//
// The host check runs on every redirect, not just the first request:
// GitHub's browser_download_url always redirects to its CDN, and that hop is
// the one that delivers the bytes. httpx's own redirect policy knows nothing
// about GitHub's hosts.
package ghrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// allowedHosts are the only hosts assets are fetched from, whatever a release
// JSON says. GitHub serves assets only from these, so a tampered API response
// cannot point the download, which is then executed, anywhere else. They are
// the same hosts internal/update pins.
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// apiHost serves release metadata and never an asset, so it is kept out of
// allowedHosts.
const apiHost = "api.github.com"

// MetaTimeout bounds the few-kilobyte metadata call; AssetTimeout bounds an
// asset of up to a hundred megabytes, matching internal/update.
const (
	MetaTimeout  = 30 * time.Second
	AssetTimeout = 5 * time.Minute
)

// maxRedirects bounds the redirect chain, since this package's CheckRedirect
// replaces httpx's. A real download takes one hop.
const maxRedirects = 10

// Asset is one file attached to a release. Size is what the API reports and
// is checked against the bytes written.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the subset of GitHub's release object this package reads.
type Release struct {
	Tag     string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Find returns the asset with exactly this name. yt-dlp publishes "yt-dlp",
// "yt-dlp_linux", "yt-dlp_linux.zip" and more side by side, and a loose match
// would pick a different program than the one checksummed.
func (r Release) Find(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// client uses httpx's transport, so the proxy and connection limits apply,
// with a CheckRedirect that validates the host on every hop. httpx.New is not
// used because its own CheckRedirect would replace this one.
func client(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: httpx.NewTransport(httpx.Options{}),
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("ghrelease: stopped after %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "https" || !allowedHosts[req.URL.Hostname()] {
				return fmt.Errorf("ghrelease: refusing a redirect to %q, which is not a GitHub release host", req.URL.Hostname())
			}
			return nil
		},
	}
}

// get issues one validated GET. first is the allowlist for the first hop (the
// API host or the asset hosts); later hops are checked against the asset
// hosts, since a redirect off the API is a download. The caller closes
// resp.Body.
func get(ctx context.Context, rawurl string, first map[string]bool, timeout time.Duration, accept string) (*http.Response, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("ghrelease: unreadable URL %q: %w", rawurl, err)
	}
	if u.Scheme != "https" || !first[u.Hostname()] {
		return nil, fmt.Errorf("ghrelease: refusing to fetch from %q, which is not a GitHub host", u.Hostname())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	// GitHub's API answers 403 without a User-Agent.
	req.Header.Set("User-Agent", httpx.UserAgent())
	resp, err := client(timeout).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// GitHub's own wording tells "403 rate limit exceeded" (wait) from
		// "404 Not Found" (the repository moved), so it is passed on.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		msg := trimOneLine(string(body))
		if msg != "" {
			return nil, fmt.Errorf("ghrelease: github answered %s: %s", resp.Status, msg)
		}
		return nil, fmt.Errorf("ghrelease: github answered %s", resp.Status)
	}
	return resp, nil
}

// trimOneLine reduces an error body to one readable line: GitHub's JSON
// message, or the first 200 bytes with line breaks flattened.
func trimOneLine(s string) string {
	var msg struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(s), &msg) == nil && msg.Message != "" {
		return msg.Message
	}
	if len(s) > 200 {
		s = s[:200]
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}

// Latest fetches the latest release of owner/repo from GitHub's own endpoint,
// which already means the newest non-draft, non-prerelease release.
func Latest(ctx context.Context, repo string) (Release, error) {
	var rel Release
	resp, err := get(ctx,
		"https://"+apiHost+"/repos/"+repo+"/releases/latest",
		map[string]bool{apiHost: true},
		MetaTimeout,
		"application/vnd.github+json")
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return rel, fmt.Errorf("ghrelease: unreadable release JSON: %w", err)
	}
	if rel.Tag == "" {
		return rel, fmt.Errorf("ghrelease: %s answered a release with no tag", repo)
	}
	return rel, nil
}

// Fetch downloads one asset into w, refusing anything larger than max or of a
// different length than the release reported. The length check makes a
// dropped connection read as one, rather than as a checksum mismatch that
// looks like tampering.
func Fetch(ctx context.Context, a Asset, w io.Writer, max int64) (int64, error) {
	resp, err := get(ctx, a.URL, allowedHosts, AssetTimeout, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return copyBounded(w, resp.Body, a, max)
}

// copyBounded is Fetch after the response is open. It is separate so the size
// rules can be tested; the host allowlist keeps a test server unreachable.
func copyBounded(w io.Writer, body io.Reader, a Asset, max int64) (int64, error) {
	// One byte past the cap tells "at the limit" from "over it".
	n, err := io.Copy(w, io.LimitReader(body, max+1))
	if err != nil {
		return n, fmt.Errorf("ghrelease: downloading %s: %w", a.Name, err)
	}
	if n > max {
		return n, fmt.Errorf("ghrelease: %s is larger than the %d byte cap", a.Name, max)
	}
	if a.Size > 0 && n != a.Size {
		return n, fmt.Errorf("ghrelease: downloaded %d bytes of %s, the release says it is %d; refusing a partial download", n, a.Name, a.Size)
	}
	return n, nil
}

// Bytes downloads a small asset, such as the checksums file, into memory.
func Bytes(ctx context.Context, a Asset, max int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := Fetch(ctx, a, &buf, max); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
