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

// sftpTimeout bounds the TCP dial and the SSH handshake, never the transfer
// that follows.
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
		// SFTP has no anonymous mode; this says "add an account" rather than
		// suggesting a wrong password.
		return nil, fmt.Errorf("remotefs: sftp %s: %w", t.Host, ErrNoAccount)
	}
	cb, err := d.hostKeys()
	if err != nil {
		return nil, err
	}
	// DialContext so a cancelled request stops the dial; ssh.Dial only knows
	// a timeout.
	conn, err := (&net.Dialer{Timeout: sftpTimeout}).DialContext(ctx, "tcp", t.Addr())
	if err != nil {
		return nil, DialError("sftp", t.Addr(), err)
	}
	// The deadline bounds the handshake and is cleared afterwards so it does
	// not cut off the transfer.
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
		// The login worked but the server has no SFTP subsystem enabled.
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

// Open seeks to offset before handing the file back. SFTP reads are addressed
// by offset, so resuming is exact. A seek past the end yields EOF at once,
// which Backend.transfer reports as a short transfer.
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

// sftpError maps the os errors pkg/sftp makes of SSH_FX_NO_SUCH_FILE and
// SSH_FX_PERMISSION_DENIED into this package's errors. Permission denied
// stays separate from ErrNotFound: one means the path is wrong, the other the
// account.
func sftpError(p string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("remotefs: sftp %s: %w", p, ErrNotFound)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("remotefs: sftp %s: the account is not allowed to read this path", p)
	}
	return fmt.Errorf("remotefs: sftp %s: %w", p, err)
}

// sftpDialError separates a refused credential from other handshake failures.
// x/crypto/ssh has no typed error for it, so the library's own "unable to
// authenticate" text is matched. Anything else stays a plain error, since a
// misread network failure would bench the account as a bad password.
func sftpDialError(t Target, err error) error {
	if msg := err.Error(); strings.Contains(msg, "unable to authenticate") {
		return fmt.Errorf("remotefs: sftp %s: %w", t.Host, ErrAuth)
	}
	return DialError("sftp", t.Addr(), err)
}

// knownHostsMu serialises the read-append cycle in hostKeys, so two tasks
// meeting the same new host do not both append it.
var knownHostsMu sync.Mutex

// hostKeys returns a trust-on-first-use host key check: an unknown host's key
// is recorded, and afterwards it must match, as with ssh(1) after answering
// "yes". Accepting any key would allow a machine in the middle, and refusing
// unknown hosts would lock out a headless app that cannot prompt. A machine in
// the middle on the very first connection remains undetected.
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
	// knownhosts.New refuses a missing file, which is normal on a fresh
	// install.
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
			// A known host offered a different key: a rebuilt server or a
			// machine in the middle, which cannot be told apart.
			return fmt.Errorf("remotefs: sftp %s: the server's host key changed since it was first accepted; "+
				"if the server really was rebuilt, remove its line from %s: %w", hostport, path, err)
		}
		// The file is read again on every dial, so a line removed by hand
		// takes effect without a restart.
		return appendKnownHost(path, hostport, key)
	}, nil
}

// appendKnownHost writes one host's key in the format ssh(1) reads, with the
// address normalised ("[host]:port" for a non-standard port).
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
