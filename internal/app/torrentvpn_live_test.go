package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/socks5"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

// quietProfile is a qBittorrent profile whose first start reaches nothing past
// this machine: no peer discovery, no port mapping, no country database, and
// on Windows none of the window's questions before the WebUI comes up.
func quietProfile(t *testing.T, profile string) {
	t.Helper()
	conf := filepath.Dir(nox.ConfigPath(profile))
	if err := os.MkdirAll(conf, 0o700); err != nil {
		t.Fatal(err)
	}
	ini := strings.Join([]string{
		"[Preferences]",
		`Win32\NeverCheckFileAssocation=true`,
		`General\StartMinimized=true`,
		`General\NoSplashScreen=true`,
		`Advanced\updateCheck=false`,
		`Connection\ResolvePeerCountries=false`,
		"[Network]",
		"PortForwardingEnabled=false",
		"[BitTorrent]",
		`Session\DHTEnabled=false`,
		`Session\LSDEnabled=false`,
		`Session\PeXEnabled=false`,
		`Session\MultiConnectionsPerIp=true`,
		"",
	}, "\r\n")
	if err := os.WriteFile(nox.ConfigPath(profile), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
}

// liveSeeder runs a second qBittorrent, on the machine's own connection, that
// seeds what a test hands it.
func liveSeeder(t *testing.T, bin string) (*qbittorrent.Client, int) {
	t.Helper()
	profile := t.TempDir()
	quietProfile(t, profile)
	in := nox.Start(nox.Options{Binary: bin, Profile: profile, Logf: t.Logf})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := p["listen_port"].(float64)
	return c, int(port)
}

// recordingTunnel stands in for the VPN's tunnel at the far end of the
// torrent proxy. It records every destination it is asked for and reaches
// only 127.0.0.1, where the test's swarm runs; a name it cannot resolve.
type recordingTunnel struct {
	mu    sync.Mutex
	asked []string
}

func (r *recordingTunnel) note(s string) {
	r.mu.Lock()
	r.asked = append(r.asked, s)
	r.mu.Unlock()
}

func (r *recordingTunnel) destinations() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.asked)
}

var errNotLocal = errors.New("the test's tunnel reaches 127.0.0.1 only")

func (r *recordingTunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	r.note(network + " " + address)
	ap, err := netip.ParseAddrPort(address)
	if err != nil || ap.Addr() != netip.MustParseAddr("127.0.0.1") {
		return nil, errNotLocal
	}
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func (r *recordingTunnel) ListenPacket(ctx context.Context, network string) (net.PacketConn, error) {
	pc, err := net.ListenPacket(network, "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &recordingPacketConn{PacketConn: pc, r: r}, nil
}

func (r *recordingTunnel) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.note("lookup " + host)
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

type recordingPacketConn struct {
	net.PacketConn
	r *recordingTunnel
}

func (c *recordingPacketConn) WriteTo(p []byte, to net.Addr) (int, error) {
	c.r.note("udp " + to.String())
	return c.PacketConn.WriteTo(p, to)
}

// localTracker answers every announce with the peers given, as compact
// 127.0.0.1 addresses.
func localTracker(t *testing.T, ports ...int) string {
	t.Helper()
	var peers []byte
	for _, p := range ports {
		peers = append(peers, 127, 0, 0, 1)
		peers = binary.BigEndian.AppendUint16(peers, uint16(p))
	}
	answer, err := bencode.Encode(map[string]any{"interval": 30, "peers": peers})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(answer) })}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String() + "/announce"
}

// tcpOf lists the TCP sockets of process pid, as Windows reports them: "local
// remote state", or "local" alone for one that listens.
func tcpOf(t *testing.T, pid int) []string {
	t.Helper()
	script := fmt.Sprintf(`Get-NetTCPConnection -OwningProcess %d -ErrorAction SilentlyContinue | `+
		`ForEach-Object { if ($_.State -eq 'Listen') { '{0}:{1}' -f $_.LocalAddress,$_.LocalPort } `+
		`else { '{0}:{1} {2}:{3} {4}' -f $_.LocalAddress,$_.LocalPort,$_.RemoteAddress,$_.RemotePort,$_.State } }`, pid)
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		t.Fatalf("the connections of %d could not be listed: %v", pid, err)
	}
	return strings.Fields(strings.ReplaceAll(string(out), "\r", ""))
}

// strayConnections are the TCP sockets of qBittorrent's process pid that
// neither listen on loopback, nor reach the proxy or the WebUI, nor join two
// ends of qBittorrent itself.
func strayConnections(t *testing.T, pid, proxyPort, webPort int) []string {
	t.Helper()
	fields := tcpOf(t, pid)
	var rows [][]string
	own := map[string]bool{}
	for i := 0; i < len(fields); {
		if i+2 < len(fields) && strings.Contains(fields[i+1], ":") && !strings.Contains(fields[i+2], ":") {
			rows = append(rows, fields[i:i+3])
			own[fields[i]] = true
			i += 3
			continue
		}
		rows = append(rows, fields[i:i+1])
		i++
	}
	proxy := "127.0.0.1:" + strconv.Itoa(proxyPort)
	web := "127.0.0.1:" + strconv.Itoa(webPort)
	var stray []string
	for _, r := range rows {
		switch {
		case len(r) == 1 && r[0] == web:
		case len(r) == 3 && r[2] == "Bound":
		case len(r) == 3 && (r[1] == proxy || r[0] == web || own[r[1]]):
		default:
			stray = append(stray, strings.Join(r, " "))
		}
	}
	return stray
}

// A torrent fetched while torrents go through the built-in VPN reaches its
// tracker, its peer and its metadata through the torrent proxy and the
// tunnel only. KnightLoader runs the qBittorrent named in KL_QBITTORRENT_NOX
// as it always does; the provider is the test VPN of vpntest, and the far end
// of the tunnel is one that records every destination, since a test cannot
// reach real peers. On Windows the test also lists qBittorrent's own TCP
// sockets while the torrent arrives: every one reaches the proxy, the WebUI
// or qBittorrent itself, and nothing listens beyond loopback.
func TestATorrentThroughTheBuiltInVPNReachesNothingButTheTunnel(t *testing.T) {
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	tun := &recordingTunnel{}
	was := torrentTunnel
	torrentTunnel = func(*App) socks5.Tunnel { return tun }
	t.Cleanup(func() { torrentTunnel = was })

	seeder, seedPort := liveSeeder(t, bin)
	src := t.TempDir()
	raw, hash := testenv.MakeTorrent(t, src, "Pack", map[string]int{"a.bin": 3 << 20, "b.bin": 5000})
	if err := seeder.Add(context.Background(), qbittorrent.Torrent{File: raw, SavePath: src, Folder: true}); err != nil {
		t.Fatal(err)
	}
	tracker := localTracker(t, seedPort)
	const udpTracker = "udp://tracker.knightloader.invalid:6969/announce"

	dataDir := t.TempDir()
	quietProfile(t, filepath.Join(dataDir, "qbittorrent"))
	a, err := newApp(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	downloads := t.TempDir()
	s := a.Settings.Get()
	s.DownloadDir = downloads
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	vs := vpntest.Start(t)
	if _, err := a.VPN.Connect("protonvpn", map[string]string{"config": vs.Config()}); err != nil {
		t.Fatal(err)
	}
	a.VPN.SetRoutes(vpnconn.Routes{Torrents: true})
	waitFor(t, "the tunnel", a.VPN.Up)

	a.fetchQBit = func(context.Context, nox.Version, func(done, total int64)) error { return nil }
	a.runQBit = func(v nox.Version) qbitRunner {
		o := a.qbitOptions(v)
		o.Binary = bin
		return nox.Start(o)
	}
	a.StartQBittorrent()
	a.UseTorrents(torrents.QBit(a.qbitClient))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	qb, err := a.qbitClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := qb.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("KnightLoader's qBittorrent: proxy type %v at %v:%v, interface %q, dht %v, protocol %v, anonymous %v",
		p["proxy_type"], p["proxy_ip"], p["proxy_port"], p["current_network_interface"], p["dht"], p["bittorrent_protocol"], p["anonymous_mode"])
	if p["proxy_port"] != float64(a.torrentProxy.Port()) || p["dht"] != false || p["upnp"] != false || p["lsd"] != false {
		t.Fatalf("qBittorrent is not set to go through the torrent proxy: %v", p)
	}

	magnet := "magnet:?xt=urn:btih:" + hash + "&dn=Pack&tr=" + url.QueryEscape(tracker) + "&tr=" + url.QueryEscape(udpTracker)
	created := a.AddLinksFrom([]string{magnet}, "VPN", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("%d tasks staged", len(created))
	}
	id := created[0].ID
	a.StartTasks([]string{id})

	st := a.qbitRun.Status()
	var stray []string
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if runtime.GOOS == "windows" {
			stray = append(stray, strayConnections(t, st.PID, a.torrentProxy.Port(), st.Port)...)
		}
		task, _ := a.taskSnapshot(id)
		if task.Status == core.StatusError {
			t.Fatalf("the task failed: %s", task.Error)
		}
		if task.Status == core.StatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not done: %s, %d of %d; the tunnel was asked for %v", task.Status, task.Loaded, task.Size, tun.destinations())
		}
		time.Sleep(500 * time.Millisecond)
	}
	if runtime.GOOS == "windows" {
		stray = append(stray, strayConnections(t, st.PID, a.torrentProxy.Port(), st.Port)...)
		t.Logf("qBittorrent (PID %d) at the end: %v", st.PID, tcpOf(t, st.PID))
	}
	if len(stray) > 0 {
		t.Errorf("qBittorrent had sockets outside the proxy: %v", stray)
	}

	got, err := os.ReadFile(filepath.Join(downloads, "VPN", "Pack", "b.bin"))
	if err != nil {
		got, err = os.ReadFile(filepath.Join(downloads, "Pack", "b.bin"))
	}
	if err != nil || string(got) != string(testenv.Content("b.bin", 5000)) {
		t.Errorf("b.bin: %d bytes, %v", len(got), err)
	}
	asked := tun.destinations()
	t.Logf("the tunnel was asked for: %v", asked)
	trackerAddr := strings.TrimSuffix(strings.TrimPrefix(tracker, "http://"), "/announce")
	for _, want := range []string{
		"tcp " + trackerAddr,
		"tcp 127.0.0.1:" + strconv.Itoa(seedPort),
		"lookup tracker.knightloader.invalid",
	} {
		if !slices.Contains(asked, want) {
			t.Errorf("the tunnel was never asked for %s", want)
		}
	}
	if runtime.GOOS == "windows" {
		out, _ := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"Get-DnsClientCache -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Entry").Output()
		if strings.Contains(string(out), "knightloader.invalid") {
			t.Error("the UDP tracker's name was looked up on the machine's own name servers")
		}
	}
}

// liveApp is an app that runs the qBittorrent named in KL_QBITTORRENT_NOX as
// its own, the way it runs the one it fetches.
func liveApp(t *testing.T) (*App, *qbittorrent.Client) {
	t.Helper()
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	dataDir := t.TempDir()
	quietProfile(t, filepath.Join(dataDir, "qbittorrent"))
	a, err := newApp(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	a.fetchQBit = func(context.Context, nox.Version, func(done, total int64)) error { return nil }
	a.runQBit = func(v nox.Version) qbitRunner {
		o := a.qbitOptions(v)
		o.Binary = bin
		return nox.Start(o)
	}
	a.StartQBittorrent()
	a.UseTorrents(torrents.QBit(a.qbitClient))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	qb, err := a.qbitClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return a, qb
}

// After a restart qBittorrent runs nothing KnightLoader has not taken over: a
// torrent whose task is on the list waits, stopped, for its task to start it,
// and one whose task is gone is taken out without its files.
func TestARealQBittorrentRunsNothingOfItsOwnAfterARestart(t *testing.T) {
	a, qb := liveApp(t)
	ctx := context.Background()
	src := t.TempDir()
	rawKept, kept := testenv.MakeTorrent(t, src, "Kept", map[string]int{"a.bin": 40000})
	rawGone, gone := testenv.MakeTorrent(t, src, "Gone", map[string]int{"b.bin": 40000})
	for _, raw := range [][]byte{rawKept, rawGone} {
		if err := qb.Add(ctx, qbittorrent.Torrent{File: raw, SavePath: src, Folder: true, Tags: []string{torrents.Tag}}); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	a.tasks["kept"] = &core.Task{ID: "kept", Name: "Kept", Resolver: "torrent", Status: core.StatusDone, InfoHash: kept, File: filepath.Join(src, "Kept")}
	a.mu.Unlock()
	waitFor(t, "both seeding", func() bool {
		list, _ := qb.Torrents(ctx, qbittorrent.Filter{Tag: torrents.Tag})
		return len(list) == 2 && strings.HasSuffix(list[0].State, "UP") && strings.HasSuffix(list[1].State, "UP") &&
			!strings.HasPrefix(list[0].State, "paused") && !strings.HasPrefix(list[1].State, "paused")
	})

	a.stopQBit()
	a.StartQBittorrent()
	ctx2, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	qb, err := a.qbitClient(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	// It reads what it kept in the background, and says only then how a
	// torrent stands.
	var list []qbittorrent.Info
	waitFor(t, "qBittorrent's own check", func() bool {
		list, err = qb.Torrents(ctx, qbittorrent.Filter{})
		return err == nil && !slices.ContainsFunc(list, func(i qbittorrent.Info) bool { return strings.HasPrefix(i.State, "checking") })
	})
	if len(list) != 1 || list[0].Hash != kept {
		t.Fatalf("after the restart qBittorrent holds %+v, want the kept one alone", list)
	}
	if st := list[0].State; !strings.HasPrefix(st, "paused") && !strings.HasPrefix(st, "stopped") {
		t.Errorf("the kept torrent is %s before its task started it", st)
	}
	t.Logf("after the restart: %s is %s, %s is gone", list[0].Name, list[0].State, gone)
	if _, err := os.Stat(filepath.Join(src, "Gone", "b.bin")); err != nil {
		t.Errorf("the files of the one taken out went too: %v", err)
	}
}
