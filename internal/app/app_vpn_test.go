package app

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

func TestEachDownloadFallsUnderItsVPNSwitch(t *testing.T) {
	cases := map[string]vpnconn.Traffic{
		"torrent":         vpnconn.Torrents,
		"ytdlp":           vpnconn.Ytdlp,
		"usenet":          vpnconn.Usenet,
		"realdebrid":      vpnconn.Debrid,
		"torbox#work":     vpnconn.Debrid,
		"direct":          vpnconn.Hosters,
		"http":            vpnconn.Hosters,
		"rapidgator":      vpnconn.Hosters,
		"hostheaders-xyz": vpnconn.Hosters,
	}
	for resolverID, want := range cases {
		if got := trafficOf(&core.Task{Resolver: resolverID}); got != want {
			t.Errorf("%s falls under %s, want %s", resolverID, got, want)
		}
	}
}

// connectedVPN brings up a provider in a, against a server in the process.
func connectedVPN(t *testing.T, a *App) *vpntest.Server {
	t.Helper()
	srv := vpntest.Start(t)
	if _, err := a.VPN.Connect("custom", map[string]string{"config": srv.Config()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for !customConnected(a) {
		if ctx.Err() != nil {
			t.Fatal("the VPN did not connect")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return srv
}

func customConnected(a *App) bool {
	for _, p := range a.VPN.State().Providers {
		if p.ID == "custom" {
			return p.State == "connected"
		}
	}
	return false
}

func TestTheVPNSwitchesSendEachKindOfDownloadThroughTheTunnel(t *testing.T) {
	a := newAccountsTestApp(t)
	connectedVPN(t, a)
	hoster := &core.Task{Resolver: "direct"}
	if _, ok := a.vpnRoute(hoster); ok {
		t.Error("a hoster download goes through the VPN with its switch off")
	}
	if a.ytdlpProxy() != "" {
		t.Error("yt-dlp goes through the VPN with its switch off")
	}

	a.VPN.SetRoutes(vpnconn.Routes{Torrents: true, Hosters: true, Ytdlp: true, Debrid: true, Usenet: true})
	r, ok := a.vpnRoute(hoster)
	if !ok || r.Host != a.vpnProxy.Addr() {
		t.Fatalf("hoster route %+v, want the VPN's proxy", r)
	}
	if _, ok := a.vpnRoute(&core.Task{Resolver: "torrent"}); ok {
		t.Error("a torrent was given a proxy route; the torrent client binds to the tunnel itself")
	}
	if a.ytdlpProxy() != "http://"+a.vpnProxy.Addr() {
		t.Errorf("yt-dlp proxy %q", a.ytdlpProxy())
	}

	// Through the proxy a request reaches the server's own port 80, which no
	// other way leads to.
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(a.vpnProxyURL())}, Timeout: 10 * time.Second}
	resp, err := client.Get("http://" + vpntest.ServerAddr.String() + "/")
	if err != nil {
		t.Fatalf("through the VPN's proxy: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != vpntest.ExitAddr {
		t.Errorf("the proxy answered %q", b)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := a.usenetDial.DialContext(ctx, "tcp", vpntest.ServerAddr.String()+":7")
	if err != nil {
		t.Fatalf("a Usenet connection does not go through the tunnel: %v", err)
	}
	c.Close()
}

func TestADownloadThroughADownVPNWaits(t *testing.T) {
	a := newAccountsTestApp(t)
	srv := connectedVPN(t, a)
	a.VPN.SetRoutes(vpnconn.Routes{Usenet: true})
	srv.Close()
	a.VPN.Disconnect()
	a.VPN.Connect("custom", map[string]string{"config": srv.Config()})

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := a.usenetDial.DialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil || time.Since(start) < 600*time.Millisecond {
		t.Errorf("a dial with the tunnel down ended after %s with %v; want it held until the caller gives up", time.Since(start), err)
	}
}
