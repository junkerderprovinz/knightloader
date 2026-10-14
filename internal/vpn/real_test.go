package vpn_test

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

func publicIP(t *testing.T, dial func(ctx context.Context, network, addr string) (net.Conn, error)) string {
	t.Helper()
	tr := &http.Transport{DialContext: dial}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	resp, err := c.Get("https://api.ipify.org")
	if err != nil {
		t.Fatalf("ipify: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// TestRealTunnel connects to the WireGuard server of the wg-quick file named
// by KL_VPN_REAL_CONFIG and checks what goes through it. KL_VPN_REAL_LAN is
// host:port of a service that answers with the address a connection came
// from, to see the tunnel leave elsewhere. KL_VPN_REAL_GATEWAY
// adds a round of NAT-PMP with that gateway, and KL_VPN_REAL_TORRENT=1
// downloads part of Debian's netinst image through the engine bound to the
// tunnel, moving its port and cutting the tunnel on the way, or the magnet
// in KL_VPN_REAL_MAGNET, which leaves out the web seeds.
func TestRealTunnel(t *testing.T) {
	path := os.Getenv("KL_VPN_REAL_CONFIG")
	if path == "" {
		t.Skip("KL_VPN_REAL_CONFIG is not set")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vpn.ParseConfig(string(text))
	if err != nil {
		t.Fatal(err)
	}
	tun := vpn.New(vpn.Options{Logf: t.Logf})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tun.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer tun.Disconnect()
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	direct := publicIP(t, d.DialContext)
	tunneled := publicIP(t, tun.DialContext)
	t.Logf("public address directly %s, through the tunnel %s", direct, tunneled)
	if direct == tunneled && os.Getenv("KL_VPN_REAL_SAME_EXIT") == "" {
		t.Error("the tunnel leaves from the same public address")
	}

	if lan := os.Getenv("KL_VPN_REAL_LAN"); lan != "" {
		seenFrom(t, tun, lan)
	}

	if gw := os.Getenv("KL_VPN_REAL_GATEWAY"); gw != "" {
		pf := &vpn.PortForward{Client: &vpn.NATPMP{Gateway: netip.MustParseAddr(gw), Dial: tun.DialContext}, Logf: t.Logf}
		pctx, pcancel := context.WithTimeout(context.Background(), 20*time.Second)
		go pf.Run(pctx)
		for pf.Port() == 0 && pctx.Err() == nil {
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("forwarded port %d", pf.Port())
		pcancel()
	}

	if os.Getenv("KL_VPN_REAL_TORRENT") != "" {
		torrentThroughTunnel(t, tun, cfg)
	}
}

// loaded follows one task's progress.
type loaded struct {
	mu   sync.Mutex
	last core.Update
}

func (l *loaded) set(u core.Update) {
	l.mu.Lock()
	l.last = u
	l.mu.Unlock()
}

func (l *loaded) get() core.Update {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last
}

func debianTorrent(t *testing.T) string {
	t.Helper()
	const dir = "https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/"
	resp, err := http.Get(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	name := regexp.MustCompile(`debian-[0-9.]+-amd64-netinst\.iso\.torrent`).Find(page)
	if name == nil {
		t.Fatal("no netinst torrent listed")
	}
	resp, err = http.Get(dir + string(name))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("torrent %s", name)
	return "data:application/x-bittorrent;base64," + base64.StdEncoding.EncodeToString(b)
}

func torrentThroughTunnel(t *testing.T, tun *vpn.Tunnel, cfg vpn.Config) {
	const name = "kl-vpn-real"
	if err := netbind.Register(name, tun); err != nil {
		t.Fatal(err)
	}
	defer netbind.Register(name, nil)
	tun.Watch(func(vpn.State) { netbind.Default.Refresh() })

	progress := &loaded{}
	e, err := engine.New(t.TempDir(), func(_ string, u core.Update) { progress.set(u) })
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.SetTorrentConfig(engine.TorrentConfig{Interface: name, Port: 51413}); err != nil {
		t.Fatal(err)
	}
	url := os.Getenv("KL_VPN_REAL_MAGNET")
	if url == "" {
		url = debianTorrent(t)
	}
	e.Start(engine.Job{TaskID: "debian", URL: url})

	waitLoaded := func(what string, past int64, within time.Duration) int64 {
		t.Helper()
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if u := progress.get(); u.Loaded > past {
				return u.Loaded
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("%s: still at %d bytes, last update %+v", what, past, progress.get())
		return 0
	}
	first := waitLoaded("first bytes through the tunnel", 4<<20, 3*time.Minute)
	t.Logf("%d bytes through the tunnel, %d B/s, binder %+v", first, progress.get().Speed, netbind.Default.State())

	if err := e.SetTorrentConfig(engine.TorrentConfig{Interface: name, Port: 51515}); err != nil {
		t.Fatal(err)
	}
	moved := waitLoaded("bytes after the port moved", first+(4<<20), 2*time.Minute)
	t.Logf("%d bytes after the port moved to 51515", moved)

	tun.Disconnect()
	time.Sleep(5 * time.Second)
	cut := progress.get().Loaded
	time.Sleep(15 * time.Second)
	if after := progress.get().Loaded; after != cut {
		t.Errorf("the download went on with the tunnel down: %d to %d bytes", cut, after)
	}
	t.Logf("held at %d bytes for 15 s with the tunnel down", cut)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tun.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := tun.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	resumed := waitLoaded("bytes once the tunnel is back", cut+(1<<20), 3*time.Minute)
	t.Logf("%d bytes once the tunnel was back", resumed)
}

// seenFrom reads what the service at addr, which answers with the address a
// connection came from, says of a direct connection and of one through the
// tunnel.
func seenFrom(t *testing.T, tun *vpn.Tunnel, addr string) {
	t.Helper()
	read := func(dial func(ctx context.Context, network, addr string) (net.Conn, error)) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := dial(ctx, "tcp4", addr)
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		defer c.Close()
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		b, _ := io.ReadAll(c)
		return strings.TrimSpace(string(b))
	}
	var d net.Dialer
	direct, tunneled := read(d.DialContext), read(tun.DialContext)
	t.Logf("%s saw a direct connection come from %s and one through the tunnel from %s", addr, direct, tunneled)
	if direct == tunneled {
		t.Error("the tunnel's connection came from this machine")
	}
}
