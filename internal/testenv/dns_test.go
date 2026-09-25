package testenv

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

func withNoDNS(t *testing.T) {
	t.Helper()
	orig := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = orig })
	NoDNS()
}

func TestNoDNSResolvesLocalhostToLoopback(t *testing.T) {
	withNoDNS(t)

	addrs, err := net.DefaultResolver.LookupHost(context.Background(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(addrs, "127.0.0.1") || !slices.Contains(addrs, "::1") {
		t.Errorf("localhost resolved to %v, want 127.0.0.1 and ::1", addrs)
	}
}

// The app classifies a failed download by the error it gets, so the answer
// has to be the "no such host" a real resolver gives for a reserved name.
func TestNoDNSSaysEveryOtherNameDoesNotExist(t *testing.T) {
	withNoDNS(t)

	for _, name := range []string{"host.example", "api.real-debrid.com"} {
		_, err := net.DefaultResolver.LookupHost(context.Background(), name)
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			t.Errorf("looking up %s gave %v, want a not-found DNS error", name, err)
		}
	}
}

// Tests that dial a local server by name reach it through the ordinary
// dialer, which is what the icon fetcher's address check relies on.
func TestNoDNSLetsADialerReachALocalServerByName(t *testing.T) {
	withNoDNS(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	c, err := (&net.Dialer{}).DialContext(context.Background(), "tcp4", net.JoinHostPort("localhost", port))
	if err != nil {
		t.Fatalf("dialing localhost:%s: %v", port, err)
	}
	c.Close()
}
