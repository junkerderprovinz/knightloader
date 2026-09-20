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

// ftpTimeout bounds the dial and every command afterwards: enough for a
// distant seedbox listing thousands of entries, short enough that a silent
// server cannot hold a download slot for long.
const ftpTimeout = 45 * time.Second

// ftpFS is one logged-in FTP or FTPS connection. The library allows one
// transfer per connection at a time, so each download gets its own connection
// and Resolver.Check walks a host's links in sequence.
type ftpFS struct {
	c *ftp.ServerConn
	// open is the data connection a Read streams from. Close closes it
	// before the control connection, or the transfer hangs.
	open io.Closer
}

// anonymousLogin is the login for a public archive: "anonymous", with an
// address-shaped password that identifies nobody.
var anonymousLogin = Login{Username: "anonymous", Password: "knightloader@invalid"}

func dialFTP(ctx context.Context, t Target, login Login) (FS, error) {
	opts := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(ftpTimeout),
	}
	if t.TLS {
		// ServerName is set explicitly so certificate checks use the host
		// name.
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

// Stat asks the server what a path is by listing its parent. MLST (the
// library's GetEntry) is refused by many servers, vsftpd's default setup
// among them, while every server that serves files can LIST. The root has no
// parent and is a directory.
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
		if e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, Entry{
			Name: e.Name,
			Size: clampSize(e.Size),
			Dir:  e.Type == ftp.EntryTypeFolder,
		})
	}
	return out, nil
}

// Open starts a RETR at offset, which the library turns into REST + RETR. The
// library requires REST's 350 reply before sending RETR, so a server without
// restarts fails here instead of streaming from byte zero.
//
// The transfer type is set to binary first: FTP's default ASCII mode rewrites
// line endings, corrupting files and shifting resume offsets.
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

// ftpError translates RFC 959 reply codes into this package's errors: 530 and
// 332 are authentication, 550 is a missing path. The transient 450 and 451
// stay unmapped, since a busy server says nothing about the file being gone.
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

// clampSize converts the library's uint64 size, treating anything that would
// overflow int64 as a malformed listing and unknown.
func clampSize(n uint64) int64 {
	const max = int64(^uint64(0) >> 1)
	if n > uint64(max) {
		return 0
	}
	return int64(n)
}
