package remotefs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// A REAL SSH SERVER SERVING A FAKE FILE SYSTEM. SFTP is a subsystem inside
// SSH, so there is no way to exercise the handshake, the host-key policy and a
// refused password without a genuine server on the other end - and those three
// are exactly the parts of sftp.go worth testing. The files it serves are the
// same in-memory tree the FTP fake uses, so nothing here touches a disk.

type fakeSFTP struct {
	addr string
	// pub is this server's host key, which the known-hosts tests need in order
	// to write a DIFFERENT one into the file and prove the client refuses.
	pub ssh.PublicKey
}

func newFakeSFTP(t *testing.T, user, pass string, tree map[string]fakeNode) *fakeSFTP {
	t.Helper()
	signer := testSigner(t)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, given []byte) (*ssh.Permissions, error) {
			if c.User() != user || string(given) != pass {
				return nil, errors.New("permission denied")
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSH(c, cfg, tree)
		}
	}()
	return &fakeSFTP{addr: ln.Addr().String(), pub: signer.PublicKey()}
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func serveSSH(c net.Conn, cfg *ssh.ServerConfig, tree map[string]fakeNode) {
	defer c.Close()
	conn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only sessions here")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func(ch ssh.Channel, reqs <-chan *ssh.Request) {
			for req := range reqs {
				// A subsystem request's payload is a length-prefixed name, and
				// "sftp" is the only one this fixture answers.
				ok := req.Type == "subsystem" && len(req.Payload) >= 4 &&
					string(req.Payload[4:4+binary.BigEndian.Uint32(req.Payload[:4])]) == "sftp"
				_ = req.Reply(ok, nil)
				if !ok {
					continue
				}
				srv := sftp.NewRequestServer(ch, sftp.Handlers{
					FileGet:  memFS{tree},
					FilePut:  memFS{tree},
					FileCmd:  memFS{tree},
					FileList: memFS{tree},
				})
				_ = srv.Serve()
				_ = srv.Close()
				_ = ch.Close()
			}
		}(ch, chReqs)
	}
}

func (s *fakeSFTP) target(p string) Target {
	h, port, _ := net.SplitHostPort(s.addr)
	n, _ := strconv.Atoi(port)
	return Target{Kind: KindSFTP, Host: h, Port: n, Path: p}
}

// memFS is a read-only sftp.Handlers over the same tree the FTP fake serves.
type memFS struct{ tree map[string]fakeNode }

func (m memFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	n, ok := m.tree[cleanPath(r.Filepath)]
	if !ok || n.dir {
		return nil, os.ErrNotExist
	}
	return bytes.NewReader(n.data), nil
}

// Read-only on purpose: this package downloads, and a fixture that could write
// would be testing a capability the FS interface deliberately does not have.
func (memFS) Filewrite(*sftp.Request) (io.WriterAt, error) { return nil, os.ErrPermission }
func (memFS) Filecmd(*sftp.Request) error                  { return os.ErrPermission }

func (m memFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	p := cleanPath(r.Filepath)
	n, ok := m.tree[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	switch r.Method {
	case "List":
		if !n.dir {
			return nil, os.ErrInvalid
		}
		var out []os.FileInfo
		var names []string
		for q := range m.tree {
			if q != p && path.Dir(q) == p {
				names = append(names, q)
			}
		}
		sort.Strings(names)
		for _, q := range names {
			out = append(out, memInfo{name: path.Base(q), node: m.tree[q]})
		}
		return listerAt(out), nil
	case "Stat":
		return listerAt{memInfo{name: path.Base(p), node: n}}, nil
	}
	return nil, os.ErrInvalid
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

type memInfo struct {
	name string
	node fakeNode
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return int64(len(i.node.data)) }
func (i memInfo) Mode() fs.FileMode {
	if i.node.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i memInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (i memInfo) IsDir() bool        { return i.node.dir }
func (i memInfo) Sys() any           { return nil }

// ---- the tests -------------------------------------------------------------

// dialerFor points the known-hosts file at the test's own temp directory, the
// same way the app points it at its data directory - never at the user's own
// ~/.ssh/known_hosts, which a test has no business appending to.
func dialerFor(t *testing.T) Dialer {
	t.Helper()
	return Dialer{KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts")}
}

func TestSFTPReadsAFileAndResumesAtAnExactOffset(t *testing.T) {
	s := newFakeSFTP(t, "alice", "secret", ftpTree())
	r := Resolver{
		Accounts: Logins{s.target("/").Host: {Username: "alice", Password: "secret"}},
		Dialer:   dialerFor(t),
	}

	got, err := r.Resolve(context.Background(), request(LinkOf(s.target("/pub/notes.txt"))))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "notes.txt" || got.Size != 5 {
		t.Errorf("got %+v, want notes.txt at 5 bytes", got)
	}

	fs, err := r.Dialer.Dial(context.Background(), s.target("/"), Login{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	// SFTP addresses reads by offset in the protocol itself, so there is no
	// restart command to be refused and a resumed read is exact.
	rc, err := fs.Open(context.Background(), "/pub/notes.txt", 3)
	if err != nil {
		t.Fatalf("Open at an offset: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "lo" {
		t.Errorf("read %q, want the tail after three bytes", b)
	}
}

func TestSFTPWrongPasswordAndMissingFileAreDifferentAnswers(t *testing.T) {
	s := newFakeSFTP(t, "alice", "secret", ftpTree())
	d := dialerFor(t)

	bad := Resolver{Accounts: Logins{s.target("/").Host: {Username: "alice", Password: "wrong"}}, Dialer: d}
	_, err := bad.Resolve(context.Background(), request(LinkOf(s.target("/pub/notes.txt"))))
	if !errors.Is(err, ErrAuth) {
		t.Errorf("wrong password: error = %v, want the credential named", err)
	}

	good := Resolver{Accounts: Logins{s.target("/").Host: {Username: "alice", Password: "secret"}}, Dialer: d}
	_, err = good.Resolve(context.Background(), request(LinkOf(s.target("/pub/gone.mkv"))))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: error = %v, want the path named", err)
	}
}

func TestSFTPWithNoAccountSaysSoRatherThanFailingAtTheHandshake(t *testing.T) {
	// There is no anonymous SFTP, so this is worth naming before a single
	// packet leaves: "no account is stored" sends somebody to the accounts
	// page, "unable to authenticate" sends them looking for a typo in a
	// password they never typed.
	s := newFakeSFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Dialer: dialerFor(t)}
	_, err := r.Resolve(context.Background(), request(LinkOf(s.target("/pub/notes.txt"))))
	if !errors.Is(err, ErrNoAccount) {
		t.Fatalf("error = %v, want it to say no account is stored", err)
	}
}

func TestSFTPRemembersAHostKeyAndRefusesAChangedOne(t *testing.T) {
	s := newFakeSFTP(t, "alice", "secret", ftpTree())
	dir := t.TempDir()
	d := Dialer{KnownHostsFile: filepath.Join(dir, "known_hosts")}
	login := Login{Username: "alice", Password: "secret"}

	// First use: nothing is known yet, so the key is accepted and written
	// down. A headless app has no terminal to answer a prompt on, so refusing
	// here would mean it could never reach a server at all.
	fs, err := d.Dial(context.Background(), s.target("/"), login)
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	_ = fs.Close()
	written, err := os.ReadFile(d.KnownHostsFile)
	if err != nil || len(written) == 0 {
		t.Fatalf("the host key was not recorded: %v", err)
	}
	if !strings.Contains(string(written), "ssh-ed25519") {
		t.Errorf("the recorded line does not look like a host key: %q", written)
	}

	// Second use, same key: accepted, and no duplicate line - the file is
	// re-read on every dial rather than kept in memory, so a line removed by
	// hand takes effect without a restart.
	fs, err = d.Dial(context.Background(), s.target("/"), login)
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	_ = fs.Close()
	again, _ := os.ReadFile(d.KnownHostsFile)
	if len(again) != len(written) {
		t.Errorf("the key was written twice:\n%s", again)
	}

	// A DIFFERENT key on a host that is already known: either the server was
	// rebuilt or somebody is in the middle, and the client cannot tell which -
	// so it refuses and names the file to fix, exactly as ssh(1) does.
	other := Dialer{KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts")}
	line := knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, testSigner(t).PublicKey())
	if err := os.WriteFile(other.KnownHostsFile, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Dial(context.Background(), s.target("/"), login); err == nil {
		t.Fatal("a changed host key was accepted")
	} else if !strings.Contains(err.Error(), "host key changed") {
		t.Errorf("error = %v, want it to name the changed host key", err)
	}
}
