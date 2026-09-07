// Package remotefs is KnightLoader's intake for the servers a person already
// owns: a seedbox reached over FTP or FTPS, a NAS over SFTP, a Nextcloud or an
// ownCloud over WebDAV. Until it existed this app could fetch from the whole
// public internet and from nothing on the user's own side of it.
//
// WHY THE PACKAGE IS NAMED AFTER A FILE SYSTEM AND NOT AFTER ITS PROTOCOLS.
// FTP, SFTP and WebDAV have almost nothing in common as protocols - one is a
// 1985 text dialogue over two sockets, one is a binary subsystem inside SSH,
// one is XML over HTTP. What they do have in common is the only thing this app
// needs from them: each presents a TREE that can be listed, a file whose size
// can be asked for, and a byte stream that can be started at an offset. That is
// the FS interface below, and it is the whole seam - so the package is named
// after the shape it depends on rather than after today's three
// implementations. SMB or S3, if they ever arrive, are two more files behind
// the same three methods and no rename.
//
// CREDENTIALS NEVER TRAVEL IN A LINK. A password pasted into a URL would be
// written to the task store in plain text, shown in the collector's own URL
// column, and carried into every log line that prints the task - so a link that
// carries one is refused outright (see Parse) rather than quietly stripped,
// with a sentence pointing at the encrypted account store where it belongs
// (internal/accounts). A username is not a secret and is honoured as a
// fallback for a host no account has been stored for, which is what keeps
// anonymous and semi-anonymous public FTP working.
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

// Kind is which of the four protocols a target speaks. It exists because the
// URL scheme alone does not decide it: webdav:// and webdavs:// are the same
// protocol over a different transport, and https:// is that same protocol
// again when an account says the host is a WebDAV server (see Resolver.Match).
type Kind string

const (
	KindFTP    Kind = "ftp"
	KindFTPS   Kind = "ftps"
	KindSFTP   Kind = "sftp"
	KindWebDAV Kind = "webdav"
)

// Default ports, applied when a URL names none. They are the registered ones:
// 21 for FTP, 22 for SSH/SFTP, 80 and 443 for the two HTTP transports WebDAV
// rides on.
//
// ftps:// gets 21 and NOT 990, and that is a decision rather than an
// oversight. Two incompatible things have been called FTPS for thirty years:
// implicit TLS on port 990, which negotiates nothing and is deprecated in
// RFC 4217's own words, and explicit TLS on the ordinary port 21, which starts
// as plain FTP and issues AUTH TLS. Everything shipped this decade speaks the
// second. So the port decides which one is meant - a target that names 990 is
// dialled implicitly, anything else explicitly - which means the common case
// needs no flag and the rare one needs no new scheme.
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
	// Host is the bare hostname, lower-cased. It is also the ACCOUNT ID a
	// stored login is filed under (see Accounts), which is why it never
	// carries the port: a person who moves their seedbox to a non-standard
	// port has not changed which account it is.
	Host string
	Port int
	// Path is the absolute path on the server. It is never empty: a URL that
	// names no path at all addresses the server's root, which is a directory
	// like any other and lists like one.
	Path string
	// User is the username the LINK named, which is only ever a fallback for a
	// host no account is stored for - see the package comment.
	User string
	// TLS is whether the transport is encrypted: an ftps:// target, or a
	// WebDAV one reached over https. Read by the FTP and WebDAV dialers; the
	// SFTP one has no unencrypted form to choose between.
	TLS bool
	// ImplicitTLS picks the older of the two things called FTPS - TLS from the
	// first byte, on port 990, with no AUTH TLS negotiation - and is set by
	// Parse from the port alone. See the port constants for why the port is
	// what decides it.
	ImplicitTLS bool
}

// Addr is the host:port the dialers connect to.
func (t Target) Addr() string { return t.Host + ":" + strconv.Itoa(t.Port) }

// HTTPURL is the ordinary http(s) URL a WebDAV target is reached by. It is
// what makes WebDAV downloads free: the answer is a plain URL the existing
// engine already fetches with ranges, several connections and the configured
// outbound route, so this package never has to move those bytes itself.
//
// The port is left off when it is the default for the scheme, because a
// redirect or an auth realm that compares origins does compare the text.
func (t Target) HTTPURL() string {
	scheme := "http"
	if t.TLS {
		scheme = "https"
	}
	host := t.Host
	if (t.TLS && t.Port != portHTTPS) || (!t.TLS && t.Port != portHTTP) {
		host = t.Addr()
	}
	// The path is re-escaped rather than pasted in: a WebDAV share is full of
	// names with spaces and umlauts in them, and a raw one produces a request
	// line the server rejects with 400 long before it ever looks at the file.
	u := url.URL{Scheme: scheme, Host: host, Path: t.Path}
	return u.String()
}

// Login is one stored credential. Both fields empty is the anonymous case,
// which only FTP has any use for.
type Login struct {
	Username string
	Password string
}

// Accounts answers which stored login belongs to a host. It is an interface
// and not *accounts.Store on purpose: this package must be testable without an
// encrypted store on disk, and the app wires the real one in one place (see
// internal/app's rewireBackends).
//
// ok=false means "nothing is stored for this host", which is the answer
// Resolver.Match turns into "leave this https:// link alone" - the whole
// reason the interface exists at all rather than a bare map.
type Accounts interface {
	Login(host string) (Login, bool)
}

// Logins is the trivial Accounts a test - or a caller that already holds the
// answers - hands in: a map from host to login.
type Logins map[string]Login

func (m Logins) Login(host string) (Login, bool) {
	l, ok := m[strings.ToLower(host)]
	return l, ok
}

// The three failures every protocol here has its own words for, translated to
// one vocabulary at the edge of each implementation so that nothing above this
// package has to know that FTP says "550", SFTP says SSH_FX_NO_SUCH_FILE and
// WebDAV says 404 for the same event.
//
// They are wrapped, never returned bare, so the sentence a user reads still
// names the server and the path while errors.Is still answers the question the
// backend and the availability check ask.
var (
	// ErrNoAccount is a server nothing is stored for. It is not an
	// authentication failure: nobody has been refused, nobody was asked.
	ErrNoAccount = errors.New("no account is stored for this server")
	// ErrAuth is a credential the server looked at and refused.
	ErrAuth = errors.New("the server refused this username and password")
	// ErrNotFound is a path the server says is not there.
	ErrNotFound = errors.New("the server says this path does not exist")
	// ErrPasswordInLink is a link carrying its own password - refused, see the
	// package comment.
	ErrPasswordInLink = errors.New("a password in the link is refused; store it under Accounts instead")
)

// DialError turns a failed connection into a sentence rather than into the
// operating system's own error text.
//
// It is not tidiness. On Windows a refused connection reads "dial tcp
// 127.0.0.1:21: connectex: Es konnte keine Verbindung hergestellt werden, da
// der Zielcomputer die Verbindung verweigerte" - three clauses of transport
// plumbing IN THE OPERATING SYSTEM'S LANGUAGE, landing in the error column of
// an English page. internal/proxycfg's probe already refuses to pass that
// through (see TestProbeReportsAnUnreachableProxyInWordsRatherThanInGoErrorText)
// and this is the same rule applied to the same class of failure.
//
// The three cases are told apart by TYPE and not by matching the message,
// because matching the message is the bug: the message is the thing that is in
// the wrong language. Anything that is not a dial failure at all passes
// through untouched - inventing a sentence for an error this function does not
// recognise would hide the one detail that mattered.
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
// for a server that lists without one, which is a real state and not an error:
// a name with no size still downloads perfectly well, it just cannot be shown
// in the collector before it starts.
type Entry struct {
	Name string
	Size int64
	Dir  bool
}

// FS is one open connection to one server: the three questions this app asks a
// remote file system and nothing else. Deliberately read-only - KnightLoader
// downloads, and a seam that cannot delete is a seam no bug in this app can
// delete through.
type FS interface {
	// Stat says whether a path is a file or a directory, and how large.
	Stat(ctx context.Context, path string) (Entry, error)
	// List names what a directory holds, directly, without descending.
	List(ctx context.Context, path string) ([]Entry, error)
	// Open starts reading a file at offset. offset 0 is an ordinary read; a
	// larger one is a resumed download, and an implementation that cannot
	// honour it must FAIL rather than quietly start from the beginning - see
	// each implementation's own note. A stream silently restarted at 0 and
	// appended to a part file is a corrupt file that reports success.
	Open(ctx context.Context, path string, offset int64) (io.ReadCloser, error)
	io.Closer
}

// Dialer opens connections. The zero value works, which is what makes this a
// struct rather than two more arguments on Dial: a test, a one-off tool and
// the app itself all say Dialer{...}.Dial and only fill in what they actually
// have an opinion about.
type Dialer struct {
	// KnownHostsFile is where SSH host keys are remembered - see the
	// trust-on-first-use note in sftp.go, which is the whole reason this field
	// exists rather than a hardcoded path. Empty means the user's own
	// ~/.ssh/known_hosts, so a person who has already accepted their NAS's key
	// in a terminal is not asked to accept it again; the app passes a path
	// inside its own data directory instead, because a container has no home
	// directory worth writing to.
	KnownHostsFile string
	// HTTPClient is what WebDAV's PROPFIND and GET go out on. Nil means a
	// client from internal/httpx with this package's own timeout, which is
	// what everything but a test uses.
	HTTPClient *http.Client
}

// Dial opens a connection to one target. It is the one place the five schemes
// turn into three implementations, so a caller - the resolver, the availability
// check, the backend - never branches on Kind itself.
//
// login is what the caller resolved out of the account store. The zero Login
// means "no credential", and the three implementations differ on it exactly as
// their protocols do: FTP falls back to anonymous, WebDAV sends no
// Authorization header at all (a public share needs none), and SFTP refuses,
// because there is no credential-free form of it to fall back to. See
// Resolver.loginFor, which is where that difference is decided.
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

// Parse turns a pasted link into a Target. It is deliberately strict about the
// two things that would otherwise fail much later and much more confusingly: a
// scheme this package does not speak, and a password riding in the URL.
//
// httpsIsWebDAV is what the caller has already decided about a plain https://
// link - true only when an account is stored for that exact host, which is the
// one thing that makes claiming an ordinary web URL safe (see Resolver.Match).
// Parse does not decide it, because Parse has no account store and a function
// that quietly claimed every https link would be a much worse bug than one
// that refuses a scheme.
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
		// Plaintext, matching the dav:// / davs:// pair every desktop file
		// manager already uses. It is the right default for the case it
		// serves - a NAS on the same LAN, where https means a certificate
		// nobody wants to install - and it does mean the Basic credential
		// goes out unencrypted, which is why the encrypted variant is one
		// letter away and named in the same breath everywhere this is
		// documented.
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
	// See the port constants: an ftps:// target on 990 is the implicit-TLS
	// dialect, and the only signal that says so is the port itself.
	t.ImplicitTLS = t.Kind == KindFTPS && t.Port == portFTPSImplic
	return t, nil
}

// cleanPath normalises a URL path into an absolute server path. An empty path
// becomes "/" because a link to a bare host addresses its root directory, and
// a trailing slash is kept off so that the same directory typed two ways is
// one target.
func cleanPath(p string) string {
	// Unescaped here rather than left as written: every FS below takes a real
	// path, and %20 is a URL's way of spelling a space, not a file whose name
	// contains a percent sign.
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

// Join is path.Join for a server path, kept here so that every caller building
// "this directory plus that entry" does it the same way and none of them uses
// filepath.Join by accident - which on Windows would build "\\pub\\file.iso"
// and hand a backslash path to an FTP server that has never seen one.
func Join(dir, name string) string { return path.Join(dir, name) }

// Name is the file name a target's path ends in, or the host when the path
// names no file - a directory listing at the root, which is a legitimate
// paste and needs a task name that is not the empty string.
func Name(t Target) string {
	if b := path.Base(t.Path); b != "" && b != "/" && b != "." {
		return b
	}
	return t.Host
}

// LinkOf rebuilds the canonical, credential-free link for a target, which is
// what a staged task's URL is and what the backend re-parses later. It is
// never the raw pasted string: that one may carry a username, a redundant
// default port or a trailing slash, and three spellings of one file are three
// tasks the duplicate check cannot fold together.
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
