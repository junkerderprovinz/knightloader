package hostheaders

// redirect.go: the single most important thing in this package.
//
// A stored header is a credential for ONE origin. A forum that redirects its
// attachment links to a third-party CDN, a Nextcloud behind a reverse proxy
// that bounces to an object store, a seedbox that hands out a signed URL
// somewhere else - all three are ordinary, and all three would hand the
// Authorization header, the session cookie or both to a server the user never
// configured, if the headers simply followed the chain. Any open redirect on a
// configured host becomes a credential giveaway; that is not a hypothetical
// class of bug, it is the reason net/http itself drops Authorization across a
// domain change and the reason internal/httpx does the same on every
// control-plane client in this tree.
//
// The guard is in two halves because a redirect chain can leak in two
// different ways:
//
//   - The chain ENDS somewhere else. Covered by Set.Attach, which returns
//     nothing for a URL off the profile's origin - so the headers handed to
//     the download backend are scoped to the URL that backend will actually
//     fetch.
//   - The chain PASSES THROUGH somewhere else and comes back. Covered by
//     checkRedirect below. This is the half net/http cannot do for us: it
//     rebuilds each hop's headers from the FIRST request, so a header deleted
//     at hop one reappears at hop two, and its own stripping keys on the
//     registered domain, which treats 127.0.0.1:9090 and 127.0.0.1:7070 - two
//     unrelated applications on a self-hosted box - as the same place.
//
// internal/httpx already strips four names across an origin change, and that
// is not enough here. Its list is Authorization, Proxy-Authorization, Cookie
// and Cookie2, which is exactly right for a fixed set of known credentials and
// blind to the whole point of this package: the header a user stores is
// whatever their forum, their seedbox or their Nextcloud asks for, and
// X-Auth-Token, X-Api-Key or a bare Referer would travel the chain untouched.
// checkRedirect strips THE NAMES THIS PROFILE ACTUALLY CARRIES, which is the
// only list that covers them.
//
// # What this cannot do, stated plainly
//
// The download itself is not made here. The engine hands the URL and the
// header map to the gopeed fetcher, which follows its own redirects with its
// own client, out of reach of anything in this tree. Preflight is the answer
// to that: it resolves the chain here, with the guard on, and hands the
// backend the URL the chain ENDED at - so by the time the engine starts there
// is no boundary left for it to cross. What remains is a server that answers
// a second request differently from the first, seconds later; that residual
// case needs the strip inside the engine's own client and is written up in
// this package's report rather than half-built here.

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

// probeTimeout bounds one preflight. It is deliberately short: this runs
// per link while a paste is being staged, and the question being asked - "does
// this URL redirect, and where to" - is answered by the response header alone.
// A host that has not answered in this long has not refused either, and the
// caller's own fallback (hand the URL over unresolved) is a better outcome
// than a paste of two hundred links that takes a minute each.
const probeTimeout = 20 * time.Second

// checkRedirect deletes this profile's header names on any hop that leaves the
// profile's origin, then hands the decision on to next (httpx's own rule,
// which bounds the chain and strips the four standard credential headers).
//
// The comparison is against the PROFILE's origin and not against the previous
// hop or against via[0]. That is what makes it total: wherever the chain has
// been, a request to anything but the one origin these headers were stored for
// carries none of them, and a chain that comes back is allowed to carry them
// again because it is back at the origin they belong to.
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

// Client builds the client one preflight for this profile must be made with:
// the app's own outbound policy (internal/httpx - proxy, user agent, dial and
// header timeouts, redirect ceiling) with this profile's guard in front of the
// redirect rule.
//
// base is shared rather than rebuilt per profile. An *http.Client is a value
// with no lock in it, so copying one to give it a different CheckRedirect
// keeps the connection pool, the user-agent stamping and every ceiling of the
// original - and a transport per profile per link would be a new pool, a new
// set of idle connections and a new set of NAT entries for every paste.
func (s Set) Client(base *http.Client) *http.Client {
	if base == nil {
		base = httpx.New(httpx.Options{Timeout: probeTimeout})
	}
	c := *base
	c.CheckRedirect = s.checkRedirect(base.CheckRedirect)
	// A jar would keep cookies from one host and replay them at the next,
	// which is the same crossing this file exists to prevent, arriving by a
	// different door.
	c.Jar = nil
	return &c
}

// Probe is what one preflight learned.
type Probe struct {
	// URL is where the redirect chain ended, which is the URL the download
	// backend must be handed - see Preflight.
	URL string
	// Headers are this profile's headers if, and only if, URL is on its own
	// origin. It is Set.Attach(URL) and nothing else.
	Headers map[string]string
	// Status is the status line the chain ended on, so the caller can tell a
	// file that is gone from a host that would not say.
	Status int
}

// Preflight follows rawurl's redirect chain with this profile's headers
// attached and reports the URL the download backend should be handed, together
// with the headers that are still this profile's to send AT THAT URL.
//
// It exists because the download backend follows redirects out of our reach
// (see the file comment). Resolving the chain here means the URL handed over
// is the one the bytes actually come from, so the engine has no boundary left
// to carry a credential across.
//
// A GET and not a HEAD, because enough servers answer HEAD with a 405, or send
// a redirect chain for it that they do not use for a GET, that a HEAD-based
// probe reports a different truth from the download that follows it. The
// single-byte Range is what makes that affordable: without it a probe of a
// ten-gigabyte file has the server start sending ten gigabytes, and a paste of
// two hundred links has it start two hundred times.
//
// Errors name the URL and the operation and never the headers, the same rule
// every error in this package follows: an error string travels into a task's
// Err field, into the log ring, and into the diagnostics bundle.
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
	// Set after the stored headers and not before: Range is on the dropped
	// list precisely so a pasted one can never reach a request, and this is
	// the probe's own, which the download itself never sees.
	req.Header.Set("Range", "bytes=0-0")
	resp, err := s.Client(base).Do(req)
	if err != nil {
		// The transport error is deliberately not wrapped in. *url.Error
		// prints the whole URL it was given, query string included, and a
		// signed download link carries its credential there.
		return Probe{}, fmt.Errorf("hostheaders: %s could not be reached", redactURL(rawurl))
	}
	defer func() {
		// Bounded, so a server that ignored the Range and started sending a
		// film cannot hold this goroutine open; the connection goes back to
		// the pool when the drain finishes and is dropped when it does not,
		// which is the ordinary trade either way.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}()
	final := rawurl
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return Probe{URL: final, Headers: s.Attach(final), Status: resp.StatusCode}, nil
}

// redactURL strips a URL's query and userinfo before it goes into a message.
//
// A signed download link carries its credential IN THE QUERY - an AWS
// signature, a Nextcloud share token, a seedbox's expiring key - so a URL
// pasted whole into an error is the same leak this package's headers are
// guarded against, arriving through the one field nobody thinks of as a
// secret.
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
