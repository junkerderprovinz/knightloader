package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// TestConfiguredConnectionCarriesTheDownload is the wiring proxycfg was built
// for and did not have: NewPicker had no caller anywhere in the tree, so a user
// could add a connection, filter it, order it and switch it on, and every
// download still left by the machine's own address. The column meant to show
// which connection carried a task was blank for the same reason.
func TestConfiguredConnectionCarriesTheDownload(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		Connections: []proxycfg.Entry{{
			ID: "one", Kind: proxycfg.KindHTTP, Host: "proxy.invalid", Port: 8080, Enabled: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	task := &core.Task{ID: "x", URL: "https://host.example/f.bin", Enabled: true}
	a.mu.Lock()
	route, id := a.routeForLocked(task, "host.example")
	a.mu.Unlock()

	if id != "one" {
		t.Fatalf("connection = %q, want the configured one", id)
	}
	if route.Host != "proxy.invalid:8080" {
		t.Errorf("route host = %q, want the proxy's", route.Host)
	}
}

// TestNoConnectionsMeansTheMachineItself: the ordinary install has configured
// no proxy, and it must not be handed a half-built route for one.
func TestNoConnectionsMeansTheMachineItself(t *testing.T) {
	a := newQueueApp(t)
	task := &core.Task{ID: "x", URL: "https://host.example/f.bin", Enabled: true}
	a.mu.Lock()
	route, id := a.routeForLocked(task, "host.example")
	a.mu.Unlock()

	if id != "" || route.Host != "" {
		t.Errorf("got connection %q host %q; with no connections configured both must be empty", id, route.Host)
	}
}

// TestTheTaskOwnChoiceWins: picking a connection on one download is the point of
// per-download routing, so the round-robin must not overrule it.
func TestTheTaskOwnChoiceWins(t *testing.T) {
	a := newQueueApp(t)
	if _, err := a.ApplySettings(settings.Settings{
		MaxConcurrent: 4, MaxPerHost: 4, DownloadDir: t.TempDir(),
		Connections: []proxycfg.Entry{
			{ID: "a", Kind: proxycfg.KindHTTP, Host: "a.invalid", Port: 1, Enabled: true},
			{ID: "b", Kind: proxycfg.KindHTTP, Host: "b.invalid", Port: 2, Enabled: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	task := &core.Task{ID: "x", URL: "https://host.example/f.bin", Enabled: true, Connection: "b"}
	a.mu.Lock()
	route, id := a.routeForLocked(task, "host.example")
	a.mu.Unlock()

	if id != "b" || route.Host != "b.invalid:2" {
		t.Errorf("got %q / %q, want the connection the task named", id, route.Host)
	}
}
