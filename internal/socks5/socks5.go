// Package socks5 is a SOCKS5 server on 127.0.0.1 for a program that can be
// given a proxy but cannot reach a tunnel that lives inside this process, such
// as the qBittorrent KnightLoader runs and its built-in VPN. Whatever it is
// asked to reach goes through the Tunnel and nowhere else: while the tunnel is
// down it refuses, and nothing goes out over the machine's own connection.
//
// It speaks CONNECT and UDP ASSOCIATE (RFC 1928) behind a username and
// password (RFC 1929) of its own, made anew for every server, so another
// program on the machine cannot use it.
package socks5

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Tunnel is where the server sends everything it carries.
type Tunnel interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// ListenPacket opens an unconnected UDP socket inside the tunnel, for
	// network "udp4" or "udp6".
	ListenPacket(ctx context.Context, network string) (net.PacketConn, error)
	// LookupNetIP resolves host with the tunnel's name servers.
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

const (
	// handshakeTimeout bounds a client's greeting, login and request.
	handshakeTimeout = 10 * time.Second
	// dialTimeout bounds one connect through the tunnel, which waits for a
	// tunnel that is reconnecting.
	dialTimeout = 30 * time.Second
	// lookupTimeout bounds resolving a datagram's host name, during which the
	// association's other datagrams wait.
	lookupTimeout = 5 * time.Second
	nameTTL       = 10 * time.Minute
	maxNames      = 256
)

// The protocol's numbers.
const (
	version     = 5
	authVersion = 1

	methodPassword = 2
	methodNone     = 0xff

	cmdConnect   = 1
	cmdAssociate = 3

	atypIPv4 = 1
	atypName = 3
	atypIPv6 = 4

	replyOK              = 0
	replyFailure         = 1
	replyHostUnreachable = 4
	replyRefused         = 5
	replyNoCommand       = 7
	replyNoAddressType   = 8
)

// Username is the name every server takes; the password is what keeps others
// out.
const Username = "knightloader"

// Server is one SOCKS5 server.
type Server struct {
	ln   net.Listener
	tun  Tunnel
	pass string
	logf func(format string, args ...any)
	// ctx ends with Close, and with it every dial and lookup under way.
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// Start brings a server up on a free port of 127.0.0.1, carrying everything
// through tun.
func Start(tun Tunnel, logf func(format string, args ...any)) (*Server, error) {
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		ln:     ln,
		tun:    tun,
		pass:   base64.RawURLEncoding.EncodeToString(secret),
		logf:   logf,
		ctx:    ctx,
		cancel: cancel,
		conns:  map[net.Conn]struct{}{},
	}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Addr is the host:port the server listens on.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Port is the port the server listens on.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Credentials are the username and password a client logs in with.
func (s *Server) Credentials() (user, pass string) { return Username, s.pass }

// Drop ends every connection and association the server carries, so a client
// connects again and takes whatever way the tunnel goes now. The server goes
// on taking new ones.
func (s *Server) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
	}
}

// Close ends every connection and stops listening, and returns once nothing
// of the server runs any more.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	err := s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	return err
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// Out of file handles, say: the next accept may work again.
			s.logf("socks5: accept: %v", err)
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		if !s.track(c) {
			_ = c.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			defer c.Close()
			s.serve(c)
		}()
	}
}

func (s *Server) serve(c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(handshakeTimeout))
	if !s.login(c) {
		return
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil || head[0] != version {
		return
	}
	host, port, ok := readAddr(c, head[3])
	if !ok {
		reply(c, replyNoAddressType, netip.AddrPort{})
		return
	}
	switch head[1] {
	case cmdConnect:
		s.connect(c, net.JoinHostPort(host, strconv.Itoa(int(port))))
	case cmdAssociate:
		s.associate(c, host, port)
	default:
		reply(c, replyNoCommand, netip.AddrPort{})
	}
}

// login takes the client's greeting and its username and password, and
// reports whether they are the server's.
func (s *Server) login(c net.Conn) bool {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil || head[0] != version {
		return false
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return false
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == methodPassword
	}
	if !offered {
		_, _ = c.Write([]byte{version, methodNone})
		return false
	}
	if _, err := c.Write([]byte{version, methodPassword}); err != nil {
		return false
	}
	ver, user, ok := readField(c, true)
	if !ok {
		return false
	}
	_, pass, ok := readField(c, false)
	if !ok {
		return false
	}
	match := subtle.ConstantTimeCompare(user, []byte(Username)) & subtle.ConstantTimeCompare(pass, []byte(s.pass))
	if ver != authVersion || match != 1 {
		_, _ = c.Write([]byte{authVersion, 1})
		return false
	}
	_, err := c.Write([]byte{authVersion, 0})
	return err == nil
}

// readField reads a length-prefixed field of the login, with the version byte
// ahead of it when withVersion is set.
func readField(c net.Conn, withVersion bool) (ver byte, field []byte, ok bool) {
	if withVersion {
		var b [1]byte
		if _, err := io.ReadFull(c, b[:]); err != nil {
			return 0, nil, false
		}
		ver = b[0]
	}
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return 0, nil, false
	}
	field = make([]byte, n[0])
	_, err := io.ReadFull(c, field)
	return ver, field, err == nil
}

// readAddr reads an address of type atyp and its port.
func readAddr(r io.Reader, atyp byte) (host string, port uint16, ok bool) {
	switch atyp {
	case atypIPv4, atypIPv6:
		b := make([]byte, 4)
		if atyp == atypIPv6 {
			b = make([]byte, 16)
		}
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, false
		}
		ip, _ := netip.AddrFromSlice(b)
		host = ip.Unmap().String()
	case atypName:
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil || n[0] == 0 {
			return "", 0, false
		}
		b := make([]byte, n[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, false
		}
		host = string(b)
	default:
		return "", 0, false
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return "", 0, false
	}
	return host, binary.BigEndian.Uint16(p[:]), true
}

// appendAddr appends ap as an address of the protocol, 0.0.0.0:0 when it is
// not valid.
func appendAddr(b []byte, ap netip.AddrPort) []byte {
	ip := ap.Addr().Unmap()
	switch {
	case ip.Is4():
		b = append(b, atypIPv4)
		b = append(b, ip.AsSlice()...)
	case ip.Is6():
		b = append(b, atypIPv6)
		b = append(b, ip.AsSlice()...)
	default:
		b = append(b, atypIPv4, 0, 0, 0, 0)
	}
	return binary.BigEndian.AppendUint16(b, ap.Port())
}

func reply(c net.Conn, code byte, bound netip.AddrPort) bool {
	_, err := c.Write(appendAddr([]byte{version, code, 0}, bound))
	return err == nil
}

// replyFor is the reply code for a dial that failed with err.
func replyFor(err error) byte {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns), errors.Is(err, context.DeadlineExceeded):
		return replyHostUnreachable
	case errors.Is(err, syscall.ECONNREFUSED):
		return replyRefused
	}
	return replyFailure
}

func (s *Server) connect(c net.Conn, address string) {
	ctx, cancel := context.WithTimeout(s.ctx, dialTimeout)
	up, err := s.tun.DialContext(ctx, "tcp", address)
	cancel()
	if err != nil {
		reply(c, replyFor(err), netip.AddrPort{})
		return
	}
	defer up.Close()
	if !reply(c, replyOK, netip.AddrPort{}) {
		return
	}
	_ = c.SetDeadline(time.Time{})
	pipe(c, up)
}

// pipe copies between the client and the far end until the far end is done.
// A client that finishes sending has that passed on and still gets the
// answer; one that breaks off ends both.
func pipe(client, up net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := io.Copy(up, client)
		cw, ok := up.(interface{ CloseWrite() error })
		if err != nil || !ok {
			_ = up.Close()
			return
		}
		_ = cw.CloseWrite()
	}()
	_, _ = io.Copy(client, up)
	_ = client.Close()
	_ = up.Close()
	<-done
}
