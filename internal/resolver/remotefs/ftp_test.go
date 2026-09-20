package remotefs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fake FTP server speaks just enough RFC 959 for the client this package
// uses, and can be made to refuse a password or a restart on purpose.

// fakeNode is one file or directory in the served tree, keyed by absolute
// path in fakeFTP.tree.
type fakeNode struct {
	dir  bool
	data []byte
}

type fakeFTP struct {
	t        *testing.T
	user     string
	pass     string
	tree     map[string]fakeNode
	addr     string
	refuseRE bool // answer REST with 502, the way a server without restart support does
	// shortBy cuts that many bytes off every RETR and still closes the data
	// connection cleanly.
	shortBy int
	// restarts records every offset a REST arrived with.
	restarts chan int64
}

func newFakeFTP(t *testing.T, user, pass string, tree map[string]fakeNode) *fakeFTP {
	t.Helper()
	s := &fakeFTP{t: t, user: user, pass: pass, tree: tree, restarts: make(chan int64, 8)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.session(c)
		}
	}()
	return s
}

// host and port split the listener address, because Target keeps the two apart.
func (s *fakeFTP) host() string {
	h, _, _ := net.SplitHostPort(s.addr)
	return h
}

func (s *fakeFTP) port() int {
	_, p, _ := net.SplitHostPort(s.addr)
	n, _ := strconv.Atoi(p)
	return n
}

func (s *fakeFTP) target(p string) Target {
	return Target{Kind: KindFTP, Host: s.host(), Port: s.port(), Path: p}
}

func (s *fakeFTP) session(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReader(c)
	say := func(format string, a ...any) {
		_, _ = fmt.Fprintf(c, format+"\r\n", a...)
	}
	say("220 fake ftp ready")

	var (
		dataLn     net.Listener
		loggedIn   bool
		restOffset int64
	)
	defer func() {
		if dataLn != nil {
			_ = dataLn.Close()
		}
	}()

	// data accepts the connection the client opened after EPSV, writes the
	// body, closes it and sends the closing 226.
	data := func(write func(net.Conn)) {
		if dataLn == nil {
			say("425 no data connection")
			return
		}
		say("150 opening data connection")
		dc, err := dataLn.Accept()
		_ = dataLn.Close()
		dataLn = nil
		if err != nil {
			say("426 data connection failed")
			return
		}
		write(dc)
		_ = dc.Close()
		say("226 transfer complete")
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "USER":
			say("331 need password")
		case "PASS":
			if arg != s.pass {
				say("530 login incorrect")
				continue
			}
			loggedIn = true
			say("230 logged in")
		case "FEAT":
			// Without FEAT the client stays on plain LIST instead of MLSD, as
			// with many real servers.
			say("500 not understood")
		case "TYPE":
			say("200 type set")
		case "EPSV":
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				say("425 cannot open data connection")
				continue
			}
			dataLn = ln
			_, p, _ := net.SplitHostPort(ln.Addr().String())
			say("229 entering extended passive mode (|||%s|)", p)
		case "REST":
			if s.refuseRE {
				say("502 restart not supported")
				continue
			}
			n, _ := strconv.ParseInt(arg, 10, 64)
			restOffset = n
			select {
			case s.restarts <- n:
			default:
			}
			say("350 restarting at %d", n)
		case "LIST":
			if !loggedIn {
				say("530 not logged in")
				continue
			}
			node, ok := s.tree[cleanPath(arg)]
			if !ok || !node.dir {
				say("550 no such directory")
				continue
			}
			body := s.listing(cleanPath(arg))
			data(func(dc net.Conn) { _, _ = io.WriteString(dc, body) })
		case "RETR":
			if !loggedIn {
				say("530 not logged in")
				continue
			}
			node, ok := s.tree[cleanPath(arg)]
			if !ok || node.dir {
				say("550 no such file")
				restOffset = 0
				continue
			}
			off := restOffset
			restOffset = 0
			if off > int64(len(node.data)) {
				off = int64(len(node.data))
			}
			body := node.data[off:]
			if s.shortBy > 0 && s.shortBy < len(body) {
				body = body[:len(body)-s.shortBy]
			}
			data(func(dc net.Conn) { _, _ = dc.Write(body) })
		case "QUIT":
			say("221 goodbye")
			return
		default:
			say("500 not understood")
		}
	}
}

// listing renders the children of dir in the Unix "ls -l" shape the library
// parses when the server advertises no MLST.
func (s *fakeFTP) listing(dir string) string {
	var names []string
	for p := range s.tree {
		if p == dir || path.Dir(p) != dir {
			continue
		}
		names = append(names, p)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, p := range names {
		n := s.tree[p]
		mode := "-rw-r--r--"
		if n.dir {
			mode = "drwxr-xr-x"
		}
		fmt.Fprintf(&b, "%s 1 owner group %8d Jan 02 15:04 %s\r\n", mode, len(n.data), path.Base(p))
	}
	return b.String()
}

func ftpTree() map[string]fakeNode {
	return map[string]fakeNode{
		"/":                      {dir: true},
		"/pub":                   {dir: true},
		"/pub/film.mkv":          {data: []byte(strings.Repeat("A", 4096))},
		"/pub/notes.txt":         {data: []byte("hello")},
		"/pub/subs":              {dir: true},
		"/pub/subs/film.srt":     {data: []byte("00:00:01 --> 00:00:02")},
		"/pub/subs/deeper":       {dir: true},
		"/pub/subs/deeper/x.nfo": {data: []byte("nfo")},
	}
}

func TestFTPResolveNamesTheFileAndItsSize(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Accounts: Logins{s.host(): {Username: "alice", Password: "secret"}}}

	link := LinkOf(s.target("/pub/film.mkv"))
	got, err := r.Resolve(context.Background(), request(link))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "film.mkv" || got.Size != 4096 {
		t.Errorf("got %+v, want film.mkv at 4096 bytes", got)
	}
	if got.Available != "online" {
		t.Errorf("availability = %q, want online", got.Available)
	}
	if got.Connections != 1 {
		t.Errorf("connections = %d, want 1", got.Connections)
	}
	// The resolved link passes through the dispatcher and must carry no
	// credential.
	if strings.Contains(got.DirectURL, "secret") || strings.Contains(got.DirectURL, "alice") {
		t.Errorf("the resolved link carries the credential: %q", got.DirectURL)
	}
}

func TestFTPWrongPasswordIsReportedAsARefusedCredential(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Accounts: Logins{s.host(): {Username: "alice", Password: "wrong"}}}

	_, err := r.Resolve(context.Background(), request(LinkOf(s.target("/pub/film.mkv"))))
	if err == nil {
		t.Fatal("a wrong password resolved successfully")
	}
	// A refused account and a missing file need different fixes.
	if !errors.Is(err, ErrAuth) {
		t.Errorf("error = %v, want it to say the credential was refused", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("a refused login was reported as a missing file: %v", err)
	}
}

func TestFTPMissingFileIsReportedAsGoneAndNotAsAFailure(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Accounts: Logins{s.host(): {Username: "alice", Password: "secret"}}}

	_, err := r.Resolve(context.Background(), request(LinkOf(s.target("/pub/gone.mkv"))))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to say the path is not there", err)
	}

	// The batched check says the same.
	got, err := r.Check(context.Background(), []string{
		LinkOf(s.target("/pub/film.mkv")),
		LinkOf(s.target("/pub/gone.mkv")),
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(got) != 2 || got[0] != "online" || got[1] != "offline" {
		t.Errorf("Check = %v, want [online offline]", got)
	}
}

func TestFTPFolderLinkBecomesOneEntryPerFile(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Accounts: Logins{s.host(): {Username: "alice", Password: "secret"}}}

	got, err := r.List(context.Background(), LinkOf(s.target("/pub")))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, l := range got {
		names = append(names, l.Name)
	}
	// Files in subfolders are included.
	want := []string{"film.mkv", "notes.txt", "film.srt", "x.nfo"}
	if len(names) != len(want) {
		t.Fatalf("List returned %v, want the four files %v", names, want)
	}
	for _, w := range want {
		if !contains(names, w) {
			t.Errorf("List is missing %q: %v", w, names)
		}
	}
	for _, l := range got {
		if l.Name == "film.mkv" && l.Size != 4096 {
			t.Errorf("the listing lost the size: %+v", l)
		}
		if strings.HasSuffix(l.URL, "/pub") {
			t.Errorf("the folder itself was staged as a file: %+v", l)
		}
	}
}

func TestFTPFileLinkIsNotAFolderAndExpandsToNothing(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	r := Resolver{Accounts: Logins{s.host(): {Username: "alice", Password: "secret"}}}
	got, err := r.List(context.Background(), LinkOf(s.target("/pub/notes.txt")))
	if err != nil || got != nil {
		t.Fatalf("List(file) = %v, %v; want nothing and no error", got, err)
	}
}

func TestFTPResumeContinuesAtTheOffsetInsteadOfStartingAgain(t *testing.T) {
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	fs, err := Dialer{}.Dial(context.Background(), s.target("/"), Login{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	rc, err := fs.Open(context.Background(), "/pub/notes.txt", 2)
	if err != nil {
		t.Fatalf("Open at an offset: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "llo" {
		t.Errorf("read %q, want the tail after two bytes", b)
	}
	select {
	case off := <-s.restarts:
		if off != 2 {
			t.Errorf("the server was asked to restart at %d, want 2", off)
		}
	case <-time.After(2 * time.Second):
		t.Error("no REST reached the server, so the read started from the beginning")
	}
}

func TestFTPResumeFailsWhenTheServerRefusesToRestart(t *testing.T) {
	// Serving from byte zero after a refused REST would corrupt the part
	// file.
	s := newFakeFTP(t, "alice", "secret", ftpTree())
	s.refuseRE = true
	fs, err := Dialer{}.Dial(context.Background(), s.target("/"), Login{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if _, err := fs.Open(context.Background(), "/pub/notes.txt", 2); err == nil {
		t.Fatal("a refused restart was accepted, which would corrupt a resumed file")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
