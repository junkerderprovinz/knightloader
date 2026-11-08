package cnl

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestClassifyNamesTheProgramOnThePort(t *testing.T) {
	for _, tc := range []struct {
		p    Process
		kind HolderKind
		name string
	}{
		{Process{Path: `C:\Program Files\JDownloader\JDownloader2.exe`}, HeldByJDownloader, "JDownloader 2"},
		{Process{Path: `C:\Users\me\AppData\Local\JDownloader 2.0\JDownloader.exe`}, HeldByJDownloader, "JDownloader 2"},
		{Process{Path: "/opt/jd2/JDownloader2"}, HeldByJDownloader, "JDownloader 2"},
		{Process{Path: `C:\Program Files\Java\bin\javaw.exe`, Cmdline: `"javaw.exe" -jar "D:\JD\JDownloader.jar"`}, HeldByJDownloader, "JDownloader 2"},
		{Process{Path: "/usr/lib/jvm/bin/java", Cmdline: "java -Xmx1g -jar /home/me/jd2/JDownloader.jar -norestart"}, HeldByJDownloader, "JDownloader 2"},
		{Process{Path: "/usr/bin/java", Cmdline: "java -jar /srv/other/server.jar"}, HeldByProgram, "java"},
		{Process{Path: `C:\Program Files\KnightLoader\KnightLoader.exe`}, HeldByKnightLoader, "KnightLoader"},
		{Process{Path: "/usr/local/bin/knightloader"}, HeldByKnightLoader, "KnightLoader"},
		{Process{Path: `C:\Tools\pyLoad\pyload.EXE`}, HeldByProgram, "pyload"},
		{Process{Path: "nginx"}, HeldByProgram, "nginx"},
		{Process{PID: 4}, HeldByUnknown, ""},
	} {
		kind, name := classify(tc.p)
		if kind != tc.kind || name != tc.name {
			t.Errorf("classify(%+v) = %q, %q; want %q, %q", tc.p, kind, name, tc.kind, tc.name)
		}
	}
}

func TestJudgePutsTheAnswersTogether(t *testing.T) {
	inUse := &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", addrInUseErrno())}
	denied := &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EACCES)}
	refused := errors.New("connection refused")
	jd := &Process{PID: 7, Path: `C:\Program Files\JDownloader\JDownloader2.exe`}
	nameless := &Process{PID: 4}
	desk := Identity{App: "KnightLoader", Name: "Desk", Instance: "inst-1"}

	for _, tc := range []struct {
		name    string
		bindErr error
		id      Identity
		idErr   error
		owner   *Process
		want    Holding
	}{
		{"a KnightLoader that answers wins over the system's name", inUse, desk, nil, jd, Holding{Kind: HeldByKnightLoader, Name: "Desk", Identity: desk}},
		{"JDownloader answering with something else", inUse, Identity{}, ErrNotKnightLoader, jd, Holding{Kind: HeldByJDownloader, Name: "JDownloader 2"}},
		{"JDownloader not answering at all", inUse, Identity{}, refused, jd, Holding{Kind: HeldByJDownloader, Name: "JDownloader 2"}},
		{"a process the system does not name", inUse, Identity{}, refused, nameless, Holding{Kind: HeldByUnknown}},
		{"something answering that the system does not show", denied, Identity{}, ErrNotKnightLoader, nil, Holding{Kind: HeldByUnknown}},
		{"a port in use with nobody to name", inUse, Identity{}, refused, nil, Holding{Kind: HeldByUnknown}},
		{"a port this process may not open", denied, Identity{}, refused, nil, Holding{}},
	} {
		if got := judge(tc.bindErr, tc.id, tc.idErr, tc.owner); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// addrInUseErrno is what a bind to a taken port fails with here.
func addrInUseErrno() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	_, err = net.Listen("tcp", ln.Addr().String())
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		panic("binding a taken port did not fail with an errno: " + err.Error())
	}
	return errno
}

func TestABindToATakenPortIsAddressInUse(t *testing.T) {
	port := freePort(t)
	first := NewListener(&recorder{}, port)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Stop)
	second := NewListener(&recorder{}, port)
	if err := second.Start(); !addrInUse(err) {
		t.Errorf("addrInUse(%v) = false", err)
	}
}

func TestTheSystemNamesTheProcessOnAPort(t *testing.T) {
	switch runtime.GOOS {
	case "windows", "linux", "darwin":
	default:
		t.Skip("this system is not asked who holds a port")
	}
	for _, family := range []struct{ name, addr string }{
		{"IPv4", "127.0.0.1:0"},
		{"IPv6", "[::1]:0"},
	} {
		t.Run(family.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", family.addr)
			if err != nil && family.name == "IPv6" {
				t.Skipf("no IPv6 loopback: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			// Linux hands out a port number already listening at another
			// address, such as a test server of a package running alongside
			// on 127.0.0.1 when this one bound [::1], so the port may have
			// more than one owner.
			owners := portOwners(context.Background(), ln.Addr().(*net.TCPAddr).Port)
			i := slices.IndexFunc(owners, func(p Process) bool { return p.PID == os.Getpid() })
			if i < 0 {
				t.Fatalf("owners = %+v; want this process, %d, among them", owners, os.Getpid())
			}
			p := owners[i]
			if kind, name := classify(p); kind != HeldByProgram || !strings.HasPrefix(name, "cnl.test") || !strings.Contains(p.Cmdline, "cnl.test") {
				t.Errorf("%+v is %q, %q; want this test binary", p, kind, name)
			}
		})
	}
}

func TestAFreePortHasNoOwner(t *testing.T) {
	if owners := portOwners(context.Background(), freePort(t)); len(owners) > 0 {
		t.Errorf("owners = %+v on a free port", owners)
	}
}
