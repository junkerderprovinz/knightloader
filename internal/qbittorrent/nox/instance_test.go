package nox

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
)

func startFake(t *testing.T, mode, profile string) *Instance {
	t.Helper()
	in := Start(Options{Binary: fakeBinary(t, mode), Profile: profile})
	t.Cleanup(func() { _ = in.Stop() })
	return in
}

func ready(t *testing.T, in *Instance) *qbittorrent.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatalf("not ready: %v (status %+v)", err, in.Status())
	}
	return c
}

func TestItsQBittorrentAnswersOnLoopbackToItsOwnLoginOnly(t *testing.T) {
	profile := t.TempDir()
	in := startFake(t, fakeGood, profile)
	c := ready(t, in)
	if v, err := c.Version(context.Background()); err != nil || v != "v4.5.5" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	st := in.Status()
	if st.State != StateRunning || st.Version != "4.5.5" {
		t.Fatalf("status %+v", st)
	}
	stranger, _ := qbittorrent.New("http://127.0.0.1:"+portOf(t, st), "knightloader", "guess")
	if err := stranger.Login(context.Background()); err == nil {
		t.Fatal("a wrong password was let in")
	}
}

func TestStopEndsItsQBittorrent(t *testing.T) {
	in := startFake(t, fakeGood, t.TempDir())
	ready(t, in)
	port := portOf(t, in.Status())
	if err := in.Stop(); err != nil {
		t.Fatal(err)
	}
	if st := in.Status(); st.State != StateStopped {
		t.Fatalf("status %+v", st)
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
		conn.Close()
		t.Fatal("something still answers on its port")
	}
}

func TestOneThatDiesIsStartedAgain(t *testing.T) {
	in := startFake(t, fakeCrashOnce, t.TempDir())
	ready(t, in)
	if st := in.Status(); st.Restarts != 1 || st.State != StateRunning {
		t.Fatalf("status %+v", st)
	}
}

func TestOneThatIgnoresTheShutdownIsEndedAnyway(t *testing.T) {
	defer func(g time.Duration) { stopGrace = g }(stopGrace)
	stopGrace = 500 * time.Millisecond
	in := startFake(t, fakeDeafToRequest, t.TempDir())
	ready(t, in)
	port := portOf(t, in.Status())
	done := make(chan error, 1)
	go func() { done <- in.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Stop did not return")
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
		conn.Close()
		t.Fatal("it still runs")
	}
}

func TestALeftoverFromALastRunIsAskedToQuitBeforeTheNextStarts(t *testing.T) {
	profile := t.TempDir()
	// What a crash leaves: a qBittorrent on the saved port with the saved
	// login, which nobody supervises.
	l, err := LoadLogin(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Port = freePort(t)
	if err := SaveLogin(profile, nil, l); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(profile, Config{Port: l.Port, Username: l.Username, Password: l.Password}); err != nil {
		t.Fatal(err)
	}
	leftover := exec.Command(fakeBinary(t, fakeGood), "--profile="+profile)
	if err := leftover.Start(); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	go func() { leftover.Wait(); close(gone) }()
	t.Cleanup(func() { leftover.Process.Kill(); <-gone })
	waitAnswers(t, l.Port)

	in := startFake(t, fakeGood, profile)
	ready(t, in)
	select {
	case <-gone:
	case <-time.After(10 * time.Second):
		t.Fatal("the leftover was not asked to quit")
	}
	if in.Status().Port != l.Port {
		t.Fatalf("the new run took port %d, not the saved %d", in.Status().Port, l.Port)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitAnswers(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond); err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("nothing answers on %d", port)
}

func TestAProgramThatIsNotThereFailsWithItsReason(t *testing.T) {
	in := Start(Options{Binary: filepath.Join(t.TempDir(), "qbittorrent-nox"), Profile: t.TempDir()})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := in.Ready(ctx); err == nil {
		t.Fatal("ready without a program")
	}
	if st := in.Status(); st.Detail == "" || st.State != StateFailed {
		t.Fatalf("status %+v", st)
	}
}
