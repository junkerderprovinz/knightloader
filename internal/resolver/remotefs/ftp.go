package remotefs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// ftpTimeout bounds the dial and every command on the connection afterwards.
// Long enough for a seedbox on the other side of an ocean answering a LIST of
// a directory with several thousand entries in it, short enough that a server
// which accepted the TCP connection and then stopped talking cannot hold a
// download slot for the rest of the afternoon.
const ftpTimeout = 45 * time.Second

// ftpFS is one logged-in FTP or FTPS connection.
//
// ONE TRANSFER AT A TIME, and the library says so itself: "A single connection
// only supports one in-flight data connection. It is not safe to be called
// concurrently." That is the reason Backend gives an FTP download a connection
// of its own rather than sharing the one the resolver stat'ed with, and the
// reason the availability check (Resolver.Check) walks a host's links in
// sequence on one connection instead of firing them off together.
type ftpFS struct {
	c *ftp.ServerConn
	// open is the data connection a Read is currently streaming from. FTP
	// keeps it separate from the control connection, and closing the control
	// one first leaves the transfer hanging - so Close closes this first.
	open io.Closer
}

// anonymousLogin is what an FTP server expects for a public archive: the
// literal user "anonymous" and, by thirty years of convention, an e-mail
// address as the password. This app sends its own name instead of inventing
// somebody's address, which every public server accepts and no server can use
// to identify a person.
var anonymousLogin = Login{Username: "anonymous", Password: "knightloader@invalid"}

func dialFTP(ctx context.Context, t Target, login Login) (FS, error) {
	opts := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(ftpTimeout),
	}
	if t.TLS {
		// ServerName is set explicitly rather than left to the library: the
		// dial address may be an IP for a server whose certificate names a
		// host, and an empty ServerName there turns a perfectly valid
		// certificate into a handshake failure nobody can act on.
		cfg := &tls.Config{ServerName: t.Host, MinVersion: tls.VersionTLS12}
		if t.ImplicitTLS {
			opts = append(opts, ftp.DialWithTLS(cfg))
		} else {
			opts = append(opts, ftp.DialWithExplicitTLS(cfg))
		}
	}
	c, err := ftp.Dial(t.Addr(), opts...)
	if err != nil {
		return nil, DialError("ftp", t.Addr(), err)
	}
	if login == (Login{}) {
		// Only FTP has an anonymous mode, and only FTP falls back to it. The
		// other two protocols answer ErrNoAccount instead, because there is no
		// credential-free form of either that would work.
		login = anonymousLogin
	}
	if login.Username == "" {
		login.Username = anonymousLogin.Username
	}
	if err := c.Login(login.Username, login.Password); err != nil {
		_ = c.Quit()
		return nil, ftpError(t, "/", err)
	}
	return &ftpFS{c: c}, nil
}

func (f *ftpFS) Close() error {
	if f.open != nil {
		_ = f.open.Close()
		f.open = nil
	}
	return f.c.Quit()
}

// Stat asks the server what a path is.
//
// It does NOT use the library's GetEntry, which speaks MLST - the modern,
// machine-readable stat command. A large share of servers still in the field
// (vsftpd with the default configuration among them) answer 500 to it, and
// falling back only on that one code means guessing which of a dozen refusal
// codes count. Listing the PARENT and picking this name out of it works on
// every server that can serve a file at all, because a client that cannot LIST
// cannot browse either.
//
// The root is the one path with no parent to look in, and it is a directory by
// definition, so it answers directly rather than listing "/" twice.
func (f *ftpFS) Stat(ctx context.Context, p string) (Entry, error) {
	if p == "/" {
		return Entry{Name: "/", Dir: true}, nil
	}
	parent, name := path.Split(strings.TrimSuffix(p, "/"))
	entries, err := f.list(ctx, parent)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("remotefs: ftp %s: %w", p, ErrNotFound)
}

func (f *ftpFS) List(ctx context.Context, p string) ([]Entry, error) { return f.list(ctx, p) }

func (f *ftpFS) list(_ context.Context, p string) ([]Entry, error) {
	if p == "" {
		p = "/"
	}
	raw, err := f.c.List(p)
	if err != nil {
		return nil, ftpError(Target{}, p, err)
	}
	out := make([]Entry, 0, len(raw))
	for _, e := range raw {
		// "." and ".." are the directory itself and its parent. Staging them
		// would turn one folder link into a task for the folder it is already
		// in, and on a server that lists ".." at the root, into a loop.
		if e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, Entry{
			Name: e.Name,
			// Size is uint64 in the library and int64 everywhere here. A
			// listing that claims more than 8 exabytes is a malformed line,
			// not a file, and letting it wrap into a negative size would make
			// the progress bar and the disk-space check both nonsense.
			Size: clampSize(e.Size),
			Dir:  e.Type == ftp.EntryTypeFolder,
		})
	}
	return out, nil
}

// Open starts a RETR at offset, which the library turns into REST + RETR.
//
// RESUME IS REAL HERE AND FAILS LOUDLY WHEN IT IS NOT. RFC 3659's REST is
// answered with 350 when the server accepted the restart point, and the
// library requires exactly that code before it sends RETR at all - so a server
// that does not support restarts returns an error here rather than a stream
// that silently begins at byte zero. That distinction is the whole safety of
// resuming: a stream restarted at 0 and appended to a half-finished part file
// produces a corrupt file that reports success.
//
// The transfer type is forced to binary first. FTP's default is ASCII, and an
// ASCII transfer rewrites line endings on the way through - which corrupts
// every archive, image and video ever downloaded, and, worse for resuming,
// makes the byte offset the client counted disagree with the one the server
// counts.
func (f *ftpFS) Open(_ context.Context, p string, offset int64) (io.ReadCloser, error) {
	if err := f.c.Type(ftp.TransferTypeBinary); err != nil {
		return nil, ftpError(Target{}, p, err)
	}
	var (
		resp *ftp.Response
		err  error
	)
	if offset > 0 {
		resp, err = f.c.RetrFrom(p, uint64(offset))
	} else {
		resp, err = f.c.Retr(p)
	}
	if err != nil {
		return nil, ftpError(Target{}, p, err)
	}
	f.open = resp
	return resp, nil
}

// ftpError translates the server's reply code into this package's vocabulary.
//
// The codes are RFC 959's own: 530 is "not logged in", 332 is "need account",
// and 550 is the catch-all "requested action not taken" that every server in
// existence uses for a path that is not there. 450 and 451 are transient and
// deliberately NOT mapped to ErrNotFound - a file the server is too busy to
// serve right now is not a file that is gone, and calling it gone would put a
// permanent "offline" on a link that works again in a minute.
func ftpError(t Target, p string, err error) error {
	where := p
	if t.Host != "" {
		where = t.Host + p
	}
	var pe *textproto.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case 530, 332:
			return fmt.Errorf("remotefs: ftp %s: %w (%s)", where, ErrAuth, pe.Msg)
		case 550:
			return fmt.Errorf("remotefs: ftp %s: %w (%s)", where, ErrNotFound, pe.Msg)
		}
		return fmt.Errorf("remotefs: ftp %s: %d %s", where, pe.Code, pe.Msg)
	}
	return fmt.Errorf("remotefs: ftp %s: %w", where, err)
}

// clampSize keeps a nonsense size out of the rest of the app. See List.
func clampSize(n uint64) int64 {
	const max = int64(^uint64(0) >> 1)
	if n > uint64(max) {
		return 0
	}
	return int64(n)
}
