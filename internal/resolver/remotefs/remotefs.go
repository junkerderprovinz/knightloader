// Package remotefs downloads from servers a person already owns: a seedbox
// over FTP or FTPS, a NAS over SFTP, a Nextcloud or ownCloud over WebDAV.
//
// The protocols share nothing but a tree that can be listed, a file size that
// can be asked for and a byte stream that can start at an offset. That is the
// FS interface, and it is why the package is named after a file system rather
// than after its protocols.
//
// Credentials never travel in a link: a URL with a password is refused (see
// Parse) because it would end up in the task store, the collector and the log.
// A username in the link is used only for a host without a stored account,
// which keeps anonymous public FTP working.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Kind is which protocol a target speaks. The scheme alone does not decide it:
// webdav:// and webdavs:// are one protocol, and so is https:// for a host an
// account marks as a WebDAV server (see Resolver.Match).
type Kind string

const (
	KindFTP    Kind = "ftp"
	KindFTPS   Kind = "ftps"
	KindSFTP   Kind = "sftp"
	KindWebDAV Kind = "webdav"
)

// Default ports, applied when a URL names none.
//
// ftps:// defaults to 21, not 990. Explicit TLS (AUTH TLS on port 21) is what
// current servers speak; implicit TLS on 990 is deprecated by RFC 4217. A
// target on port 990 is dialled implicitly, anything else explicitly.
const (
	portFTP        = 21
	portFTPSImplic = 990
	portSFTP       = 22
	portHTTP       = 80
	portHTTPS      = 443
)

// Target is one file or directory on one server, with everything needed to
// reach it except the credential.
type Target struct {
	Kind Kind
	// Host is the bare hostname, lower-cased. It is also the account id a
	// stored login is filed under, so it never carries the port.
	Host string
	Port int
	// Path is the absolute path on the server, "/" for the root.
	Path string
	// User is the username the link named, used only when no account is
	// stored for the host.
	User string
	// TLS is whether the transport is encrypted (ftps://, or WebDAV over
	// https). SFTP ignores it.
	TLS bool
	// ImplicitTLS selects FTPS with TLS from the first byte on port 990. Parse
	// sets it from the port.
	ImplicitTLS bool
}

// Addr is the host:port the dialers connect to.
func (t Target) Addr() string { return t.Host + ":" + strconv.Itoa(t.Port) }

// HTTPURL is the plain http(s) URL of a WebDAV target, which the engine
// fetches like any other link. The port is left off when it is the scheme's
// default, since origin comparisons compare the text.
func (t Target) HTTPURL() string {
	scheme := "http"
	if t.TLS {
		scheme = "https"
	}
	host := t.Host
	if (t.TLS && t.Port != portHTTPS) || (!t.TLS && t.Port != portHTTP) {
		host = t.Addr()
	}
	// Built through url.URL so spaces and umlauts in the path are escaped.
	u := url.URL{Scheme: scheme, Host: host, Path: t.Path}
	return u.String()
}

// Login is one stored credential. Both fields empty is the anonymous case,
// which only FTP has any use for.
type Login struct {
	Username string
	Password string
}

// Accounts answers which stored login belongs to a host; the app wires in the
// encrypted account store. ok=false means nothing is stored, which
// Resolver.Match reads as "leave this https:// link alone".
type Accounts interface {
	Login(host string) (Login, bool)
}

// Logins is an Accounts backed by a map from host to login.
type Logins map[string]Login

func (m Logins) Login(host string) (Login, bool) {
	l, ok := m[strings.ToLower(host)]
	return l, ok
}

// The failures every protocol has its own words for (FTP 550, SFTP
// SSH_FX_NO_SUCH_FILE, WebDAV 404), translated at the edge of each
// implementation. They are always wrapped, so the message still names server
// and path while errors.Is keeps working.
var (
	// ErrNoAccount is a server nothing is stored for; nobody was refused.
	ErrNoAccount = errors.New("no account is stored for this server")
	// ErrAuth is a credential the server looked at and refused.
	ErrAuth = errors.New("the server refused this username and password")
	// ErrNotFound is a path the server says is not there.
	ErrNotFound = errors.New("the server says this path does not exist")
	// ErrPasswordInLink is a link carrying its own password.
	ErrPasswordInLink = errors.New("a password in the link is refused; store it under Accounts instead")
)

// DialError turns a failed connection into a sentence instead of the operating
// system's error text, which on Windows arrives in the system's language. The
// cases are told apart by error type, never by message; anything that is not
// a dial failure passes through wrapped.
func DialError(proto string, addr string, err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fmt.Errorf("remotefs: %s %s: the name %s does not resolve", proto, addr, dns.Name)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("remotefs: %s %s: the server did not answer in time", proto, addr)
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return fmt.Errorf("remotefs: %s %s: nothing answered on that port", proto, addr)
	}
	return fmt.Errorf("remotefs: %s %s: %w", proto, addr, err)
}

// Entry is one thing a remote directory holds. Size is 0 for a directory and
// for a server that lists without sizes.
type Entry struct {
	Name string
	Size int64
	Dir  bool
}

// FS is one open connection to one server. It is read-only, so no bug in the
// app can delete anything through it.
type FS interface {
	// Stat says whether a path is a file or a directory, and how large.
	Stat(ctx context.Context, path string) (Entry, error)
	// List names what a directory holds, directly, without descending.
	List(ctx context.Context, path string) ([]Entry, error)
	// Open starts reading a file at offset. An implementation that cannot
	// honour a non-zero offset must fail rather than restart at 0, which
	// would append the whole file to a part file.
	Open(ctx context.Context, path string, offset int64) (io.ReadCloser, error)
	io.Closer
}

// Dialer opens connections. The zero value works.
type Dialer struct {
	// KnownHostsFile is where SSH host keys are remembered (see sftp.go).
	// Empty means ~/.ssh/known_hosts; the app passes a path in its data
	// directory because a container has no useful home directory.
	KnownHostsFile string
	// HTTPClient carries WebDAV's PROPFIND and GET. Nil means an httpx client
	// with this package's timeout.
	HTTPClient *http.Client
}

// Dial opens a connection to one target.
//
// The zero Login means no credential: FTP falls back to anonymous, WebDAV
// sends no Authorization header, and SFTP refuses (see Resolver.loginFor).
func (d Dialer) Dial(ctx context.Context, t Target, login Login) (FS, error) {
	switch t.Kind {
	case KindFTP, KindFTPS:
		return dialFTP(ctx, t, login)
	case KindSFTP:
		return d.dialSFTP(ctx, t, login)
	case KindWebDAV:
		return d.dialWebDAV(ctx, t, login)
	}
	return nil, fmt.Errorf("remotefs: %q is not a protocol this handles", t.Kind)
}

// Parse turns a pasted link into a Target, refusing unknown schemes and
// passwords in the URL.
//
// httpsIsWebDAV is the caller's decision for a plain https:// link: true only
// when an account is stored for that exact host (see Resolver.Match).
func Parse(raw string, httpsIsWebDAV bool) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("remotefs: %q is not a URL: %w", raw, err)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Target{}, fmt.Errorf("remotefs: %q names no server", raw)
	}

	t := Target{Host: host, Path: cleanPath(u.EscapedPath())}
	if u.User != nil {
		if _, set := u.User.Password(); set {
			return Target{}, fmt.Errorf("remotefs: %s: %w", host, ErrPasswordInLink)
		}
		t.User = u.User.Username()
	}

	var defPort int
	switch strings.ToLower(u.Scheme) {
	case "ftp":
		t.Kind, defPort = KindFTP, portFTP
	case "ftps":
		t.Kind, t.TLS, defPort = KindFTPS, true, portFTP
	case "sftp":
		t.Kind, defPort = KindSFTP, portSFTP
	case "webdav":
		// Plaintext like the dav:// of desktop file managers, for a NAS on the
		// LAN; webdavs:// is the encrypted form.
		t.Kind, defPort = KindWebDAV, portHTTP
	case "webdavs":
		t.Kind, t.TLS, defPort = KindWebDAV, true, portHTTPS
	case "https":
		if !httpsIsWebDAV {
			return Target{}, fmt.Errorf("remotefs: %s is an ordinary web link, not a configured WebDAV server", host)
		}
		t.Kind, t.TLS, defPort = KindWebDAV, true, portHTTPS
	default:
		return Target{}, fmt.Errorf("remotefs: %q is not a protocol this handles", u.Scheme)
	}

	t.Port = defPort
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Target{}, fmt.Errorf("remotefs: %q is not a port", p)
		}
		t.Port = n
	}
	t.ImplicitTLS = t.Kind == KindFTPS && t.Port == portFTPSImplic
	return t, nil
}

// cleanPath normalises a URL path into an absolute server path: "" becomes
// "/", and a trailing slash is dropped so one directory typed two ways is one
// target.
func cleanPath(p string) string {
	// Unescaped because every FS takes a real path.
	if dec, err := url.PathUnescape(p); err == nil {
		p = dec
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	return p
}

// Join is path.Join for a server path, so no caller reaches for filepath.Join
// and builds backslash paths on Windows.
func Join(dir, name string) string { return path.Join(dir, name) }

// Name is the file name a target's path ends in, or the host for the root
// directory.
func Name(t Target) string {
	if b := path.Base(t.Path); b != "" && b != "/" && b != "." {
		return b
	}
	return t.Host
}

// LinkOf rebuilds the canonical, credential-free link for a target, used as a
// staged task's URL. Without a username, default port or trailing slash, one
// file has one spelling and the duplicate check can fold it.
func LinkOf(t Target) string {
	scheme := string(t.Kind)
	defPort := portFTP
	switch t.Kind {
	case KindFTPS:
		defPort = portFTP
	case KindSFTP:
		defPort = portSFTP
	case KindWebDAV:
		if t.TLS {
			scheme, defPort = "webdavs", portHTTPS
		} else {
			scheme, defPort = "webdav", portHTTP
		}
	}
	host := t.Host
	if t.Port != defPort {
		host = t.Addr()
	}
	u := url.URL{Scheme: scheme, Host: host, Path: t.Path}
	return u.String()
}
