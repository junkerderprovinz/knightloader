package app

import (
	"context"
	"log"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/netproxy"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

// vpnRouteID is the connection a download carried through the VPN records.
const vpnRouteID = "vpn"

// openVPN sets up the VPN downloads can go through and takes up the provider
// that was in use. It needs the credential store, where the logins are
// sealed, and the engine, whose torrent config follows it.
func (a *App) openVPN() error {
	m, err := vpnconn.New(vpnconn.Options{
		Store:      a.Accounts,
		StatePath:  filepath.Join(a.DataDir, "vpn.json"),
		ManualPort: func() int { return a.Settings.Get().Torrent.Port },
		OnChange:   a.applyVPN,
		Logf:       log.Printf,
	})
	if err != nil {
		return err
	}
	// Downloads, yt-dlp and the debrid APIs reach the tunnel through a
	// loopback proxy of its own, which meters them like the engine's.
	px, err := netproxy.StartVia(a.Throttle, m.DialContext)
	if err != nil {
		return err
	}
	a.VPN, a.vpnProxy = m, px
	m.Resume()
	a.applyVPN()
	return nil
}

func (a *App) closeVPN() {
	if a.VPN != nil {
		a.VPN.Close()
	}
	httpx.SetClassProxy(httpx.ClassDebrid, nil)
	if a.vpnProxy != nil {
		_ = a.vpnProxy.Close()
	}
}

// applyVPN moves every kind of traffic to where the VPN switches put it.
func (a *App) applyVPN() {
	if err := a.Engine.SetTorrentConfig(a.withVPN(torrentConfig(a.Settings.Get().Torrent))); err != nil {
		log.Printf("torrent network not applied: %v", err)
	}
	var proxy *url.URL
	if a.through(vpnconn.Debrid) {
		proxy = a.vpnProxyURL()
	}
	httpx.SetClassProxy(httpx.ClassDebrid, proxy)
}

func (a *App) through(t vpnconn.Traffic) bool {
	return a.VPN != nil && a.vpnProxy != nil && a.VPN.Through(t)
}

func (a *App) vpnProxyURL() *url.URL {
	return &url.URL{Scheme: "http", Host: a.vpnProxy.Addr()}
}

// withVPN puts the torrent client on the VPN while torrents go through it, on
// the port it forwards once it does. A provider that is down keeps torrents
// waiting rather than letting them out over the machine's own connection.
func (a *App) withVPN(c engine.TorrentConfig) engine.TorrentConfig {
	if a.VPN == nil || !a.VPN.Through(vpnconn.Torrents) {
		return c
	}
	c.Interface = vpnconn.NetworkName
	if p := a.VPN.ForwardedPort(); p > 0 {
		c.Port = p
	}
	return c
}

// trafficOf is the VPN switch a task's download falls under, by the backend
// that fetches it.
func trafficOf(t *core.Task) vpnconn.Traffic {
	service, _, _ := strings.Cut(t.Resolver, resolver.AccountSep)
	switch service {
	case "torrent":
		return vpnconn.Torrents
	case "ytdlp":
		return vpnconn.Ytdlp
	case usenet.ResolverID:
		return vpnconn.Usenet
	}
	if s, ok := accounts.Lookup(service); ok && s.Group == accounts.GroupDebrid {
		return vpnconn.Debrid
	}
	return vpnconn.Hosters
}

// vpnRoute is the route of an engine download that goes through the VPN.
// Torrents are carried by the torrent client's own binding, and yt-dlp and
// Usenet by their own hooks.
func (a *App) vpnRoute(t *core.Task) (proxycfg.Route, bool) {
	switch tr := trafficOf(t); tr {
	case vpnconn.Hosters, vpnconn.Debrid:
		if a.through(tr) {
			return proxycfg.Route{ID: vpnRouteID, Scheme: "http", Host: a.vpnProxy.Addr()}, true
		}
	}
	return proxycfg.Route{}, false
}

// ytdlpProxy is the proxy yt-dlp goes through, "" for the machine's own
// connection.
func (a *App) ytdlpProxy() string {
	if !a.through(vpnconn.Ytdlp) {
		return ""
	}
	return a.vpnProxyURL().String()
}

// usenetDialer dials the own Usenet servers through the VPN while Usenet goes
// through it, decided on every connection.
type usenetDialer struct{ a *App }

func (d *usenetDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.a.through(vpnconn.Usenet) {
		return d.a.VPN.DialContext(ctx, network, address)
	}
	nd := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return nd.DialContext(ctx, network, address)
}
