package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/tsnetsrv"
)

// TestTsnetPeersEmptyWhenNotConnected: a fresh instance has never logged
// into Tailscale, so GET /api/tsnet/peers must answer 200 with an empty
// list, not an error - "nothing discovered yet" and "this route is broken"
// have to stay distinguishable.
func TestTsnetPeersEmptyWhenNotConnected(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tsnet/peers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tsnet/peers answered %d, want 200", resp.StatusCode)
	}
	var peers []tsnetsrv.PeerInstance
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		t.Fatalf("unparseable response: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("peers = %v, want none from a never-connected instance", peers)
	}
}

// TestTsnetStatusOffByDefault: registerAll wires a.Tsnet in, but a fresh
// instance with TsnetEnabled=false (the zero value) must never auto-start -
// applyTsnet's own guard is the one thing standing between "installed" and
// "silently phoning home to Tailscale".
func TestTsnetStatusOffByDefault(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tsnet/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info tsnetsrv.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Status != tsnetsrv.StatusOff {
		t.Fatalf("Status = %q on a fresh instance, want %q", info.Status, tsnetsrv.StatusOff)
	}
}
