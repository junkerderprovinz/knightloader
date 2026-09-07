// Package testenv holds the one decision every test that opens a socket on a
// real network interface has to make, so it is made once instead of per test.
//
// It exists only for tests. Nothing in the built binaries imports it, which is
// why importing "testing" here is harmless: the flags that import registers
// only ever land in test binaries.
package testenv

import (
	"os"
	"testing"
)

// NetTestsEnv is the switch that turns the wide-listening tests back on.
const NetTestsEnv = "KNIGHTLOADER_NET_TESTS"

// RequireWideListener skips the test unless KNIGHTLOADER_NET_TESTS is set.
//
// A few tests here have to open a socket that is reachable from the network
// rather than from loopback: discovery joins a multicast group on every
// interface, and the torrent tests start a real BitTorrent client. Both are
// worth having, and neither can be written against 127.0.0.1 without testing
// something other than the thing that ships.
//
// The cost is paid on the developer's machine, not in the code: Windows asks
// "allow this app through the firewall?" for every such binary, once per build,
// and a build with a new hash is a new binary. Running the full suite therefore
// meant a stack of dialogs, each one blocking until it was clicked away.
//
// So the default is quiet and CI opts in (see .github/workflows/ci.yml, which
// sets the variable for every test job). That keeps the coverage where the
// dialog cannot exist - a Linux runner - and keeps a plain `go test ./...` on a
// workstation from stopping to ask about the network.
//
// Running them locally on purpose:
//
//	KNIGHTLOADER_NET_TESTS=1 go test ./internal/discovery/...
func RequireWideListener(t *testing.T) {
	t.Helper()
	if !wideListenersWanted() {
		t.Skipf("listens on a real network interface; set %s=1 to run it", NetTestsEnv)
	}
}

// wideListenersWanted is the decision on its own, so it can be tested in both
// directions. Asserting on RequireWideListener itself would mean asserting that
// a test skipped, which a test cannot observe about itself - and a gate that is
// only ever exercised in its skipping direction is exactly how CI ends up green
// because it ran nothing.
func wideListenersWanted() bool {
	return os.Getenv(NetTestsEnv) != ""
}
