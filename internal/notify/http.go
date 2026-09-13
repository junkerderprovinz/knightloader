package notify

// One request out, and everything that came back, in a shape the test panel and
// the health row can both be drawn from.
//
// THE SECRET REACHES THE ERROR STRING, and that is the failure this file is
// mostly shaped around. ntfy and Gotify both accept their credential in the
// query (`?token=...`), *url.Error prints the whole URL it was given, and a
// transport error quotes whatever it feels like - so without a choke point the
// token lands in the health table, in the test panel and in the log, all three
// of which are read by a browser that this package deliberately never showed
// the header values to. reconnect's own redact (config.go) is the pattern, and
// it is careful for a reason worth repeating: it replaces the plain text AND
// QueryEscape AND PathEscape AND the userinfo form, because those disagree with
// each other - a space is "+" in a query and "%20" in a path.

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
// Eight kilobytes is well past every push server's own error document and well
// short of a server that answers a notification with a web page. The cap is on
// the READ, not on a slice afterwards: a peer that answers with a gigabyte must
// not be able to make this process buffer it.
const MaxResponseBody = 8 << 10

// SentRequest is the request as it went out, with every stored secret masked
// again on the way back.
//
// It exists because the test panel's whole worth is that a refusal can be read
// instead of guessed at, and half of reading a refusal is seeing what was
// actually sent - the expanded URL, the header names, the body after the
// placeholders were filled in. The header VALUES are masked here even though
// the server knows them: this struct is serialised straight to a browser, and
// the browser is the one thing that must not learn them.
type SentRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Attempt is one delivery: what went out, what came back, and how long it took.
type Attempt struct {
	Sent SentRequest `json:"sent"`
	// Status is the HTTP status, 0 when nothing answered at all. The two are a
	// real distinction and the copy leans on it: a 401 is a token to fix, a 0 is
	// a host that was never reached.
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	DurationMS int64  `json:"durationMs"`
	// ResponseHeaders is what came back, one line per name, multi-valued names
	// joined with ", " the way a header line already is on the wire.
	ResponseHeaders map[string]string `json:"responseHeaders"`
	Body            string            `json:"body"`
	// Truncated says the answer was longer than MaxResponseBody, so a panel can
	// say "only the first 8192 characters" rather than showing a sentence that
	// stops mid-word with no explanation.
	Truncated bool `json:"truncated"`
	// Err is empty whenever the far end answered AT ALL, whatever it answered.
	// A 500 is not an error here; it is an answer, and it is in Status.
	Err string `json:"error,omitempty"`
	// Code is a Problem code, so the page can say what to try. Empty for a
	// success, and empty for a transport failure nothing recognised - in which
	// case Err is the sentence and showing it beats guessing at a category.
	Code string `json:"code,omitempty"`
	// Retryable is this package's own decision and is deliberately not on the
	// wire: it is about what the worker does next, not about anything a person
	// reads, and a browser that could see it would be a browser somebody
	// eventually renders it in.
	Retryable bool `json:"-"`
}

// OK reports a delivery the far end accepted. 2xx only: a 3xx that reached here
// is a redirect httpx already declined to follow, and a redirect nobody
// followed delivered nothing.
func (a Attempt) OK() bool { return a.Status >= 200 && a.Status < 300 }

// clients is one client per distinct timeout rather than one per Send.
//
// A client per send is a connection pool per send, each holding its idle
// connections open for a minute and a half after the answer is on screen - the
// identical reason app_feeds.go builds its test client once. Keyed by the
// timeout because that is the only thing that differs between targets, and the
// timeout is clamped to 1..60 seconds, so this map has at most sixty entries
// for the life of the process. Guarded rather than sync.Map: it is written once
// per distinct timeout and read on every delivery, and a plain mutex is easier
// to be sure about than an atomic value nobody has to reason about twice.
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
	// The app's shared outbound policy: the same user agent and the same
	// connection ceilings. MaxRedirects is NEGATIVE, and that is the load-bearing
	// line here.
	//
	// httpx's checkRedirect strips Authorization, Proxy-Authorization, Cookie and
	// Cookie2 on a hop to another origin, and it cannot strip more, because it
	// has no way to know that a header it was handed is a credential. Target.Headers
	// says the opposite in as many words: "Headers is where a token goes. EVERY
	// VALUE HERE IS A SECRET." So X-Gotify-Key, ntfy's own token header and every
	// webhook's X-Api-Key would ride along to whoever owns the hop, and httpx
	// follows ten hops by default. internal/mediahook reached this exact
	// conclusion for the same reason and closed it the same way; this is the
	// sibling half of that decision, and TestSendDoesNotCarryACustomHeaderAcross-
	// ARedirect fails if it is ever reopened.
	//
	// Negative means "hand the caller the 3xx" (httpx.Options.MaxRedirects), so a
	// redirecting address is reported as what it is rather than as whatever the
	// far end answered afterwards. That is also strictly better than what this
	// did before: a target behind a reverse proxy used to answer 401 with nothing
	// on screen to explain it, and now the 3xx names the address to type instead.
	c := httpx.New(httpx.Options{Timeout: timeout, MaxRedirects: -1})
	clients[timeout] = c
	return c
}

// Send delivers one firing to one target and reports everything about it.
//
// It never returns an error: every failure is a field of the Attempt, because
// both callers need the same thing. The worker needs Retryable and Code to
// decide what to do next, and the test route needs the whole story to put on
// screen - and an (Attempt, error) pair would have the interesting half of a
// failed delivery in the error and the rest in a struct the caller then has to
// remember to look at anyway.
func Send(ctx context.Context, t Target, f script.Firing, instanceName string) Attempt {
	method := normalizeMethod(t.Method)
	addr := ExpandURL(t, f, instanceName)
	body := ExpandBody(t, f, instanceName)
	headers := ExpandHeaders(t.Headers, f, instanceName)

	// What is reported is built before anything is sent, so a request that never
	// leaves still says what it was going to be. Masked here, once: everything
	// below only ever appends to this.
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
		// Only reachable from a hand-edited settings.json: Validate refuses an
		// address this would choke on, and the API refuses the save. Reported as
		// the row being wrong rather than as a network failure, because a retry
		// cannot fix a URL.
		att.Err = redact(t, addr, err.Error())
		att.Code = ProblemBadURL
		return att
	}
	for name, value := range headers {
		// An empty value is skipped rather than sent. It is what Merge leaves
		// behind when a stored secret could not follow a changed address, and a
		// bare `Authorization:` on the wire is a 401 the operator cannot tell
		// from a wrong token.
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
		// Every transport failure is worth another go: the host was not reached,
		// so nothing about the request itself has been shown to be wrong.
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
	// The answer goes through the same choke point as the error. A far end that
	// echoes the request back - and several push servers do, in their own error
	// documents - would otherwise print the token into the panel.
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
// or draw it.
//
// TWO SEPARATE SECRETS, and missing either one is a leak:
//
//   - every header VALUE, in all four spellings, exactly as reconnect does it.
//   - the URL's QUERY, which this package never sees the inside of. ntfy and
//     Gotify put the credential there and there is no field here that knows it
//     is a credential, so the whole query goes rather than a value this code
//     cannot identify. The full address is replaced with scheme://host/path
//     first, because *url.Error prints the address as one string and a
//     replacement of the query alone would leave the address half rewritten.
func redact(t Target, addr, s string) string {
	if s == "" {
		return s
	}
	if safe, query := splitQuery(addr); query != "" {
		s = strings.ReplaceAll(s, addr, safe)
		// And the bare query, for a message that quoted only part of the
		// address - a *net.OpError naming the host and a wrapper naming the
		// path, say.
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
// The 4xx family is split three ways on purpose. 401/403 is a credential to
// check, 404 is a path that does not exist (with ntfy the topic is part of the
// address, with Matrix the room id is), and everything else is the far end
// having read the request and refused it - for which the answer body says more
// than any label here could, which is why that code's own sentence points at it.
func classifyStatus(status int) string {
	switch {
	case status >= 200 && status < 300:
		return ""
	// A 3xx only reaches this function because clientFor declines to follow
	// one. Before that it could not happen at all, which is why this arm used
	// to read 200..399 and call the whole range fine: harmless then, and a
	// failure with no cause on screen now.
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

// retryableStatus decides whether another go could plausibly change the answer.
//
// A refusal is NOT retried, and that is the decision worth writing down: a
// wrong token, a topic that does not exist and a body the far end will not
// parse are all identical on the second attempt, and repeating them turns one
// mistake into three requests against somebody's public instance. Only the
// three that are about the far end's own state are repeated.
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
// Unnamed is a real outcome and not a gap: the health row then shows the
// (redacted) sentence the transport produced, which for an unusual failure says
// more than a category invented here. Only the four that have a specific next
// step get a code.
func classifyError(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		// A container has its own resolver, so an address the operator's laptop
		// reaches is not automatically one this box reaches. That is what the
		// copy for this code says, and it is the commonest cause by a distance.
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

// wsaeconnrefused is Winsock's own "connection refused".
//
// It is a literal because syscall.ECONNREFUSED is NOT it on Windows: that
// platform has no such errno, so Go invents one (536870934, an
// APPLICATION_ERROR offset) for the sake of the os package, while the socket
// layer reports the real Winsock number - and errors.Is between the two is
// false. Measured, not assumed: a dial at a closed loopback port on Windows
// unwraps to syscall.Errno(10061) with syscall.ECONNREFUSED at 536870934.
//
// Safe to compare against on every platform: Linux and the BSDs number their
// errnos in the low hundreds, so nothing there can ever be 10061 by accident.
const wsaeconnrefused = syscall.Errno(10061)

// isRefused reports a connection the far end actively rejected.
//
// Two errno comparisons and deliberately no substring check on the message.
// That was the first shape of this function and it was wrong for a reason worth
// leaving written down: the message is LOCALISED. On a German Windows the same
// failure reads "Es konnte keine Verbindung hergestellt werden, da der
// Zielcomputer die Verbindung verweigerte", which contains neither "connection
// refused" nor "actively refused", so the check quietly matched nothing on
// exactly the desktops this app is most used on.
func isRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaeconnrefused
}
