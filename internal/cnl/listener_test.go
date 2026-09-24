package cnl

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// freePort returns a loopback port nothing listens on, so a test never binds
// the protocol port a real JDownloader may hold.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func answersProbe(port int) bool {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/jdcheck.js", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.Contains(string(b), "jdownloader=true")
}

func TestListenBindsThePortKLCNLNames(t *testing.T) {
	port := freePort(t)
	t.Setenv("KL_CNL", fmt.Sprint(port))

	l := Listen(&recorder{})
	t.Cleanup(l.Stop)
	if l.Port() != port {
		t.Fatalf("Port() = %d, want %d", l.Port(), port)
	}
	if !answersProbe(port) {
		t.Fatal("the listener does not answer the Click'n'Load probe")
	}
}

func TestListenLeavesTheListenerDownWithKLCNLZero(t *testing.T) {
	t.Setenv("KL_CNL", "0")

	l := Listen(&recorder{})
	t.Cleanup(l.Stop)
	if l.Port() != 0 || !l.OffByEnv() {
		t.Fatalf("Port() = %d, OffByEnv() = %v; want 0 and true", l.Port(), l.OffByEnv())
	}
	if l.Address() != fmt.Sprintf("127.0.0.1:%d", DefaultPort) {
		t.Errorf("Address() = %q; switching on after KL_CNL=0 binds the standard port", l.Address())
	}
}

func TestPortFromEnv(t *testing.T) {
	for _, c := range []struct {
		in      string
		port    int
		atStart bool
	}{
		{"", DefaultPort, true},
		{"9777", 9777, true},
		{"0", DefaultPort, false},
		{"-1", DefaultPort, false},
		{"nine", DefaultPort, true},
	} {
		port, atStart := portFromEnv(c.in)
		if port != c.port || atStart != c.atStart {
			t.Errorf("portFromEnv(%q) = %d, %v; want %d, %v", c.in, port, atStart, c.port, c.atStart)
		}
	}
}

func TestToggleStartsAndStopsTheListener(t *testing.T) {
	port := freePort(t)
	l := NewListener(&recorder{}, port)
	t.Cleanup(l.Stop)

	if err := l.Toggle(true); err != nil {
		t.Fatal(err)
	}
	if l.Port() != port || !answersProbe(port) {
		t.Fatalf("switched on: Port() = %d, want %d and a probe answer", l.Port(), port)
	}
	if err := l.Toggle(true); err != nil {
		t.Errorf("switching a running listener on again: %v", err)
	}

	if err := l.Toggle(false); err != nil {
		t.Fatal(err)
	}
	if l.Port() != 0 || answersProbe(port) {
		t.Fatalf("switched off: Port() = %d and the port still answers", l.Port())
	}
}

func TestATakenPortIsReportedAndForgottenOnStop(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port

	l := NewListener(&recorder{}, port)
	t.Cleanup(l.Stop)
	if err := l.Start(); err == nil {
		t.Fatal("Start succeeded on a port another listener holds")
	}
	if l.Port() != 0 || l.Err() == nil {
		t.Fatalf("Port() = %d, Err() = %v; want 0 and the bind error", l.Port(), l.Err())
	}

	l.Stop()
	if l.Err() != nil {
		t.Errorf("Err() = %v after Stop; a listener switched off is not failing", l.Err())
	}
}
