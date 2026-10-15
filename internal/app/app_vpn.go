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
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

// vpnRouteID is the connection a download carried through the VPN records.
const vpnRouteID = "vpn"

// vpnClasses are the requests of the app's own that each VPN switch carries
// along with the downloads.
var vpnClasses = map[vpnconn.Traffic]httpx.Class{
	vpnconn.Hosters: httpx.ClassHosters,
	vpnconn.Debrid:  httpx.ClassDebrid,
	vpnconn.Ytdlp:   httpx.ClassYtdlp,
	vpnconn.Usenet:  httpx.ClassUsenet,
}

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
	// Downloads, yt-dlp and the app's own requests reach the tunnel through a
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
	for _, c := range vpnClasses {
		httpx.SetClassProxy(c, nil)
	}
	if a.vpnProxy != nil {
		_ = a.vpnProxy.Close()
	}
}

// applyVPN moves every kind of traffic to where the VPN switches put it. The
// engine's transfers follow at their next progress report (see keepOnRoute).
func (a *App) applyVPN() {
	if err := a.Engine.SetTorrentConfig(a.withVPN(torrentConfig(a.Settings.Get().Torrent))); err != nil {
		log.Printf("torrent network not applied: %v", err)
	}
	for tr, c := range vpnClasses {
		var proxy *url.URL
		if a.through(tr) {
			proxy = a.vpnProxyURL()
		}
		httpx.SetClassProxy(c, proxy)
	}
	// The Usenet servers' connections were dialled the way Usenet went then.
	if on := a.through(vpnconn.Usenet); a.usenetVPN.Swap(on) != on {
		a.redialNNTP()
	}
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
	case usenet.ResolverID, local.ResolverID:
		return vpnconn.Usenet
	}
	if s, ok := accounts.Lookup(service); ok && s.Group == accounts.GroupDebrid {
		return vpnconn.Debrid
	}
	return vpnconn.Hosters
}

// vpnRoute is the route of an engine download that goes through the VPN: a
// hoster's, a debrid service's, or a file a Usenet service fetched. Torrents
// are carried by the torrent client's own binding, and yt-dlp and the own
// Usenet servers by their own hooks.
func (a *App) vpnRoute(t *core.Task) (proxycfg.Route, bool) {
	switch tr := trafficOf(t); tr {
	case vpnconn.Hosters, vpnconn.Debrid, vpnconn.Usenet:
		if a.through(tr) {
			return proxycfg.Route{ID: vpnRouteID, Scheme: "http", Host: a.vpnProxy.Addr()}, true
		}
	}
	return proxycfg.Route{}, false
}

// misroutedLocked returns the route t's engine transfer belongs on when a VPN
// switch has changed since it started: into the tunnel once its switch is on,
// back to its own connection once the switch is off. Caller holds a.mu.
func (a *App) misroutedLocked(t *core.Task) (proxycfg.Route, string, bool) {
	r, through := a.vpnRoute(t)
	if through == (t.Connection == vpnRouteID) {
		return proxycfg.Route{}, "", false
	}
	if through {
		return r, vpnRouteID, true
	}
	r, chosen := a.routeForLocked(t, hostOf(t.URL))
	return r, chosen, true
}

// keepOnRoute moves the engine transfer engineID onto the route its task's
// VPN switch asks for. The engine reports a running transfer twice a second,
// so after a switch none goes on where it went for longer than that. engineID
// is the task's own id, or that of the file of a torrent a debrid service
// fetched.
func (a *App) keepOnRoute(engineID string) {
	taskID := a.torrentFiles.taskOf(engineID)
	a.mu.Lock()
	t := a.tasks[taskID]
	if t == nil || !a.active[taskID] || a.rerouting[engineID] {
		a.mu.Unlock()
		return
	}
	r, chosen, misrouted := a.misroutedLocked(t)
	if misrouted {
		a.rerouting[engineID] = true
	}
	a.mu.Unlock()
	if misrouted {
		a.spawn(func() { a.reroute(taskID, engineID, r, chosen) })
	}
}

// reroute has the engine move the transfer engineID onto r, and records the
// connection on the task once it has. A transfer the engine cannot move yet is
// tried again at its next report or resume.
func (a *App) reroute(taskID, engineID string, r proxycfg.Route, chosen string) {
	moved := a.Engine.Reroute(engineID, r)
	a.mu.Lock()
	delete(a.rerouting, engineID)
	t := a.tasks[taskID]
	if !moved || t == nil {
		a.mu.Unlock()
		return
	}
	t.Connection = chosen
	c := a.copyLocked(t)
	a.mu.Unlock()
	a.publish(&c)
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
