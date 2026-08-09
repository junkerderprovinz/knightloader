// Package httpx is the one outbound HTTP policy this app has. Every request
// that leaves the box - a router being asked for a new address, a page being
// crawled, a debrid API being polled - is made by a client built here, so that
// a proxy, a user agent, a redirect rule or a connection ceiling is one edit
// instead of fifteen scattered http.Client literals.
//
// Clients come from New rather than from one client the package exports.
// A package-level client is a dependency nothing declares: a test cannot give
// one subsystem a stub without every other subsystem silently receiving the
// same stub, and nothing in the wiring shows which of them share a connection
// pool. Sharing is still fine here, it just has to be written down at the call
// site where it can be seen.
//
// This is not the download path. Downloaded bytes are metered through
// internal/netproxy and must not carry a whole-request deadline: a transfer
// that runs for an hour is not a stuck request. Clients from here are for the
// short control-plane calls that happen around a download.
package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// NoTimeout switches the whole-request ceiling off for a caller that streams a
// response rather than reading a small one. The transport ceilings still apply,
// so a peer that accepts the connection and then stops talking is still not
// immortal.
const NoTimeout = time.Duration(-1)

// The ceilings. None of these is tuning: each one exists so that a specific way
// of hanging terminates on its own instead of pinning the goroutine that
// started it for the life of the process.
const (
	// DefaultTimeout bounds a whole request, body included. Everything this
	// package is pointed at answers in a few kilobytes, so a minute is already a
	// server that has effectively refused.
	DefaultTimeout = 60 * time.Second

	// DefaultDialTimeout bounds reaching the host at all. A dropped packet to a
	// dead LAN address otherwise sits in the kernel's SYN retry schedule for
	// well over a minute, which the user reads as the app being broken.
	DefaultDialTimeout = 15 * time.Second

	// DefaultTLSHandshakeTimeout bounds the handshake separately, because a
	// middlebox that completes the TCP connection and then eats the ClientHello
	// is indistinguishable from a slow server without it.
	DefaultTLSHandshakeTimeout = 10 * time.Second

	// DefaultResponseHeaderTimeout is the one that catches the nastiest case: a
	// host that accepts everything and answers nothing. Without it such a peer
	// is only noticed when the overall Timeout expires, and a caller that opted
	// out of that ceiling would wait forever.
	DefaultResponseHeaderTimeout = 30 * time.Second

	// DefaultMaxRedirects bounds the hop chain. An unbounded chain is the
	// cheapest way to send a client in circles; no legitimate endpoint needs
	// anywhere near this many.
	DefaultMaxRedirects = 10
)

// The connection pool. It is bounded rather than generous on purpose: a hoster
// that is being polled by five subsystems at once should see a handful of
// reused connections, not one per request, and a self-hosted box behind a
// consumer router runs out of NAT table entries long before it runs out of RAM.
const (
	idleConnTimeout     = 90 * time.Second
	keepAlive           = 30 * time.Second
	maxIdleConns        = 64
	maxIdleConnsPerHost = 8
	maxConnsPerHost     = 16

	// expectContinueTimeout only matters for the few APIs that send
	// Expect: 100-continue; one second before sending the body anyway.
	expectContinueTimeout = time.Second

	// maxResponseHeaderBytes caps the header block. A megabyte of headers is
	// not a response, it is something trying to make us buffer for it.
	maxResponseHeaderBytes = 1 << 20
)

// Options tunes one client. The zero value is the policy, so a caller with no
// opinion of its own writes New(Options{}) and gets every ceiling above.
type Options struct {
	// UserAgent overrides the app's own identification. Empty means UserAgent();
	// a caller only sets this when a host refuses anything that does not look
	// like a browser.
	UserAgent string

	// Timeout bounds the whole request. Zero means DefaultTimeout, NoTimeout
	// removes the ceiling for a streaming caller.
	Timeout time.Duration

	DialTimeout           time.Duration // zero means DefaultDialTimeout
	TLSHandshakeTimeout   time.Duration // zero means DefaultTLSHandshakeTimeout
	ResponseHeaderTimeout time.Duration // zero means DefaultResponseHeaderTimeout

	// MaxRedirects bounds the hop chain. Zero means DefaultMaxRedirects;
	// negative follows none at all and hands the caller the 3xx itself, which is
	// what a resolver wants when the redirect target *is* the answer.
	MaxRedirects int

	// Proxy resolves the proxy for a request. Nil means the environment, which
	// is what a container operator expects HTTP_PROXY to do; NoProxy pins a
	// client to direct connections.
	Proxy func(*http.Request) (*url.URL, error)

	// Jar keeps cookies for callers that need a session. Nil means cookies are
	// not kept at all, so nothing leaks between two unrelated hosts.
	Jar http.CookieJar
}

// NoProxy is the Proxy for a client that must never be sent through one - a
// loopback call, or a LAN router that an operator's HTTP_PROXY would otherwise
// swallow.
func NoProxy(*http.Request) (*url.URL, error) { return nil, nil }

// UserAgent identifies the app and the build to the far end. A host that
// decides to refuse us can then say who it is refusing, and a server log line
// in a bug report names the version it came from.
func UserAgent() string {
	return "KnightLoader/" + buildinfo.Version + " (+https://github.com/junkerderprovinz/knightloader)"
}

// New builds a client with the whole policy applied: the transport below, the
// redirect rule, the user agent and the request ceiling.
func New(o Options) *http.Client {
	o = o.withDefaults()
	return &http.Client{
		Transport:     &userAgentTransport{base: NewTransport(o), ua: o.UserAgent},
		CheckRedirect: checkRedirect(o.MaxRedirects),
		Jar:           o.Jar,
		Timeout:       clientTimeout(o.Timeout),
	}
}

// NewTransport builds just the transport, for the callers that need to hand one
// to a library instead of a whole client. It carries the ceilings and the pool;
// the user agent and the redirect rule live on the client, because a transport
// never sees a redirect.
func NewTransport(o Options) *http.Transport {
	o = o.withDefaults()
	return &http.Transport{
		Proxy:                  o.Proxy,
		DialContext:            dialer(o).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           maxIdleConns,
		MaxIdleConnsPerHost:    maxIdleConnsPerHost,
		MaxConnsPerHost:        maxConnsPerHost,
		IdleConnTimeout:        idleConnTimeout,
		TLSHandshakeTimeout:    o.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  o.ResponseHeaderTimeout,
		ExpectContinueTimeout:  expectContinueTimeout,
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
	}
}

// dialer is the dial half of the policy. The keep-alive probe is set here as
// well: a NAT box that forgets an idle mapping leaves a connection that looks
// alive from this side and answers nothing, and the pool would keep handing it
// out.
func dialer(o Options) *net.Dialer {
	return &net.Dialer{Timeout: o.DialTimeout, KeepAlive: keepAlive}
}

func (o Options) withDefaults() Options {
	if o.UserAgent == "" {
		o.UserAgent = UserAgent()
	}
	if o.Timeout == 0 {
		o.Timeout = DefaultTimeout
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = DefaultDialTimeout
	}
	if o.TLSHandshakeTimeout <= 0 {
		o.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if o.MaxRedirects == 0 {
		o.MaxRedirects = DefaultMaxRedirects
	}
	if o.Proxy == nil {
		o.Proxy = http.ProxyFromEnvironment
	}
	return o
}

// clientTimeout maps the sentinel onto what net/http means by "no deadline".
func clientTimeout(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

// checkRedirect bounds the hop chain and keeps credentials on the origin they
// were meant for.
func checkRedirect(max int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if max < 0 {
			// Not an error: the caller asked to be handed the 3xx, so the
			// response it already has is the result.
			return http.ErrUseLastResponse
		}
		if len(via) >= max {
			return fmt.Errorf("httpx: stopped after %d redirects", max)
		}
		if len(via) == 0 {
			return nil
		}
		// The comparison is against the *first* request, not the previous hop,
		// because net/http rebuilds each hop's headers from that first request:
		// a header deleted at hop one would be copied in again at hop two, and a
		// chain A -> B -> A would arrive back at A stripped of the credential it
		// is entitled to.
		if !sameOrigin(via[0].URL, req.URL) {
			for _, h := range credentialHeaders {
				req.Header.Del(h)
			}
		}
		return nil
	}
}

// credentialHeaders authenticate us to the host they were set for and mean
// nothing anywhere else - at best they are ignored, at worst an open redirect
// hands the router password or a debrid API key to whoever owns the hop. Go
// drops some of these by itself, but only across a registered domain: to it,
// 127.0.0.1:9090 and 127.0.0.1:7070 are the same place, which on a self-hosted
// box is two unrelated applications.
var credentialHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie", "Cookie2"}

// sameOrigin compares scheme, host and port, which is the scope a credential
// was handed out for. A downgrade from https to http on the same host is a
// different origin on purpose: forwarding a bearer token onto a plaintext hop
// gives it to everyone on the path.
func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		originPort(a) == originPort(b)
}

// originPort fills in the port the scheme implies, so that https://h and
// https://h:443 are recognised as the one origin they are.
func originPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// userAgentTransport stamps the app's identity on requests that did not bring
// their own. It sits on the transport rather than being set per request,
// because the point of this package is that a caller cannot forget.
type userAgentTransport struct {
	base http.RoundTripper
	ua   string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// An explicitly empty User-Agent means "send none" to net/http, so presence
	// of the key is the test, not its value. Overriding that would take away the
	// one way a caller has of staying anonymous.
	if _, set := req.Header["User-Agent"]; set || t.ua == "" {
		return t.base.RoundTrip(req)
	}
	// RoundTrip may not modify the request it is handed: the caller still owns
	// it, and net/http retries with the same object.
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", t.ua)
	return t.base.RoundTrip(clone)
}

// CloseIdleConnections keeps Client.CloseIdleConnections working through the
// wrapper. Without it the method silently does nothing and the pool outlives
// whatever asked for it to be dropped.
func (t *userAgentTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
