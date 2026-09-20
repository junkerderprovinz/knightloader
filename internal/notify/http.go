package notify

// One request out, and everything that came back, in a shape the test panel and
// the health row can both be drawn from.
//
// Most of this file is shaped around keeping the secret out of the error
// string. ntfy and Gotify both accept their credential in the query
// (`?token=...`), *url.Error prints the whole URL it was given, and a transport
// error quotes whatever it likes, so without a choke point the token lands in
// the health table, the test panel and the log, all three read by a browser
// that was never shown the header values. redact below follows reconnect's own:
// it replaces the plain text, the QueryEscape form, the PathEscape form and the
// userinfo form, because those disagree with each other. A space is "+" in a
// query and "%20" in a path.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// MaxResponseBody is how much of the far end's answer is kept.
//
// Eight kilobytes is well past every push server's error document and well
// short of a server that answers a notification with a web page. The cap is on
// the read rather than on a slice afterwards, so a peer answering with a
// gigabyte cannot make this process buffer it.
const MaxResponseBody = 8 << 10

// SentRequest is the request as it went out, with every stored secret masked
// again on the way back.
//
// Half of reading a refusal is seeing what was sent: the expanded URL, the
// header names, the body with the placeholders filled in. The header values are
// masked even though the server knows them, because this struct is serialised
// straight to a browser.
type SentRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Attempt is one delivery: what went out, what came back, and how long it took.
type Attempt struct {
	Sent SentRequest `json:"sent"`
	// Status is the HTTP status, 0 when nothing answered. The copy leans on the
	// difference: a 401 is a token to fix, a 0 is a host never reached.
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	DurationMS int64  `json:"durationMs"`
	// ResponseHeaders is what came back, one line per name, multi-valued names
	// joined with ", " as a header line already is on the wire.
	ResponseHeaders map[string]string `json:"responseHeaders"`
	Body            string            `json:"body"`
	// Truncated says the answer was longer than MaxResponseBody, so a panel can
	// say so rather than show a sentence that stops mid-word.
	Truncated bool `json:"truncated"`
	// Err is empty whenever the far end answered at all. A 500 is an answer,
	// and it is in Status.
	Err string `json:"error,omitempty"`
	// Code is a Problem code, so the page can say what to try. Empty for a
	// success and for a transport failure nothing recognised, where Err is the
	// sentence and showing it beats guessing at a category.
	Code string `json:"code,omitempty"`
	// Retryable is about what the worker does next rather than anything a
	// person reads, so it stays off the wire.
	Retryable bool `json:"-"`
}

// OK reports a delivery the far end accepted. 2xx only: a 3xx that reached here
// is a redirect httpx already declined to follow, and a redirect nobody
// followed delivered nothing.
func (a Attempt) OK() bool { return a.Status >= 200 && a.Status < 300 }

// clients is one client per distinct timeout rather than one per Send, since a
// client per send is a connection pool per send holding its idle connections
// open long after the answer is on screen. The timeout is the only thing that
// differs between targets and is clamped to 1..60 seconds, so the map has at
// most sixty entries.
var (
	clientMu sync.Mutex
	clients  = map[time.Duration]*http.Client{}
)

func clientFor(timeout time.Duration) *http.Client {
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[timeout]; ok {
		return c
	}
	// The app's shared outbound policy, with redirects handed back rather than
	// followed.
	//
	// httpx's checkRedirect strips Authorization, Proxy-Authorization, Cookie
	// and Cookie2 on a hop to another origin and cannot strip more, since it
	// has no way to know a header it was handed is a credential. Every value in
	// Target.Headers is one, so X-Gotify-Key, ntfy's token header and a
	// webhook's X-Api-Key would ride along to whoever owns the hop.
	// internal/mediahook closes the same hole the same way.
	//
	// A negative MaxRedirects hands the caller the 3xx, so a redirecting
	// address is reported as one rather than as whatever answered afterwards.
	c := httpx.New(httpx.Options{Timeout: timeout, MaxRedirects: -1})
	clients[timeout] = c
	return c
}

// Send delivers one firing to one target and reports everything about it.
//
// It never returns an error; every failure is a field of the Attempt. The
// worker needs Retryable and Code to decide what to do next and the test route
// needs the whole story on screen, whereas an (Attempt, error) pair would split
// a failed delivery across the two.
func Send(ctx context.Context, t Target, f script.Firing, instanceName string) Attempt {
	method := normalizeMethod(t.Method)
	addr := ExpandURL(t, f, instanceName)
	body := ExpandBody(t, f, instanceName)
	headers := ExpandHeaders(t.Headers, f, instanceName)

	// Built before anything is sent, so a request that never leaves still says
	// what it was going to be. Masked once here; everything below appends.
	att := Attempt{Sent: SentRequest{
		Method:  method,
		URL:     addr,
		Headers: maskValues(headers),
		Body:    body,
	}}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, addr, reader)
	if err != nil {
		// Only reachable from a hand-edited settings.json, since Validate
		// refuses such an address on save. Reported as the row being wrong
		// rather than a network failure, because a retry cannot fix a URL.
		att.Err = redact(t, addr, err.Error())
		att.Code = ProblemBadURL
		return att
	}
	for name, value := range headers {
		// An empty value is what Merge leaves behind when a stored secret could
		// not follow a changed address, and a bare `Authorization:` on the wire
		// is a 401 the operator cannot tell from a wrong token.
		if value == "" {
			continue
		}
		req.Header.Set(name, value)
	}

	started := time.Now()
	resp, err := clientFor(t.Timeout()).Do(req)
	att.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		att.Err = redact(t, addr, err.Error())
		att.Code = classifyError(err)
		// The host was not reached, so nothing about the request itself has
		// been shown to be wrong and another go is worth it.
		att.Retryable = true
		return att
	}
	defer resp.Body.Close()

	att.Status = resp.StatusCode
	att.StatusText = resp.Status
	att.ResponseHeaders = collectHeaders(t, addr, resp.Header)

	// LimitReader with one byte of slack, so "exactly 8192 bytes" is not
	// reported as truncated and 8193 is.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if len(raw) > MaxResponseBody {
		raw = raw[:MaxResponseBody]
		att.Truncated = true
	}
	// The answer goes through the same choke point as the error, since several
	// push servers echo the request back in their error documents and would
	// otherwise print the token into the panel.
	att.Body = redact(t, addr, string(raw))
	att.Code = classifyStatus(resp.StatusCode)
	att.Retryable = retryableStatus(resp.StatusCode)
	return att
}

// maskValues is Redacted's rule applied to an already-expanded header map: a
// value that exists becomes stars, a value that does not stays empty so the
// panel shows which header is waiting to be filled in.
func maskValues(h map[string]string) map[string]string {
	if len(h) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if v != "" {
			v = RedactedValue
		}
		out[k] = v
	}
	return out
}

// collectHeaders flattens the response headers, redacted like everything else
// that came from the far end.
func collectHeaders(t Target, addr string, h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		out[name] = redact(t, addr, strings.Join(values, ", "))
	}
	return out
}

// redact strips this target's secrets out of a string before anybody can log it
// or draw it. There are two, and missing either one is a leak:
//
//   - every header value, in all four spellings, as reconnect does it.
//   - the URL's query. ntfy and Gotify put the credential there and no field
//     here knows which part of it that is, so the whole query goes. The full
//     address is replaced with scheme://host/path first, because *url.Error
//     prints the address as one string and replacing the query alone would
//     leave it half rewritten.
func redact(t Target, addr, s string) string {
	if s == "" {
		return s
	}
	if safe, query := splitQuery(addr); query != "" {
		s = strings.ReplaceAll(s, addr, safe)
		// And the bare query, for a message that quoted only part of the
		// address, such as a *net.OpError naming the host.
		s = strings.ReplaceAll(s, query, RedactedValue)
	}
	for _, v := range t.Headers {
		if v == "" {
			continue
		}
		for _, form := range []string{
			v,
			url.QueryEscape(v),
			url.PathEscape(v),
			strings.TrimPrefix(url.UserPassword("", v).String(), ":"),
		} {
			if form == "" {
				continue
			}
			s = strings.ReplaceAll(s, form, RedactedValue)
		}
	}
	return s
}

// splitQuery returns the address without its query (and without any userinfo)
// plus the query that was cut, or "" when there was none to cut.
func splitQuery(addr string) (safe, query string) {
	u, err := url.Parse(addr)
	if err != nil {
		return addr, ""
	}
	if u.RawQuery == "" && u.User == nil {
		return addr, ""
	}
	query = u.RawQuery
	u.RawQuery = ""
	u.Fragment = ""
	// Userinfo goes with it: "https://user:token@ntfy.example/topic" is the
	// other way a push credential ends up inside an address.
	u.User = nil
	return u.String(), query
}

// classifyStatus turns a status into the code the page has a sentence for.
//
// The 4xx family is split three ways: 401 and 403 are a credential to check,
// 404 is a path that does not exist (with ntfy the topic is part of the
// address, with Matrix the room id is), and everything else is the far end
// having read the request and refused it, for which its own answer body says
// more than a label here could.
func classifyStatus(status int) string {
	switch {
	case status >= 200 && status < 300:
		return ""
	// A 3xx reaches this only because clientFor declines to follow one.
	case status >= 300 && status < 400:
		return ProblemRedirect
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return ProblemAuth
	case status == http.StatusNotFound:
		return ProblemNotFound
	case status == http.StatusRequestTimeout:
		return ProblemTimeout
	case status == http.StatusTooManyRequests:
		return ProblemRateLimited
	case status >= 500:
		return ProblemServer
	default:
		return ProblemRejected
	}
}

// retryableStatus decides whether another go could change the answer.
//
// A refusal is not retried: a wrong token, a topic that does not exist and a
// body the far end will not parse are identical on the second attempt, and
// repeating them turns one mistake into three requests against somebody's
// public instance. Only the three about the far end's own state are repeated.
func retryableStatus(status int) bool {
	switch {
	case status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
		return true
	case status >= 500:
		return true
	default:
		return false
	}
}

// classifyError names a transport failure, or leaves it unnamed.
//
// Unnamed is a real outcome: the health row then shows the redacted sentence
// the transport produced, which for an unusual failure says more than a
// category invented here. Only the four with a specific next step get a code.
func classifyError(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		// A container has its own resolver, so an address the operator's laptop
		// reaches is not one this box necessarily reaches.
		return ProblemDNS
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ProblemTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ProblemTimeout
	}
	var certErr *tls.CertificateVerificationError
	var authorityErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var recordErr tls.RecordHeaderError
	if errors.As(err, &certErr) || errors.As(err, &authorityErr) || errors.As(err, &hostErr) || errors.As(err, &recordErr) {
		return ProblemTLS
	}
	if isRefused(err) {
		return ProblemRefused
	}
	return ""
}

// wsaeconnrefused is Winsock's "connection refused".
//
// A literal, because syscall.ECONNREFUSED is not it on Windows: that platform
// has no such errno, so Go invents one (536870934, an APPLICATION_ERROR offset)
// for the os package while the socket layer reports the real Winsock number,
// and errors.Is between the two is false. A dial at a closed loopback port
// there unwraps to syscall.Errno(10061).
//
// Safe to compare against everywhere, since Linux and the BSDs number their
// errnos in the low hundreds and cannot reach 10061.
const wsaeconnrefused = syscall.Errno(10061)

// isRefused reports a connection the far end actively rejected.
//
// Two errno comparisons and no substring check on the message, because the
// message is localised. On a German Windows the same failure reads "Es konnte
// keine Verbindung hergestellt werden, da der Zielcomputer die Verbindung
// verweigerte", which contains neither "connection refused" nor "actively
// refused".
func isRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaeconnrefused
}
