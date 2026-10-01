package main

import (
	"bytes"
	"net/http"
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
