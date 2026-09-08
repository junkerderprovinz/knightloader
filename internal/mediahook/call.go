package mediahook

// call.go: the one request, and turning whatever came back into a sentence
// somebody can act on at three in the morning.
//
// THE CLASSIFICATION IS THE FEATURE. "The call did not work" is worth nothing:
// the six things that actually go wrong here - a name that does not resolve from
// inside a container, a port nothing listens on, a self-signed certificate, a
// token the server will not take, a path that is not there, and an HTTP_PROXY
// that swallows a LAN address - all look identical from the settings page and
// have six different fixes. So every failure is folded onto one of the codes
// below, the interface holds the sentence for each, and the raw error travels
// alongside for the log rather than instead of the sentence.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// The failure codes. A closed list, mirrored key for key in the interface's
// string catalogue (settings.mediahook.problem.*), which is why they are
// constants here rather than string literals in a switch: the day one is
// renamed, the compiler finds every use on this side and the check script finds
// the missing key on the other.
const (
	CodeDNS      = "dns"
	CodeRefused  = "refused"
	CodeTimeout  = "timeout"
	CodeTLS      = "tls"
	CodeAuth     = "auth"
	CodeNotFound = "notFound"
	CodeMethod   = "method"
	CodeRedirect = "redirect"
	CodeServer   = "server"
	CodeProxy    = "proxy"
	CodeUnknown  = "unknown"
)

// CallTimeout bounds one call, whole.
//
// Twenty seconds and not httpx.DefaultTimeout's sixty, because this one is
// waited on by a person looking at a Test button, and a library scan that has
// not been ACKNOWLEDGED in twenty seconds is a server that is not going to
// acknowledge it. The scan itself carries on at the far end regardless - what
// this call reports is that the instruction arrived, not that the scan finished.
const CallTimeout = 20 * time.Second

// maxBodyRead is how much of the answer is read before the body is closed.
//
// Something has to be read or the connection cannot go back in the pool, and
// nothing here ever looks at what came back: a library refresh answers 204, or
// an HTML page, or a JSON blob, and none of the three says anything the status
// code has not already said.
const maxBodyRead = 4 << 10

// Result is what one call did. It is what the settings card draws under an
// address and what a log line is built from, and it is deliberately the same
// shape for a test call and for a real one - a test that reported differently
// from the thing it is testing would be worth nothing.
type Result struct {
	At time.Time `json:"at"`
	// Package is the package that armed this call, empty for a test call. When
	// several were folded into one call it is the first of them by name and
	// Packages says how many there were, because "for Foo and 19 others" is what
	// the person actually wants to read.
	Package string `json:"package,omitempty"`
	// Packages counts what was folded into this call. 0 and 1 both mean one
	// package, which is why it is omitempty: the interface only draws the
	// "several packages" line when there were several.
	Packages int  `json:"packages,omitempty"`
	Test     bool `json:"test,omitempty"`
	// Status is the HTTP status, 0 when the call never got an answer at all.
	Status     int   `json:"status,omitempty"`
	DurationMS int64 `json:"durationMs"`
	OK         bool  `json:"ok"`
	// Code keys the sentence the interface shows. Empty on success.
	Code string `json:"code,omitempty"`
	// Params fills the placeholders in that sentence. Numbers stay numbers so the
	// interface can format them in the reader's own locale.
	Params map[string]any `json:"params,omitempty"`
	// Error is the raw sentence, for the log and for the "unknown" case. It is
	// never the whole story on its own, which is the entire reason Code exists.
	Error string `json:"error,omitempty"`
}

// NewClient builds the client every call in this package goes out on.
//
// MaxRedirects is NEGATIVE and that is the load-bearing line in this file.
// httpx's own credential list is Authorization, Proxy-Authorization, Cookie and
// Cookie2 (httpx.credentialHeaders); X-Emby-Token, X-Plex-Token and X-Api-Key
// are not on it and cannot be, because httpx has no way to know that a header it
// was handed is a credential. So a media server behind a reverse proxy that 302s
// to a login page on another host would be handed the token, and httpx follows
// ten hops by default. Negative means "hand the caller the 3xx" (see
// httpx.Options.MaxRedirects), and the 3xx is then reported as what it is: a
// configuration answer, with the address it points at as the thing to type in
// instead.
//
// The proxy is left as the environment's, deliberately, so that an operator's
// NO_PROXY still works - see classifyError's proxy branch for the other half of
// that decision.
func NewClient() *http.Client {
	return httpx.New(httpx.Options{Timeout: CallTimeout, MaxRedirects: -1})
}

// Call makes one request and reports what happened. It never returns an error:
// every way this can fail is a Result, because every one of them is something
// the person has to be shown rather than something a caller can handle.
func Call(ctx context.Context, c *http.Client, h Hook, headerValue string) Result {
	res := Result{At: time.Now()}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, methodOf(h), strings.TrimSpace(h.URL), nil)
	if err != nil {
		// Only reachable for an address Validate would have refused, which is
		// still worth answering rather than ignoring: a settings.json edited by
		// hand reaches here through the runner. The parse error itself is not
		// quoted - it names a byte position in a string that may carry a token.
		res.DurationMS = time.Since(started).Milliseconds()
		res.Code = CodeUnknown
		res.Error = "the address could not be turned into a request"
		return res
	}
	if name := strings.TrimSpace(h.HeaderName); name != "" && headerValue != "" {
		req.Header.Set(name, headerValue)
	}
	if c == nil {
		c = NewClient()
	}
	resp, err := c.Do(req)
	res.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		res.Code, res.Params, res.Error = classifyError(err, req)
		return res
	}
	// Read a little and close, so the connection can go back in the pool.
	// Nothing here looks at the body; see maxBodyRead.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyRead))
	_ = resp.Body.Close()
	res.Status = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res.OK = true
		return res
	}
	res.Code, res.Params, res.Error = classifyStatus(resp.StatusCode, h)
	return res
}

// methodOf is the method actually sent. Anything this build does not send
// becomes a GET rather than being refused here: Validate and Sanitize have both
// already had their say, and a request that is never made reports nothing at
// all.
func methodOf(h Hook) string {
	if strings.EqualFold(strings.TrimSpace(h.Method), MethodPost) {
		return MethodPost
	}
	return MethodGet
}

// classifyStatus folds an answer that is not a 2xx onto one code.
//
// The four that get their own sentence are the four with different fixes: the
// token (401/403), the path (404), the method (405) and the redirect (3xx). 5xx
// is the far end's own fault and says so, which matters because the natural
// assumption on any red mark here is that the address is wrong.
func classifyStatus(status int, h Hook) (string, map[string]any, string) {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return CodeAuth, map[string]any{"status": status}, ""
	case status == http.StatusNotFound:
		return CodeNotFound, nil, ""
	case status == http.StatusMethodNotAllowed:
		return CodeMethod, map[string]any{"method": methodOf(h)}, ""
	case status >= 300 && status < 400:
		return CodeRedirect, map[string]any{"status": status}, ""
	case status >= 500:
		return CodeServer, map[string]any{"status": status}, ""
	default:
		// Every other 4xx. There is no useful sentence to write for a 429 or a
		// 409 from a library scanner, and inventing one per status would be a
		// translation table that is wrong more often than it is right. The status
		// itself is what the person takes to the media server's own log.
		return CodeUnknown, nil, "HTTP " + strconv.Itoa(status)
	}
}

// classifyError folds a transport failure onto one code.
//
// The proxy question is asked around every branch and it is the one nobody
// expects. httpx leaves Proxy at http.ProxyFromEnvironment, which is what an
// operator setting HTTP_PROXY for hoster traffic wants for hoster traffic - and
// it means the call to 192.168.1.10:8096 is handed to that proxy too, which
// refuses it or times out. What comes back reads exactly like a dead media
// server, and the fix (put the host in NO_PROXY) is one nobody arrives at from
// "connection refused". So when the environment says this request would be
// proxied and the call failed before it got an answer, the sentence names the
// proxy instead.
//
// DNS and TLS are the two exceptions to that, and deliberately: a name that does
// not resolve failed before any proxy was involved, and a certificate that will
// not verify is a certificate whether or not a proxy carried the bytes.
func classifyError(err error, req *http.Request) (string, map[string]any, string) {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return CodeDNS, nil, err.Error()
	}
	if isTLS(err) {
		return CodeTLS, nil, err.Error()
	}
	if proxied(req) {
		return CodeProxy, nil, err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return CodeTimeout, map[string]any{"seconds": int(CallTimeout / time.Second)}, err.Error()
	}
	if isRefused(err) {
		return CodeRefused, nil, err.Error()
	}
	return CodeUnknown, nil, err.Error()
}

// proxied reports whether the environment would send this request through a
// proxy. Asked of net/http's own resolver rather than of the error text, because
// the error a proxy failure produces is the error the far end's own failure
// produces - there is nothing in it to read.
//
// A malformed HTTP_PROXY answers an error here, which is itself a proxy problem
// and is treated as one: the request did not go where the person thinks it went.
func proxied(req *http.Request) bool {
	if req == nil {
		return false
	}
	u, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return true
	}
	return u != nil
}

// isTimeout covers both shapes a deadline arrives in: net's own Timeout()
// interface, and the "Client.Timeout exceeded" error net/http builds when the
// whole-request ceiling fires, which does not wrap context.DeadlineExceeded on
// every path.
func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// isTLS covers the handshake failures worth telling apart from a plain refusal:
// a self-signed certificate (the overwhelmingly common one on a home server), a
// certificate issued for another name, and the answer a plain HTTP server gives
// when something speaks TLS at it.
func isTLS(err error) bool {
	var rec tls.RecordHeaderError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	return errors.As(err, &rec) ||
		errors.As(err, &unknown) ||
		errors.As(err, &hostname) ||
		errors.As(err, &invalid) ||
		errors.As(err, &verify)
}

// wsaeconnrefused is Winsock's own "connection refused".
//
// It is written out as a number because Go does NOT fold it onto
// syscall.ECONNREFUSED: on Windows the two are different Errno values and
// errors.Is against ECONNREFUSED answers false for a refused connection, which
// is exactly what the test for this found. Reading the text instead is not an
// option either - Windows writes that sentence in the machine's own language,
// and on a German box it does not contain the word "refused" at all. The
// constant is inert on every other platform, where no errno is 10061.
const wsaeconnrefused = syscall.Errno(10061)

// isRefused covers "nothing is listening there".
//
// It gets its own code because the fix is a PORT, while the code it would
// otherwise fall into (dns, or worse, unknown) sends somebody off to check a
// name that was never wrong.
//
// The text check at the end is the last resort for an error that reached here
// with the syscall wrapped away, and it is deliberately last: it only ever fires
// on an English-language error string, which is the one case the two checks
// above cannot already answer.
func isRefused(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.ECONNREFUSED || errno == wsaeconnrefused) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "refused")
}

// urlHost is one address's host[:port], or "". The runner's log lines are built
// from it so that a dropped call names where it was going without printing the
// whole address, path and query included - Plex's own documented refresh call
// carries its token in that query.
func urlHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Host
}
