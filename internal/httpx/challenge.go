package httpx

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrBrowserCheck is a site answering with Cloudflare's browser check, which
// only a browser that runs the page's scripts can pass.
var ErrBrowserCheck = errors.New("the site asks for a browser check")

// challengePeek is how much of a refusal's page is read for the challenge's
// marks. They sit in its first few kilobytes.
const challengePeek = 32 << 10

// challengeTransport turns a browser check into ErrBrowserCheck, so a client
// that came for a form or a link fails with that rather than with a status
// or a page it cannot place.
type challengeTransport struct {
	base http.RoundTripper
}

func (t *challengeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || !browserCheck(resp) {
		return resp, err
	}
	resp.Body.Close()
	return nil, ErrBrowserCheck
}

func (t *challengeTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// browserCheck reports whether resp is Cloudflare's challenge. Cloudflare
// marks it with the cf-mitigated header; without that, a 403 or 503 from
// Cloudflare whose page loads the challenge is one too. A page that is not
// is put back for the caller to read.
func browserCheck(resp *http.Response) bool {
	if resp.Header.Get("Cf-Mitigated") == "challenge" {
		return true
	}
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusServiceUnavailable ||
		!strings.EqualFold(resp.Header.Get("Server"), "cloudflare") {
		return false
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, challengePeek))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	return err == nil && (bytes.Contains(head, []byte("_cf_chl_opt")) || bytes.Contains(head, []byte("/cdn-cgi/challenge-platform/")))
}
