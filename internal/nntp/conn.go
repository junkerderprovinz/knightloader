// Package nntp fetches articles from Usenet servers: a pool of connections per
// server, TLS, the AUTHINFO login, the BODY and STAT commands, and the order
// in which several servers are asked for one article (see Client.Fetch).
package nntp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// Server is one Usenet server and how it is used.
type Server struct {
	// ID tells two servers on the same host apart.
	ID       string
	Host     string
	Port     int
	TLS      bool
	Username string
	Password string
	// Connections is how many the provider allows at once.
	Connections int
	// Level is 0 for the servers asked first. A server on level 1 or above is
	// asked only for what every server below it lacks, the way a block
	// account fills the gaps of the main one.
	Level int
	// Optional servers are passed over while they cannot be reached, as long
	// as another server answers. A server that is not optional holds an
	// article back instead: it might have it, so the article is asked for
	// again once the server is back.
	Optional bool
	// RetentionDays is how far back the server keeps articles, 0 for no
	// limit. An older article is not asked of it.
	RetentionDays int
	// TLSConfig replaces the default for a server whose certificate is not
	// signed by a public authority, such as a test server's.
	TLSConfig *tls.Config
}

// Addr is host:port, with the usual port for plain or TLS when none is set.
func (s Server) Addr() string {
	port := s.Port
	if port <= 0 {
		port = 119
		if s.TLS {
			port = 563
		}
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

func (s Server) name() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Addr()
}

// ErrNoArticle is a server answering that it does not have an article.
var ErrNoArticle = errors.New("nntp: the server does not have this article")

// ErrAuth is a server refusing the username or password, or asking for a
// login none was given for.
var ErrAuth = errors.New("nntp: the server refused the login")

// ProtoError is an answer the client did not expect, kept with its code.
type ProtoError struct {
	Code int
	Msg  string
}

func (e *ProtoError) Error() string { return fmt.Sprintf("nntp: server answered %d %s", e.Code, e.Msg) }

const (
	dialTimeout = 20 * time.Second
	// idleTimeout is how long a server may send nothing while it owes an
	// answer. It bounds the silence rather than the whole article, which under
	// a low speed limit can take minutes.
	idleTimeout = 60 * time.Second
	// maxArticle bounds one article's body in memory. Posted articles run to a
	// few MiB, and a server sending far more would otherwise fill the memory,
	// once per connection.
	maxArticle = 16 << 20
)

// conn is one logged-in connection.
type conn struct {
	nc   *idleConn
	tp   *textproto.Conn
	idle time.Time
}

// idleConn moves the read deadline on with every read. cancelled is set once
// the caller has given up, so a read that starts afterwards fails at once
// rather than waiting out a fresh deadline.
type idleConn struct {
	net.Conn
	cancelled atomic.Bool
}

func (c *idleConn) Read(p []byte) (int, error) {
	if c.cancelled.Load() {
		return 0, os.ErrDeadlineExceeded
	}
	_ = c.SetReadDeadline(time.Now().Add(idleTimeout))
	// Checked again: a cancellation between the first check and the new
	// deadline would otherwise be overwritten by it.
	if c.cancelled.Load() {
		_ = c.SetReadDeadline(time.Unix(1, 0))
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(idleTimeout))
	return c.Conn.Write(p)
}

// dial connects to s, reads its greeting and logs in when s has a username.
func dial(ctx context.Context, s Server) (*conn, error) {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	var raw net.Conn
	var err error
	if s.TLS {
		cfg := s.TLSConfig.Clone()
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		if cfg.ServerName == "" {
			cfg.ServerName = s.Host
		}
		raw, err = (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", s.Addr())
	} else {
		raw, err = d.DialContext(ctx, "tcp", s.Addr())
	}
	if err != nil {
		return nil, err
	}
	nc := &idleConn{Conn: raw}
	c := &conn{nc: nc, tp: textproto.NewConn(nc)}
	if err := c.greet(ctx, s); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *conn) greet(ctx context.Context, s Server) error {
	defer c.watch(ctx)()
	code, msg, err := c.tp.ReadCodeLine(0)
	if err != nil {
		return err
	}
	// 201 is a server that allows no posting, which reading does not need.
	if code != 200 && code != 201 {
		return &ProtoError{code, msg}
	}
	if s.Username == "" {
		return nil
	}
	code, msg, err = c.cmd("AUTHINFO USER %s", s.Username)
	if err != nil {
		return err
	}
	if code == 381 {
		code, msg, err = c.cmd("AUTHINFO PASS %s", s.Password)
		if err != nil {
			return err
		}
	}
	switch code {
	case 281:
		return nil
	case 481, 482, 502:
		return fmt.Errorf("%w: %d %s", ErrAuth, code, msg)
	}
	return &ProtoError{code, msg}
}

// watch cuts the next exchange short when ctx ends, and returns the function
// that stops watching.
func (c *conn) watch(ctx context.Context) func() {
	stop := context.AfterFunc(ctx, func() {
		c.nc.cancelled.Store(true)
		_ = c.nc.SetDeadline(time.Unix(1, 0))
	})
	return func() { stop() }
}

func (c *conn) cmd(format string, args ...any) (int, string, error) {
	if err := c.tp.PrintfLine(format, args...); err != nil {
		return 0, "", err
	}
	return c.tp.ReadCodeLine(0)
}

// Copier moves an article from the connection into memory. The app's speed
// limiter is one, so articles count against the limit with every other
// download.
type Copier interface {
	Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error)
}

type plainCopy struct{}

func (plainCopy) Copy(_ context.Context, dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

// body fetches one article's body, dot-unstuffed and with LF line ends.
func (c *conn) body(ctx context.Context, id string, cp Copier) ([]byte, error) {
	defer c.watch(ctx)()
	code, msg, err := c.cmd("BODY <%s>", id)
	if err != nil {
		return nil, err
	}
	if err := articleAnswer(code, msg, 222); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if _, err := cp.Copy(ctx, &b, io.LimitReader(c.tp.DotReader(), maxArticle+1)); err != nil {
		return nil, err
	}
	// The rest stays unread, so the connection is closed with this error.
	if b.Len() > maxArticle {
		return nil, fmt.Errorf("%w: article %s is larger than %d MiB", yenc.ErrFormat, id, maxArticle>>20)
	}
	return b.Bytes(), nil
}

// stat asks whether the server has an article without fetching it.
func (c *conn) stat(ctx context.Context, id string) error {
	defer c.watch(ctx)()
	code, msg, err := c.cmd("STAT <%s>", id)
	if err != nil {
		return err
	}
	return articleAnswer(code, msg, 223)
}

func articleAnswer(code int, msg string, ok int) error {
	switch code {
	case ok:
		return nil
	case 423, 430:
		return ErrNoArticle
	case 480, 481, 502:
		return fmt.Errorf("%w: %d %s", ErrAuth, code, msg)
	}
	return &ProtoError{code, msg}
}

func (c *conn) close() {
	_ = c.nc.SetDeadline(time.Now().Add(time.Second))
	_ = c.tp.PrintfLine("QUIT")
	_ = c.nc.Close()
}

// validID reports whether id can go into a command line. An .nzb comes from
// anywhere, and a line break in an id would send a command of its own.
func validID(id string) bool {
	if id == "" || len(id) > 250 {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool { return r <= ' ' || r == '<' || r == '>' || r == 0x7f })
}

// cleanID drops the angle brackets an .nzb sometimes keeps around an id.
func cleanID(id string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(id), "<"), ">")
}
