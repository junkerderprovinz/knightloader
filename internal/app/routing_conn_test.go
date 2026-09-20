package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The wiring between proxycfg and dispatch: without a caller for NewPicker, a
// connection can be added, filtered, ordered and switched on while every
// download still leaves by the machine's own address.
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

// An install with no proxy configured is not handed a half-built route for one.
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

// A connection picked on one download is not overruled by the round-robin.
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
