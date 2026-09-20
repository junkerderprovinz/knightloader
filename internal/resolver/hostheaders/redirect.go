package hostheaders

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// probeTimeout bounds one preflight. It runs per link while a paste is staged
// and only needs the response header; on timeout the caller hands the URL on
// unresolved.
const probeTimeout = 20 * time.Second

// checkRedirect deletes this profile's header names on any hop that leaves the
// profile's origin, then defers to next (httpx's rule, which bounds the chain
// and strips the standard credential headers).
//
// httpx and net/http alone are not enough: they only know Authorization and
// Cookie, while a profile may carry X-Api-Key or any other name, and net/http
// rebuilds each hop's headers from the first request and treats two ports on
// one host as the same place. Comparing against the profile's origin rather
// than the previous hop lets a chain that returns to the origin carry the
// headers again.
func (s Set) checkRedirect(next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if OriginOf(req.URL.String()) != s.Origin {
			for _, h := range s.Headers {
				req.Header.Del(h.Name)
			}
		}
		if next == nil {
			return nil
		}
		return next(req, via)
	}
}

// Client returns a copy of base, the app's outbound client, with this
// profile's redirect guard in front of its own redirect rule. Copying keeps
// the shared connection pool.
func (s Set) Client(base *http.Client) *http.Client {
	if base == nil {
		base = httpx.New(httpx.Options{Timeout: probeTimeout})
	}
	c := *base
	c.CheckRedirect = s.checkRedirect(base.CheckRedirect)
	// A jar would replay one host's cookies at the next.
	c.Jar = nil
	return &c
}

// Probe is what one preflight learned.
type Probe struct {
	// URL is where the redirect chain ended, the URL the download backend
	// is handed.
	URL string
	// Headers is Set.Attach(URL): the profile's headers only if URL is on its
	// origin.
	Headers map[string]string
	// Status is the status the chain ended on, so the caller can tell a file
	// that is gone from a host that would not say.
	Status int
}

// Preflight follows rawurl's redirect chain with this profile's headers and
// reports the final URL together with the headers still allowed there.
//
// The download backend (gopeed) follows redirects with its own client, out of
// reach of checkRedirect, so the chain is resolved here and the backend gets
// a URL with no boundary left to cross. A server that answers the second
// request differently can still escape this.
//
// It sends a GET with a one-byte Range because many servers answer HEAD with
// 405 or redirect it differently. Errors never include the headers.
func (s Set) Preflight(ctx context.Context, base *http.Client, rawurl string) (Probe, error) {
	if s.Origin == "" {
		return Probe{}, errors.New("hostheaders: this profile has no origin")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return Probe{}, fmt.Errorf("hostheaders: %w", err)
	}
	for name, value := range s.Attach(rawurl) {
		req.Header.Set(name, value)
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := s.Client(base).Do(req)
	if err != nil {
		// Not wrapped: *url.Error prints the full URL, and a signed link
		// carries its credential in the query.
		return Probe{}, fmt.Errorf("hostheaders: %s could not be reached", redactURL(rawurl))
	}
	defer func() {
		// Bounded in case the server ignored the Range.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}()
	final := rawurl
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return Probe{URL: final, Headers: s.Attach(final), Status: resp.StatusCode}, nil
}

// redactURL strips a URL's query and userinfo before it goes into a message,
// since signed download links carry their credential in the query.
func redactURL(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "the link"
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}
