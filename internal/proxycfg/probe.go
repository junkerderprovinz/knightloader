package proxycfg

// Finding out whether a connection works. A TCP dial only proves the port is
// open; a wrong password or a proxy that will not forward to the wanted host
// both pass it. So each protocol is spoken as far as it goes without a third
// party:
//
//	http, https:  a CONNECT, which carries the credentials, so a wrong
//	              password comes back as the proxy's own 407.
//	socks5:       the greeting and, with credentials, the RFC 1929 exchange,
//	              both before any target is named.
//	socks4, 4a:   nothing is exchanged before a request, so without a target
//	              there is only the dial, and the report says so.
//
// The target is optional and the report says which question it answered. It
// is never a fixed third-party address, so a test button does not make the
// app phone home.

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

// Stage is how far the probe got, reported on success too, since reached,
// authenticated and forwarded are different amounts of good news.
type Stage string

const (
	// StageRefused: nothing was attempted, because the entry is invalid or
	// names no endpoint.
	StageRefused Stage = "refused"
	// StageDial: the endpoint was reached.
	StageDial Stage = "dial"
	// StageAuth: the proxy accepted or refused the credentials.
	StageAuth Stage = "auth"
	// StageConnect: the proxy was asked to forward to the target and answered.
	StageConnect Stage = "connect"
)

// Report is the answer to one probe. Detail is always a sentence the page can
// show.
type Report struct {
	OK     bool   `json:"ok"`
	Stage  Stage  `json:"stage"`
	Detail string `json:"detail"`
	// Millis is how long the exchange took, for a proxy that works but slowly.
	Millis int64 `json:"millis"`
}

// defaultTargetPort is used for a target named without a port. A proxy that
// forwards anything forwards HTTPS, and hoster downloads need it.
const defaultTargetPort = "443"

// probeTimeout bounds one exchange when the caller's context has no deadline:
// long enough for a distant proxy, short enough not to hold a browser request.
const probeTimeout = 12 * time.Second

// Probe reaches the connection e describes and reports how far it got.
//
// target is optional, a host or host:port. Naming one also tests that the
// proxy will forward to it. Probe returns no error: every failure is part of
// the answer the user asked for.
func Probe(ctx context.Context, e Entry, target string) Report {
	if err := Validate(e); err != nil {
		// Said now, since Sanitize would drop this row on the next save.
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

// probeDirect answers the one question a direct row can be asked: can this box
// reach the host without a proxy.
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
	// One deadline over the whole exchange, so a proxy answering a byte at a
	// time cannot keep it open forever.
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}

	switch e.Kind {
	case KindHTTP, KindHTTPS:
		if target == "" {
			return done(StageDial, start, "%s answers. Name a host to test the credentials too; "+
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

// dialProxy opens the hop to the proxy. For an https entry that hop is itself
// TLS, which differs from an http proxy forwarding an HTTPS request.
func dialProxy(ctx context.Context, e Entry, endpoint string) (net.Conn, error) {
	d := &net.Dialer{}
	if e.Kind == KindHTTPS {
		// ServerName is derived from the address, so an IP literal does not
		// send an invalid SNI name.
		return (&tls.Dialer{NetDialer: d, Config: &tls.Config{MinVersion: tls.VersionTLS12}}).
			DialContext(ctx, "tcp", endpoint)
	}
	return d.DialContext(ctx, "tcp", endpoint)
}

// httpConnect asks the proxy to tunnel to target. The credentials ride on this
// request, so it is the only way to check them.
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

	// A short answer without a newline still counts: a TLS server given a
	// plaintext CONNECT replies with a five-byte alert and hangs up.
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
		return fail(StageConnect, start, "the answer was not HTTP at all (%q); an https proxy addressed as http "+
			"looks like this", clip(line))
	default:
		return fail(StageConnect, start, "the proxy answered %q", clip(line))
	}
}

// socks5 runs the greeting, the user/password exchange when there is one, and
// the request when a target was named.
func socks5(c net.Conn, e Entry, target string, start time.Time) Report {
	// "No authentication" is offered alongside user/password, so a proxy that
	// wants neither still answers.
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
		return fail(StageAuth, start, "the proxy answered with SOCKS version %d, not 5; "+
			"a SOCKS4 proxy set to socks5 looks like this", greeting[0])
	}

	authed := false
	switch greeting[1] {
	case methodNone:
		// authed stays false: the proxy never checked the row's credentials.
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

// socks5Auth is RFC 1929. It reports whether it failed, with the report for
// the failure.
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
	// The first four bytes hold the verdict; the bound address after them only
	// matters to a connection that will be used.
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

// socks4 sends the protocol's one message. SOCKS4 carries an IPv4 address, so
// the name is resolved on this machine; SOCKS4a sends the name for the proxy
// to resolve, and the failure message points that out.
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

// splitTarget reads the host the user typed. A bare host gets the default
// port; anything else must be host:port, so "example.org:8080:whoops" is
// refused rather than guessed at.
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

// withDefaultPort appends the default port to a bare host, including a
// bracketed or bare IPv6 literal.
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
// when it has one, so a browser that gave up does not leave a probe running.
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

// netReason turns a dial or read error into a short reason. Go's own text
// wraps the one relevant word in several clauses of transport detail.
func netReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it did not answer in time"
	case errors.Is(err, context.Canceled):
		return "the test was cancelled"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "it hung up"
	}
	// Matched on the error number, because Windows returns the message in
	// the system language and would put German into an English sentence.
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

// The Winsock numbers for the four socket failures worth naming. Go's Windows
// syscall package defines the POSIX names as synthetic values no socket
// returns (syscall.ECONNREFUSED is 536870934 there, a refused connection
// reports 10061), so isErrno checks both and needs no build tag.
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

// statusCode reads the number out of an HTTP status line, or 0 when the line
// is not one, as with an https proxy answering a plain request with a TLS
// alert.
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

// clip keeps a proxy's own words in the report without pasting a kilobyte of
// HTML into the page.
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
