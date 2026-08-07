package proxycfg

// Finding out whether a connection actually works.
//
// The temptation is to write this as a TCP dial and call it a test. A dial only
// proves something is listening on that port, and the two failures people
// actually hit — a typo in the password, and a proxy that refuses to forward to
// the host you want — both sail straight through it. A green tick that means
// "the port is open" is worse than no button, because the user then stops
// suspecting the proxy.
//
// So each protocol is spoken as far as it can be taken without a third party:
//
//	http, https  a CONNECT, which is what carries the credentials, so a wrong
//	             password comes back as the proxy's own 407.
//	socks5       the greeting and, when there are credentials, the RFC 1929
//	             user/password exchange — both happen before any target is named,
//	             so the password can be checked with nothing else involved.
//	socks4, 4a   nothing happens before a request in this protocol, so without a
//	             target there is only the dial, and the report says exactly that
//	             rather than implying more.
//
// The target is optional and the report always says which of the two questions
// it answered. Naming one is a deliberate act by the user: this is a downloader,
// and a test button that quietly reached out to some fixed third-party address
// to prove connectivity would be the app phoning home on its own initiative.

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Stage is how far the probe got. It is reported for a success as well as for a
// failure, because "reached" and "authenticated" and "forwarded" are three
// different amounts of good news and the interface has to be able to say which.
type Stage string

const (
	// StageRefused: nothing was attempted. The entry cannot be probed at all —
	// it is invalid, or it is a kind that names no endpoint.
	StageRefused Stage = "refused"
	// StageDial: the endpoint was reached.
	StageDial Stage = "dial"
	// StageAuth: the proxy accepted (or refused) the credentials.
	StageAuth Stage = "auth"
	// StageConnect: the proxy was asked to forward to the target and answered.
	StageConnect Stage = "connect"
)

// Report is the answer to one probe. Detail is always set, and always a
// sentence: it is what the page shows, and a bare boolean with no words is the
// failure mode this whole file exists to avoid.
type Report struct {
	OK     bool   `json:"ok"`
	Stage  Stage  `json:"stage"`
	Detail string `json:"detail"`
	// Millis is how long the exchange took, for the case where everything
	// succeeds and the answer the user needs is "yes, but it takes four seconds".
	Millis int64 `json:"millis"`
}

// defaultTargetPort is the port a target named without one is tried on. 443,
// because a proxy that forwards anything at all forwards HTTPS, and a proxy
// configured to allow only port 80 is a proxy no hoster download will survive.
const defaultTargetPort = "443"

// probeTimeout bounds one exchange when the caller's context carries no
// deadline. Long enough for a proxy on another continent to answer twice, short
// enough that a dead one does not hold a browser request open until it gives up.
const probeTimeout = 12 * time.Second

// Probe reaches the connection e describes and reports how far it got.
//
// target is optional and is a host, or a host:port. Empty means "just tell me
// the proxy is there"; naming one also tests that the proxy is willing to
// forward to it, which is the question behind "why does this one hoster fail".
//
// It never returns an error: every way this can go wrong is an answer the user
// asked for, and a caller that had to render both an error and a report would
// have two ways to say the same thing.
func Probe(ctx context.Context, e Entry, target string) Report {
	if err := Validate(e); err != nil {
		// Verbatim, and before anything is dialled. Sanitize would drop this row
		// on the next save, so saying so now is the difference between fixing a
		// typo and watching a row vanish.
		return Report{Stage: StageRefused, Detail: err.Error()}
	}
	target = strings.TrimSpace(target)

	switch e.Kind {
	case KindNone:
		return Report{Stage: StageRefused, Detail: "an inert row names no connection, so there is nothing to reach. " +
			"Give it a type, or use direct if the point is to bypass every proxy for these hosts"}
	case KindDirect:
		if target == "" {
			return Report{Stage: StageRefused, Detail: "a direct row is this machine's own connection, which has no " +
				"endpoint of its own to test. Name a host to check that it can be reached without a proxy"}
		}
		return probeDirect(ctx, target)
	}
	return probeProxy(ctx, e, target)
}

// probeDirect answers the only question a direct row can be asked: can this box
// reach that host at all, with no proxy in the way. It is the same question the
// user is implicitly making a claim about when they exclude their NAS from a
// whole-app proxy, and getting a no here means the exclusion is not the problem.
func probeDirect(ctx context.Context, target string) Report {
	ctx, cancel, start := begin(ctx)
	defer cancel()

	addr := withDefaultPort(target)
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fail(StageDial, start, "%s could not be reached without a proxy: %s", addr, netReason(err))
	}
	_ = c.Close()
	return done(StageConnect, start, "%s answers directly, with no proxy in the way", addr)
}

func probeProxy(ctx context.Context, e Entry, target string) Report {
	ctx, cancel, start := begin(ctx)
	defer cancel()

	endpoint := net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
	c, err := dialProxy(ctx, e, endpoint)
	if err != nil {
		return fail(StageDial, start, "%s could not be reached: %s", endpoint, netReason(err))
	}
	defer func() { _ = c.Close() }()
	// One deadline over the whole exchange rather than one per read: a proxy that
	// answers a byte at a time would otherwise keep the connection alive forever
	// without ever finishing anything.
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}

	switch e.Kind {
	case KindHTTP, KindHTTPS:
		if target == "" {
			return done(StageDial, start, "%s answers. Name a host to test the credentials too — "+
				"an HTTP proxy only asks for them when it is given something to forward", endpoint)
		}
		return httpConnect(c, e, target, start)
	case KindSOCKS5:
		return socks5(c, e, target, start)
	case KindSOCKS4, KindSOCKS4A:
		if target == "" {
			return done(StageDial, start, "%s answers. SOCKS4 exchanges nothing before a request, "+
				"so name a host to test any more than this", endpoint)
		}
		return socks4(ctx, c, e, target, start)
	}
	return Report{Stage: StageRefused, Detail: fmt.Sprintf("%q is not a connection type this can probe", string(e.Kind))}
}

// dialProxy opens the hop to the proxy. An https entry means that hop is itself
// TLS — the proxy speaks HTTP, encrypted, which is a different thing from an
// http proxy that will forward an HTTPS request, and mixing the two up is why
// somebody's working proxy is refused with a garbled first byte.
func dialProxy(ctx context.Context, e Entry, endpoint string) (net.Conn, error) {
	d := &net.Dialer{}
	if e.Kind == KindHTTPS {
		// ServerName is left to be derived from the address, so an entry whose
		// host is an IP literal does not present an invalid SNI name.
		return (&tls.Dialer{NetDialer: d, Config: &tls.Config{MinVersion: tls.VersionTLS12}}).
			DialContext(ctx, "tcp", endpoint)
	}
	return d.DialContext(ctx, "tcp", endpoint)
}

// httpConnect asks the proxy to tunnel to target. This is the exchange the
// credentials ride on, so it is also the only way to find out whether they are
// right.
func httpConnect(c net.Conn, e Entry, target string, start time.Time) Report {
	addr := withDefaultPort(target)
	var req strings.Builder
	fmt.Fprintf(&req, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if e.Username != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(e.Username + ":" + e.Password))
		req.WriteString("Proxy-Authorization: Basic " + cred + "\r\n")
	}
	req.WriteString("\r\n")
	if _, err := io.WriteString(c, req.String()); err != nil {
		return fail(StageConnect, start, "the proxy closed the connection before the request was sent: %s", netReason(err))
	}

	// A short answer with no newline is still an answer, and the one that matters:
	// a TLS server handed a plaintext CONNECT replies with a five-byte alert and
	// hangs up, so treating "read failed" as "said nothing" would throw away the
	// only evidence of what is actually wrong.
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return fail(StageConnect, start, "the proxy answered nothing: %s", netReason(err))
	}
	line = strings.TrimSpace(line)
	code := statusCode(line)
	switch {
	case code == 200:
		if e.Username != "" {
			return done(StageConnect, start, "the proxy accepted the credentials and forwarded to %s", addr)
		}
		return done(StageConnect, start, "the proxy forwarded to %s", addr)
	case code == 407:
		if e.Username == "" {
			return fail(StageAuth, start, "the proxy wants a user name and password (407) and this row has none")
		}
		return fail(StageAuth, start, "the proxy refused the credentials (407)")
	case code == 403:
		return fail(StageConnect, start, "the proxy is reachable and the credentials passed, "+
			"but it will not forward to %s (403)", addr)
	case code == 0:
		return fail(StageConnect, start, "the answer was not HTTP at all (%q) — an https proxy addressed as http "+
			"looks like this", clip(line))
	default:
		return fail(StageConnect, start, "the proxy answered %q", clip(line))
	}
}

// socks5 runs the greeting, the user/password exchange when there is one, and
// the request when a target was named.
func socks5(c net.Conn, e Entry, target string, start time.Time) Report {
	// Offering "no authentication" alongside user/password, in that order of
	// preference, so a proxy that wants neither still answers instead of hanging
	// up on a client that would only authenticate.
	methods := []byte{methodNone}
	if e.Username != "" {
		methods = []byte{methodUserPass, methodNone}
	}
	if _, err := c.Write(append([]byte{5, byte(len(methods))}, methods...)); err != nil {
		return fail(StageAuth, start, "the proxy closed the connection during the SOCKS5 greeting: %s", netReason(err))
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(c, greeting); err != nil {
		return fail(StageAuth, start, "the proxy did not answer the SOCKS5 greeting: %s", netReason(err))
	}
	if greeting[0] != 5 {
		return fail(StageAuth, start, "the proxy answered with SOCKS version %d, not 5 — "+
			"a SOCKS4 proxy set to socks5 looks like this", greeting[0])
	}

	authed := false
	switch greeting[1] {
	case methodNone:
		// Not an error, and authed stays false on purpose: a proxy that asks for
		// nothing has not checked the credentials sitting in this row, so
		// reporting this as "the credentials are fine" would be a green tick for
		// a password nobody has ever looked at.
	case methodUserPass:
		if e.Username == "" {
			return fail(StageAuth, start, "the proxy wants a user name and password and this row has none")
		}
		if r, bad := socks5Auth(c, e, start); bad {
			return r
		}
		authed = true
	case methodNoneAcceptable:
		if e.Username == "" {
			return fail(StageAuth, start, "the proxy rejected an unauthenticated connection: it wants a user name and password")
		}
		return fail(StageAuth, start, "the proxy rejected every way of authenticating that this offered")
	default:
		return fail(StageAuth, start, "the proxy asked for authentication method 0x%02x, which this does not speak", greeting[1])
	}

	if target == "" {
		if authed {
			return done(StageAuth, start, "the proxy accepted the credentials")
		}
		if e.Username != "" {
			return done(StageAuth, start, "the proxy let the connection through without asking for the credentials, "+
				"so nothing here checked them")
		}
		return done(StageAuth, start, "the proxy accepted the connection and asked for no credentials")
	}
	return socks5Connect(c, target, start)
}

const (
	methodNone           = 0x00
	methodUserPass       = 0x02
	methodNoneAcceptable = 0xff
)

// socks5Auth is RFC 1929. It reports the failing case rather than the succeeding
// one, so the caller reads as a straight line.
func socks5Auth(c net.Conn, e Entry, start time.Time) (Report, bool) {
	if len(e.Username) > 255 || len(e.Password) > 255 {
		return fail(StageAuth, start, "SOCKS5 allows 255 bytes each for the user name and the password, "+
			"and this row is over that"), true
	}
	msg := []byte{1, byte(len(e.Username))}
	msg = append(msg, e.Username...)
	msg = append(msg, byte(len(e.Password)))
	msg = append(msg, e.Password...)
	if _, err := c.Write(msg); err != nil {
		return fail(StageAuth, start, "the proxy closed the connection while the credentials were being sent: %s",
			netReason(err)), true
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(c, reply); err != nil {
		return fail(StageAuth, start, "the proxy did not answer the credentials: %s", netReason(err)), true
	}
	if reply[1] != 0 {
		return fail(StageAuth, start, "the proxy refused the credentials"), true
	}
	return Report{}, false
}

func socks5Connect(c net.Conn, target string, start time.Time) Report {
	host, port, err := splitTarget(target)
	if err != nil {
		return Report{Stage: StageRefused, Detail: err.Error()}
	}
	req := []byte{5, 1, 0}
	switch ip := net.ParseIP(host); {
	case ip == nil:
		if len(host) > 255 {
			return Report{Stage: StageRefused, Detail: "that host name is too long for SOCKS5"}
		}
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	case ip.To4() != nil:
		req = append(req, 1)
		req = append(req, ip.To4()...)
	default:
		req = append(req, 4)
		req = append(req, ip.To16()...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		return fail(StageConnect, start, "the proxy closed the connection during the request: %s", netReason(err))
	}
	// Four bytes is the whole verdict; the bound address after it is only of
	// interest to a connection that is about to be used, and this one is about to
	// be hung up on.
	reply := make([]byte, 4)
	if _, err := io.ReadFull(c, reply); err != nil {
		return fail(StageConnect, start, "the proxy did not answer the request: %s", netReason(err))
	}
	if reply[1] != 0 {
		return fail(StageConnect, start, "the proxy would not forward to %s: %s", target, socks5Reply(reply[1]))
	}
	return done(StageConnect, start, "the proxy forwarded to %s", target)
}

func socks5Reply(code byte) string {
	switch code {
	case 1:
		return "general failure"
	case 2:
		return "not allowed by the proxy's rules"
	case 3:
		return "the network is unreachable from the proxy"
	case 4:
		return "the host is unreachable from the proxy"
	case 5:
		return "the connection was refused"
	case 6:
		return "the attempt timed out"
	case 7:
		return "the proxy does not support this kind of request"
	case 8:
		return "the proxy does not support that address type"
	}
	return fmt.Sprintf("reply code %d", code)
}

// socks4 sends the one message this protocol has.
//
// The version difference is the whole reason both kinds exist: socks4 carries a
// four-byte IPv4 address, so the NAME has to be resolved here, by this machine —
// which is precisely what somebody using a proxy to reach a host their own DNS
// cannot see does not want. socks4a sends the name instead and lets the proxy
// resolve it. Saying so in the failure is the difference between switching to
// socks4a and giving up on the proxy.
func socks4(ctx context.Context, c net.Conn, e Entry, target string, start time.Time) Report {
	host, port, err := splitTarget(target)
	if err != nil {
		return Report{Stage: StageRefused, Detail: err.Error()}
	}

	var addr [4]byte
	var trailingHost string
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		copy(addr[:], ip.To4())
	} else if e.Kind == KindSOCKS4A {
		// 0.0.0.x with x non-zero is the SOCKS4a signal that a host name follows.
		addr = [4]byte{0, 0, 0, 1}
		trailingHost = host
	} else {
		ips, rerr := (&net.Resolver{}).LookupIP(ctx, "ip4", host)
		if rerr != nil || len(ips) == 0 {
			return fail(StageConnect, start, "socks4 can only name an IPv4 address and %q did not resolve to one here; "+
				"socks4a sends the name to the proxy and lets it resolve", host)
		}
		copy(addr[:], ips[0].To4())
	}

	req := []byte{4, 1, byte(port >> 8), byte(port)}
	req = append(req, addr[:]...)
	req = append(req, e.Username...) // the user id, which is all SOCKS4 has
	req = append(req, 0)
	if trailingHost != "" {
		req = append(req, trailingHost...)
		req = append(req, 0)
	}
	if _, err := c.Write(req); err != nil {
		return fail(StageConnect, start, "the proxy closed the connection during the request: %s", netReason(err))
	}
	reply := make([]byte, 8)
	if _, err := io.ReadFull(c, reply); err != nil {
		return fail(StageConnect, start, "the proxy did not answer the request: %s", netReason(err))
	}
	switch reply[1] {
	case 0x5a:
		return done(StageConnect, start, "the proxy forwarded to %s", target)
	case 0x5b:
		return fail(StageConnect, start, "the proxy refused to forward to %s", target)
	case 0x5c, 0x5d:
		return fail(StageConnect, start, "the proxy wants to verify the user id over identd, which this cannot answer")
	}
	return fail(StageConnect, start, "the proxy answered with an unknown status 0x%02x", reply[1])
}

// splitTarget reads the host the user typed. A bare host is the common case and
// gets the default port; anything else has to be host:port, because guessing at
// a second colon is how "example.org:8080:whoops" becomes a probe of a host
// nobody named.
func splitTarget(target string) (string, int, error) {
	addr := withDefaultPort(target)
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a host or a host:port", target)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("%q is not a port", rawPort)
	}
	return host, port, nil
}

// withDefaultPort appends the default port to a bare host. A bracketed IPv6
// literal is a host, not a host:port, which is why the count is not enough on
// its own.
func withDefaultPort(target string) string {
	if strings.HasPrefix(target, "[") && strings.HasSuffix(target, "]") {
		return target + ":" + defaultTargetPort
	}
	if _, _, err := net.SplitHostPort(target); err == nil {
		return target
	}
	if net.ParseIP(target) != nil && strings.Contains(target, ":") {
		return "[" + target + "]:" + defaultTargetPort
	}
	return target + ":" + defaultTargetPort
}

// begin bounds the exchange and starts the clock. The caller's deadline wins
// when it has one, so a browser that gave up is not left with a goroutine still
// waiting on a proxy.
func begin(ctx context.Context) (context.Context, context.CancelFunc, time.Time) {
	if _, ok := ctx.Deadline(); !ok {
		c, cancel := context.WithTimeout(ctx, probeTimeout)
		return c, cancel, time.Now()
	}
	c, cancel := context.WithCancel(ctx)
	return c, cancel, time.Now()
}

func done(stage Stage, start time.Time, format string, args ...any) Report {
	return Report{OK: true, Stage: stage, Detail: fmt.Sprintf(format, args...), Millis: since(start)}
}

func fail(stage Stage, start time.Time, format string, args ...any) Report {
	return Report{Stage: stage, Detail: fmt.Sprintf(format, args...), Millis: since(start)}
}

func since(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// netReason turns a dial or read error into something worth reading. Go's own
// text is accurate and unreadable — "dial tcp 10.0.0.5:1080: connectex: No
// connection could be made because the target machine actively refused it" is
// three clauses of transport plumbing wrapped around the one word that matters.
func netReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it did not answer in time"
	case errors.Is(err, context.Canceled):
		return "the test was cancelled"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "it hung up"
	}
	// Matched on the error number rather than on the text, because Windows
	// returns the message in the operating system's language: left to the
	// fallback below, a German box puts "Es konnte keine Verbindung hergestellt
	// werden" in the middle of an English sentence.
	switch {
	case isErrno(err, syscall.ECONNREFUSED, wsaeConnRefused):
		return "nothing is listening on that port"
	case isErrno(err, syscall.ECONNRESET, wsaeConnReset):
		return "it hung up"
	case isErrno(err, syscall.EHOSTUNREACH, wsaeHostUnreach):
		return "that host cannot be reached from here"
	case isErrno(err, syscall.ENETUNREACH, wsaeNetUnreach):
		return "that network cannot be reached from here"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fmt.Sprintf("the name %s does not resolve", dns.Name)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "it did not answer in time"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

// The Winsock numbers for the four socket failures worth naming.
//
// They are written out because the platform constants cannot be relied on
// alone: Go's Windows syscall package defines the POSIX names as synthetic
// APPLICATION_ERROR values that no socket ever returns — syscall.ECONNREFUSED is
// 536870934 there, while a refused connection reports 10061 — so
// errors.Is(err, syscall.ECONNREFUSED) is false for precisely the error it
// names. On every other platform the constant is the right one, so isErrno tries
// both and neither platform needs a build tag.
const (
	wsaeNetUnreach  syscall.Errno = 10051
	wsaeConnReset   syscall.Errno = 10054
	wsaeConnRefused syscall.Errno = 10061
	wsaeHostUnreach syscall.Errno = 10065
)

func isErrno(err error, posix, winsock syscall.Errno) bool {
	if errors.Is(err, posix) {
		return true
	}
	var got syscall.Errno
	return errors.As(err, &got) && got == winsock
}

// statusCode reads the number out of an HTTP status line, or 0 when the line was
// not one — which is itself the answer in the case that matters, an https proxy
// addressed as http answering with a TLS alert.
func statusCode(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return n
}

// clip keeps a proxy's own words in the report without letting a misconfigured
// one paste a kilobyte of HTML into the page.
func clip(s string) string {
	const max = 120
	s = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
