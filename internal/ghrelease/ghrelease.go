// Package ghrelease fetches a GitHub release and its assets under a host
// allowlist, for the one case this app has of downloading somebody else's
// program and then running it.
//
// WHY THIS EXISTS NEXT TO internal/update, WHICH DOES THE SAME THING.
// internal/update fetches KnightLoader's OWN release: it knows the repository,
// the asset name its own workflow produces, and the checksums file that
// workflow writes. This package fetches a THIRD PARTY's release (yt-dlp), where
// none of those are known ahead of time and the asset list has to be searched
// rather than constructed. Folding the two together is a real and named
// follow-up, deliberately not attempted in the same change that introduces this
// one: internal/update/update_install_test.go pins verifyChecksum and the
// atomic swap exactly where they are, and moving them while other work is in
// the same tree buys nothing that waiting a week does not.
//
// What is NOT copied from internal/update is its HTTP client. That package
// builds a bare &http.Client{} (update.go's fetchLatestRelease and fetchAsset),
// which ignores the operator's proxy entirely and sends no user agent, both
// contrary to internal/httpx's own package doc ("Every request that leaves the
// box is made by a client built here"). A self-hosted box behind a corporate
// proxy is exactly the deployment where "fetch yt-dlp for me" has to work, so
// the transport here comes from httpx.NewTransport and the request carries
// httpx.UserAgent().
//
// The host allowlist and the redirect re-validation ARE copied, because they
// are the integrity boundary. httpx's own CheckRedirect (httpx.go's
// checkRedirect) bounds the hop COUNT and strips credentials across origins; it
// does not and should not know about GitHub's asset hosts. Without a host check
// on every hop the allowlist would be enforced on the first request and not on
// the redirect that actually delivers the bytes, which is the hop that matters:
// GitHub's browser_download_url is always a 302 to its own CDN.
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

// allowedHosts are the only hosts this package will fetch from, whatever a
// release's own JSON says a download URL is. GitHub serves release assets from
// its own CDN hosts and never from an arbitrary redirect target, so pinning
// here is a real (if partial) boundary rather than theatre: it is what stops a
// tampered or spoofed API response from pointing the downloader - whose output
// is then made executable and run - at a host of somebody else's choosing.
//
// The same three internal/update pins, and that is not a coincidence: they are
// GitHub's, not this repository's.
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// apiHost is where the release metadata itself comes from. Separate from
// allowedHosts on purpose: api.github.com serves JSON and never serves an
// asset, and an asset URL that resolved to it would be a response shape nothing
// here expects.
const apiHost = "api.github.com"

// The two ceilings. The metadata call is a few kilobytes and a host that has
// not answered in half a minute has effectively refused; an asset is up to a
// hundred megabytes over whatever link a self-hosted box has, and five minutes
// is internal/update's own figure for the same kind of transfer.
const (
	MetaTimeout  = 30 * time.Second
	AssetTimeout = 5 * time.Minute
)

// maxRedirects bounds the hop chain independently of httpx's own bound, since
// this package supplies its own CheckRedirect and therefore replaces it. Ten is
// httpx.DefaultMaxRedirects; a legitimate GitHub download is one hop.
const maxRedirects = 10

// Asset is one file attached to a release. Size is what the API itself reports
// and is checked against the bytes actually written - a truncated download is
// otherwise indistinguishable from a complete one until something tries to run
// it.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the subset of GitHub's release object anything here reads.
type Release struct {
	Tag     string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Find returns the asset with exactly this name. Exact match, never a prefix or
// a suffix: yt-dlp's release carries "yt-dlp", "yt-dlp.exe", "yt-dlp_linux",
// "yt-dlp_linux.zip" and "yt-dlp_linux_aarch64" side by side, and a loose match
// there picks a different program than the one that was checksummed.
func (r Release) Find(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// client builds the one client shape this package uses: httpx's transport, so
// the operator's proxy and every connection ceiling apply, plus a CheckRedirect
// that re-validates the host on every hop.
//
// httpx.New is deliberately not used. It would install httpx's own
// CheckRedirect, and a client has exactly one - so the host allowlist could
// only be enforced on the first request. The user agent httpx.New would have
// stamped on is set per request instead (see get); GitHub's API refuses a
// request without one outright, so this is not optional politeness.
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

// get issues one validated GET. host is the allowlist the FIRST hop is checked
// against - the API host for metadata, the asset hosts for a download - while
// every later hop is checked against the asset hosts by the CheckRedirect
// above, because a redirect off the API is a redirect to a download.
//
// The caller owns resp.Body and must close it.
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
	// Set here rather than by a transport wrapper, for the reason client()
	// gives: this package cannot use httpx.New at all, and GitHub's API answers
	// 403 to a request with no User-Agent.
	req.Header.Set("User-Agent", httpx.UserAgent())
	resp, err := client(timeout).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// The status line is kept verbatim and handed up rather than folded
		// into "could not check". GitHub says "403 rate limit exceeded" and
		// "404 Not Found" in exactly those words, and those two mean entirely
		// different things to whoever is looking at the page: one is "wait an
		// hour", the other is "this repository moved".
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

// trimOneLine reduces an error body to a single readable line. GitHub's own
// error JSON is one short object; anything longer is not something to paste
// into a settings page.
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

// Latest fetches the "latest release" of owner/repo. GitHub's own endpoint,
// not the releases list: the list needs pagination and a definition of "latest"
// this package would then have to invent, and it would have to decide what a
// prerelease means. The endpoint already answers "the newest non-prerelease,
// non-draft release", which is the only answer anybody wants here.
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
	// Capped rather than read whole: a release with a very long body plus a
	// hundred assets is still well under this, and an unbounded decode from a
	// host that decided to answer forever is the one shape that turns a version
	// check into an out-of-memory kill.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return rel, fmt.Errorf("ghrelease: unreadable release JSON: %w", err)
	}
	if rel.Tag == "" {
		return rel, fmt.Errorf("ghrelease: %s answered a release with no tag", repo)
	}
	return rel, nil
}

// Fetch downloads one asset into w, refusing anything larger than max and
// anything whose length disagrees with what the release itself reported.
//
// The size check is not belt-and-braces next to the checksum that follows it:
// it is what makes the FAILURE readable. A truncated download fails its
// checksum too, but "the digest does not match" reads as tampering, while
// "downloaded 4 MB, the release says 30 MB" reads as the dropped connection it
// almost always is.
func Fetch(ctx context.Context, a Asset, w io.Writer, max int64) (int64, error) {
	resp, err := get(ctx, a.URL, allowedHosts, AssetTimeout, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return copyBounded(w, resp.Body, a, max)
}

// copyBounded is Fetch's whole body once the response is open, split out so the
// three refusals below can be exercised without a live TLS server: the host
// allowlist makes an httptest server unreachable by construction, which is the
// point of the allowlist and would otherwise leave the size rules untested.
func copyBounded(w io.Writer, body io.Reader, a Asset, max int64) (int64, error) {
	// One byte past the cap, so "exactly at the limit" is told apart from "over
	// it" without reading the rest of an endless body.
	n, err := io.Copy(w, io.LimitReader(body, max+1))
	if err != nil {
		return n, fmt.Errorf("ghrelease: downloading %s: %w", a.Name, err)
	}
	if n > max {
		return n, fmt.Errorf("ghrelease: %s is larger than the %d byte cap", a.Name, max)
	}
	if a.Size > 0 && n != a.Size {
		return n, fmt.Errorf("ghrelease: downloaded %d bytes of %s, the release says it is %d - refusing a partial download", n, a.Name, a.Size)
	}
	return n, nil
}

// Bytes downloads a small asset into memory. Used for the checksums file, which
// is a few kilobytes of text and gains nothing from being staged on disk; max
// exists only so a host that decided to answer forever cannot turn this into an
// unbounded read.
func Bytes(ctx context.Context, a Asset, max int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := Fetch(ctx, a, &buf, max); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
