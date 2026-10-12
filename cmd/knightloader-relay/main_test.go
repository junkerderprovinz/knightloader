package main

import (
	"bytes"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

func TestVersionFlagPrintsVersionAndCommit(t *testing.T) {
	defer func(v, c string) { buildinfo.Version, buildinfo.Commit = v, c }(buildinfo.Version, buildinfo.Commit)
	buildinfo.Version, buildinfo.Commit = "v1.2.3", "0123abcd"

	var out, errOut bytes.Buffer
	versionOnly, err := parseArgs([]string{"-version"}, &out, &errOut)
	if err != nil || !versionOnly {
		t.Fatalf("parseArgs(-version) = %v, %v; want true, nil", versionOnly, err)
	}
	if got := strings.TrimSpace(out.String()); got != "knightloader-relay v1.2.3 (commit 0123abcd)" {
		t.Fatalf("printed %q", got)
	}
}

// TestASilentConnectionIsBoundedButAnOpenSocketIsNot: without a header
// timeout net/http sets no TLS handshake deadline either, and a read or write
// timeout would cut every relay socket after that long.
func TestASilentConnectionIsBoundedButAnOpenSocketIsNot(t *testing.T) {
	srv := newServer(http.NotFoundHandler(), nil)
	if srv.ReadHeaderTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("header timeout %v, idle timeout %v; want both set", srv.ReadHeaderTimeout, srv.IdleTimeout)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Errorf("read timeout %v, write timeout %v; want neither, they would cut the relay's sockets", srv.ReadTimeout, srv.WriteTimeout)
	}
}

func TestNoArgumentsStartsTheRelay(t *testing.T) {
	var out, errOut bytes.Buffer
	versionOnly, err := parseArgs(nil, &out, &errOut)
	if err != nil || versionOnly {
		t.Fatalf("parseArgs() = %v, %v; want false, nil", versionOnly, err)
	}
	if out.Len() > 0 {
		t.Fatalf("printed %q", out.String())
	}
}

func TestUnknownFlagOrArgumentIsRefused(t *testing.T) {
	for _, args := range [][]string{{"-verison"}, {"--port", "80"}, {"serve"}} {
		var out, errOut bytes.Buffer
		if _, err := parseArgs(args, &out, &errOut); err == nil {
			t.Errorf("parseArgs(%q) accepted it", args)
		}
		if !strings.Contains(errOut.String(), "-version") {
			t.Errorf("parseArgs(%q) printed no usage: %q", args, errOut.String())
		}
	}
}

func TestTrustedProxiesReadsAddressesAndRanges(t *testing.T) {
	got, err := trustedProxies(" 172.17.0.1, 10.0.0.0/8 ,::1")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("172.17.0.1/32"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("trustedProxies = %v, want %v", got, want)
	}
	if got, _ := trustedProxies("none"); got != nil {
		t.Errorf(`trustedProxies("none") = %v, want none`, got)
	}
	unset, _ := trustedProxies("")
	for _, addr := range []string{"127.0.0.1", "::1"} {
		if !slices.ContainsFunc(unset, func(p netip.Prefix) bool { return p.Contains(netip.MustParseAddr(addr)) }) {
			t.Errorf("unset does not trust %s on the relay's own host", addr)
		}
	}
	for _, addr := range []string{"10.0.0.7", "172.17.0.2", "192.168.1.20", "fd00::7"} {
		if slices.ContainsFunc(unset, func(p netip.Prefix) bool { return p.Contains(netip.MustParseAddr(addr)) }) {
			t.Errorf("unset trusts %s, so any machine on that network picks the address the throttle counts", addr)
		}
	}
	if _, err := trustedProxies("proxy.lan"); err == nil {
		t.Error("a host name was taken for a trusted proxy")
	}
}

func TestTheRelayGoesByItsPublicAddressAndItsDomains(t *testing.T) {
	got, err := relayNames(" https://Relay.Example.com/relay/connect, wss://old.example.com:8443 ", []string{"relay.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"relay.example.com", "old.example.com", "relay.example.org"}; !slices.Equal(got, want) {
		t.Errorf("relayNames = %v, want %v", got, want)
	}
	if got, _ := relayNames("", nil); len(got) != 0 {
		t.Errorf("with nothing set the relay goes by %v, want the Host header", got)
	}
	if _, err := relayNames("https://", nil); err == nil {
		t.Error("an address without a host was taken")
	}
}
