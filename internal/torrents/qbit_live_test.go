package torrents_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// liveQBit runs the qBittorrent named in KL_QBITTORRENT_NOX with a profile
// whose first start reaches nothing past this machine.
func liveQBit(t *testing.T) *qbittorrent.Client {
	t.Helper()
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	profile := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(nox.ConfigPath(profile)), 0o700); err != nil {
		t.Fatal(err)
	}
	ini := strings.Join([]string{
		"[Preferences]",
		`Win32\NeverCheckFileAssocation=true`,
		`General\StartMinimized=true`,
		`General\NoSplashScreen=true`,
		`Advanced\updateCheck=false`,
		"[Network]",
		"PortForwardingEnabled=false",
		"[BitTorrent]",
		`Session\DHTEnabled=false`,
		`Session\LSDEnabled=false`,
		"",
	}, "\r\n")
	if err := os.WriteFile(nox.ConfigPath(profile), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	in := nox.Start(nox.Options{Binary: bin, Profile: profile, Logf: t.Logf})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// An interface picked by the name the system gives it reaches a real
// qBittorrent as the name qBittorrent lists it by: on Windows the adapter's
// internal name, elsewhere the same name.
func TestARealQBittorrentIsTiedToTheInterfaceTheSystemNames(t *testing.T) {
	c := liveQBit(t)
	ctx := context.Background()
	list, err := c.NetworkInterfaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("qBittorrent lists %+v", list)
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, ifc := range ifs {
		var want string
		for _, i := range list {
			if i.Name == ifc.Name || i.Value == ifc.Name {
				want = i.Value
			}
		}
		if want == "" {
			continue
		}
		tried++
		if err := torrents.QBit(func(context.Context) (*qbittorrent.Client, error) { return c, nil }).Configure(ctx, torrents.Prefs{Interface: ifc.Name}); err != nil {
			t.Fatal(err)
		}
		p, err := c.Preferences(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if p["current_network_interface"] != want {
			t.Errorf("%q reached qBittorrent as %v, want %q", ifc.Name, p["current_network_interface"], want)
		}
	}
	if tried == 0 {
		t.Fatalf("no interface of the system is on qBittorrent's list %+v", list)
	}
}
