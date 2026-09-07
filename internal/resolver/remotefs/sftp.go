package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sftpTimeout bounds the TCP dial and the SSH handshake. It does not bound the
// transfer that follows, and must not: an SFTP download of a 40 GB disk image
// is not a stuck connection.
const sftpTimeout = 30 * time.Second

// sftpFS is one authenticated SSH connection with the SFTP subsystem open on
// it. Both handles are kept because closing only the SFTP client leaves the
// SSH connection and its goroutines alive for the life of the process.
type sftpFS struct {
	ssh *ssh.Client
	c   *sftp.Client
}

func (d Dialer) dialSFTP(ctx context.Context, t Target, login Login) (FS, error) {
	if login.Username == "" {
		// No anonymous SFTP exists. Saying so here, by name, is worth more
		// than the "unable to authenticate" the server would answer three
		// round trips later: one tells the user to add an account, the other
		// sends them looking for a typo in a password they never typed.
		return nil, fmt.Errorf("remotefs: sftp %s: %w", t.Host, ErrNoAccount)
	}
	cb, err := d.hostKeys()
	if err != nil {
		return nil, err
	}
	// DialContext rather than ssh.Dial: ssh.Dial's own Timeout field bounds
	// the dial and the handshake but knows nothing about a cancelled request,
	// and a user who closed the tab must not leave a half-open SSH session
	// behind for thirty seconds.
	conn, err := (&net.Dialer{Timeout: sftpTimeout}).DialContext(ctx, "tcp", t.Addr())
	if err != nil {
		return nil, DialError("sftp", t.Addr(), err)
	}
	// The handshake has no context of its own, so the deadline stands in for
	// one and is cleared again the moment it completes - leaving it in place
	// would kill the transfer the instant it ran out.
	_ = conn.SetDeadline(time.Now().Add(sftpTimeout))
	cfg := &ssh.ClientConfig{
		User:            login.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(login.Password)},
		HostKeyCallback: cb,
		Timeout:         sftpTimeout,
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, t.Addr(), cfg)
	if err != nil {
		_ = conn.Close()
		return nil, sftpDialError(t, err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sc, chans, reqs)
	fs, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		// The one failure worth naming separately: the login worked and the
		// server simply has no SFTP subsystem enabled, which is a server
		// configuration a person can go and fix.
		return nil, fmt.Errorf("remotefs: sftp %s: the server accepted the login but would not open an SFTP session: %w", t.Host, err)
	}
	return &sftpFS{ssh: client, c: fs}, nil
}

func (f *sftpFS) Close() error {
	err := f.c.Close()
	if cerr := f.ssh.Close(); err == nil {
		err = cerr
	}
	return err
}

func (f *sftpFS) Stat(_ context.Context, p string) (Entry, error) {
	fi, err := f.c.Stat(p)
	if err != nil {
		return Entry{}, sftpError(p, err)
	}
	return Entry{Name: filepath.ToSlash(fi.Name()), Size: fi.Size(), Dir: fi.IsDir()}, nil
}

func (f *sftpFS) List(_ context.Context, p string) ([]Entry, error) {
	fis, err := f.c.ReadDir(p)
	if err != nil {
		return nil, sftpError(p, err)
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		out = append(out, Entry{Name: fi.Name(), Size: fi.Size(), Dir: fi.IsDir()})
	}
	return out, nil
}

// Open seeks to offset before handing the file back.
//
// RESUME IS EXACT HERE. SFTP reads are addressed by offset in the protocol
// itself - there is no restart command to be refused and no ambiguity about
// where the stream begins - so a resumed download continues at precisely the
// byte the part file ended on. Seek past the end of the file succeeds and
// yields EOF immediately, which is the correct behaviour for the one way that
// can happen: a part file larger than the remote file, i.e. the remote file
// was replaced with a shorter one while the download was paused. The caller
// notices that as a transfer that ends with fewer bytes than Stat promised -
// see Backend.transfer, which refuses to call such a download finished.
func (f *sftpFS) Open(_ context.Context, p string, offset int64) (io.ReadCloser, error) {
	file, err := f.c.Open(p)
	if err != nil {
		return nil, sftpError(p, err)
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, sftpError(p, err)
		}
	}
	return file, nil
}

// sftpError maps the SFTP status codes the library has already normalised into
// this package's vocabulary. pkg/sftp turns SSH_FX_NO_SUCH_FILE into
// os.ErrNotExist and SSH_FX_PERMISSION_DENIED into os.ErrPermission, so this
// reads those rather than the raw status numbers.
//
// Permission denied stays its own sentence and is NOT folded into ErrNotFound.
// The two look the same to a user staring at a failed download and are
// opposites to whoever has to fix it: one means the path is wrong, the other
// means the account is.
func sftpError(p string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("remotefs: sftp %s: %w", p, ErrNotFound)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("remotefs: sftp %s: the account is not allowed to read this path", p)
	}
	return fmt.Errorf("remotefs: sftp %s: %w", p, err)
}

// sftpDialError separates a refused credential from every other handshake
// failure. x/crypto/ssh has no typed error for it - the transport returns a
// plain error built from the server's disconnect message - so the text is what
// there is to read, and the two phrasings below are the library's own, not a
// server's: "ssh: unable to authenticate" when every method was rejected and
// "ssh: handshake failed" wrapping it.
//
// Getting this wrong in the safe direction costs a slightly vaguer sentence;
// getting it wrong the other way would mark an account benched (see
// internal/accounts/health.go) because a network hiccup looked like a bad
// password, so anything unrecognised stays a plain error.
func sftpDialError(t Target, err error) error {
	if msg := err.Error(); strings.Contains(msg, "unable to authenticate") {
		return fmt.Errorf("remotefs: sftp %s: %w", t.Host, ErrAuth)
	}
	return DialError("sftp", t.Addr(), err)
}

// ---- host keys ------------------------------------------------------------
//
// TRUST ON FIRST USE, AND A LOUD REFUSAL AFTER THAT. An SSH client that
// accepts any key it is offered has no protection against a machine in the
// middle at all, so ssh.InsecureIgnoreHostKey is not an option here even
// though it is one line and always "works". The other extreme - refusing every
// host that is not already in a known_hosts file - would mean a headless app
// with no terminal to answer a prompt on could never reach a NAS at all.
//
// So: the first time a host is seen its key is written down, and from then on
// it must match. That is exactly what ssh(1) does when a person answers "yes"
// at its prompt, and it defends against the attack that actually matters here
// - a server that used to be the user's and now is not - while leaving the
// one it cannot defend against (a machine in the middle on the very first
// connection) clearly documented rather than silently accepted.

// knownHostsMu serialises the read-append-reload cycle below. Two SFTP tasks
// starting together against the same new host would otherwise both find it
// unknown and both append it, leaving a duplicate line in the file.
var knownHostsMu sync.Mutex

func (d Dialer) hostKeys() (ssh.HostKeyCallback, error) {
	path := d.KnownHostsFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("remotefs: sftp: no known-hosts file is configured and this machine has no home directory: %w", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("remotefs: sftp: %w", err)
	}
	// knownhosts.New refuses a file that is not there, and "no host has ever
	// been accepted yet" is the ordinary state of a fresh install rather than
	// an error - so an empty one is created instead of failing the dial.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("remotefs: sftp: %w", err)
	}
	_ = f.Close()

	return func(hostport string, remote net.Addr, key ssh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()
		verify, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("remotefs: sftp: %s is unreadable: %w", path, err)
		}
		err = verify(hostport, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) || len(ke.Want) > 0 {
			// Want being non-empty is the case worth stopping for: this host
			// IS known and offered a different key. That is either a rebuilt
			// server or somebody in the middle, and the client cannot tell
			// which - so it refuses and names the file to fix, exactly as ssh
			// itself does.
			return fmt.Errorf("remotefs: sftp %s: the server's host key changed since it was first accepted; "+
				"if the server really was rebuilt, remove its line from %s: %w", hostport, path, err)
		}
		// Unknown host: write it down and go on. Re-read on the next dial
		// rather than kept in memory, so a line removed by hand takes effect
		// without a restart.
		return appendKnownHost(path, hostport, key)
	}, nil
}

// appendKnownHost writes one host's key in the format ssh(1) reads.
//
// knownhosts.Line builds the line rather than fmt: the address has to be
// normalised (a non-standard port becomes "[host]:port"), and getting that
// spelling wrong produces a file that looks right and never matches anything.
func appendKnownHost(path, hostport string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("remotefs: sftp: %w", err)
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostport)}, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("remotefs: sftp: %w", err)
	}
	return nil
}
