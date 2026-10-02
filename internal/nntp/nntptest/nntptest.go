// Package nntptest runs a small NNTP server for tests: articles held in
// memory, an optional login, TLS, and ways to make it misbehave the way real
// servers do (articles it lacks, copies damaged on the way, dropped
// connections).
package nntptest

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// Server is one fake server.
type Server struct {
	// Host and Port are where it listens.
	Host string
	Port int
	// ClientTLS is what a client needs to trust a TLS server, nil otherwise.
	ClientTLS *tls.Config

	l net.Listener

	mu       sync.Mutex
	user     string
	pass     string
	articles map[string][]byte
	damaged  map[string]int
	dropNext int
	bodies   map[string]int
	open     int
	maxOpen  int
	logins   int
	conns    map[net.Conn]bool
}

// New starts a plain server that is closed when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return start(t, l, nil)
}

// NewTLS starts a server that speaks TLS from the first byte, with a
// certificate of its own that ClientTLS trusts.
func NewTLS(t testing.TB) *Server {
	t.Helper()
	cert, pool := selfSigned(t)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	return start(t, l, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
}

func start(t testing.TB, l net.Listener, client *tls.Config) *Server {
	addr := l.Addr().(*net.TCPAddr)
	s := &Server{
		Host: addr.IP.String(), Port: addr.Port, ClientTLS: client, l: l,
		articles: map[string][]byte{}, damaged: map[string]int{}, bodies: map[string]int{}, conns: map[net.Conn]bool{},
	}
	go s.serve()
	t.Cleanup(s.Close)
	return s
}

// Close stops listening and hangs up every connection, so the server looks
// unreachable from then on.
func (s *Server) Close() {
	_ = s.l.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
	}
}

// SetLogin makes user and pass the only login the server accepts, and it
// answers no article before one.
func (s *Server) SetLogin(user, pass string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user, s.pass = user, pass
}

// Login is the login SetLogin set.
func (s *Server) Login() (user, pass string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user, s.pass
}

// Add stores an article body under id, without angle brackets.
func (s *Server) Add(id string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.articles[id] = body
}

// AddPart stores p, yEnc-encoded, under id.
func (s *Server) AddPart(id string, p yenc.Part) {
	var b bytes.Buffer
	if err := yenc.Encode(&b, p, 128); err != nil {
		panic(err)
	}
	s.Add(id, b.Bytes())
}

// Damage makes the next n answers for id carry a flipped byte, as an article
// damaged on its way between servers does.
func (s *Server) Damage(id string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.damaged[id] = n
}

// DropNext hangs up instead of answering the next n BODY commands.
func (s *Server) DropNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropNext = n
}

// Bodies is how often BODY was asked for id.
func (s *Server) Bodies(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[id]
}

// MaxOpen is the most connections that were open at once.
func (s *Server) MaxOpen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxOpen
}

// Logins counts the successful logins.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func (s *Server) serve() {
	for {
		c, err := s.l.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = true
		s.open++
		s.maxOpen = max(s.maxOpen, s.open)
		s.mu.Unlock()
		go func() {
			s.session(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.open--
			s.mu.Unlock()
			_ = c.Close()
		}()
	}
}

func (s *Server) session(c net.Conn) {
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	reply := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\r\n", args...)
		_ = w.Flush()
	}
	reply("200 nntptest ready")
	wantUser, wantPass := s.Login()
	authed := wantUser == ""
	user := ""
	for {
		_ = c.SetReadDeadline(time.Now().Add(time.Minute))
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			reply("500 empty command")
			continue
		}
		cmd := strings.ToUpper(fields[0])
		arg := ""
		if len(fields) > 1 {
			arg = fields[len(fields)-1]
		}
		switch {
		case cmd == "QUIT":
			reply("205 bye")
			return
		case cmd == "AUTHINFO" && len(fields) == 3 && strings.EqualFold(fields[1], "USER"):
			user = arg
			reply("381 password please")
		case cmd == "AUTHINFO" && len(fields) == 3 && strings.EqualFold(fields[1], "PASS"):
			if user == wantUser && arg == wantPass {
				authed = true
				s.mu.Lock()
				s.logins++
				s.mu.Unlock()
				reply("281 welcome")
			} else {
				reply("481 wrong login")
			}
		case cmd == "BODY" || cmd == "STAT":
			if !authed {
				reply("480 login first")
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(arg, "<"), ">")
			body, ok, drop := s.lookup(id, cmd == "BODY")
			switch {
			case drop:
				return
			case !ok:
				reply("430 no such article")
			case cmd == "STAT":
				reply("223 0 <%s>", id)
			default:
				reply("222 0 <%s>", id)
				_, _ = w.Write(dotStuff(body))
				reply(".")
			}
		default:
			reply("500 what?")
		}
	}
}

// lookup finds an article and counts the BODY. A damaged copy has one byte
// of its data changed, which leaves the yEnc lines intact.
func (s *Server) lookup(id string, body bool) (out []byte, ok, drop bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if body {
		s.bodies[id]++
		if s.dropNext > 0 {
			s.dropNext--
			return nil, false, true
		}
	}
	a, ok := s.articles[id]
	if !ok {
		return nil, false, false
	}
	if body && s.damaged[id] > 0 {
		s.damaged[id]--
		a = damage(a)
	}
	return a, true, false
}

// damage changes the last character of the first data line, which changes
// one decoded byte and leaves the lines around it as they were.
func damage(article []byte) []byte {
	out := bytes.Clone(article)
	start := 0
	for start < len(out) {
		end := bytes.IndexByte(out[start:], '\n')
		if end < 0 {
			end = len(out) - start
		}
		line := bytes.TrimRight(out[start:start+end], "\r")
		if len(line) > 0 && !bytes.HasPrefix(line, []byte("=y")) {
			last := &line[len(line)-1]
			if *last == 'A' {
				*last = 'B'
			} else {
				*last = 'A'
			}
			return out
		}
		start += end + 1
	}
	return out
}

// dotStuff doubles a dot at the start of a line and ends every line in CR LF.
func dotStuff(body []byte) []byte {
	var b bytes.Buffer
	for _, line := range bytes.SplitAfter(body, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		line = bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(line, []byte(".")) {
			b.WriteByte('.')
		}
		b.Write(line)
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

func selfSigned(t testing.TB) (tls.Certificate, *x509.CertPool) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "nntptest"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// Addr is host:port, for a log line.
func (s *Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }
