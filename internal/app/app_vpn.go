package app

import (
	"log"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

// openVPN sets up the VPN the built-in torrent client goes through and takes
// up the provider that was in use. It needs the credential store, where the
// logins are sealed, and the engine, whose torrent config follows it.
func (a *App) openVPN() error {
	m, err := vpnconn.New(vpnconn.Options{
		Store:      a.Accounts,
		StatePath:  filepath.Join(a.DataDir, "vpn.json"),
		ManualPort: func() int { return a.Settings.Get().Torrent.Port },
		OnChange:   a.applyTorrentNetwork,
		Logf:       log.Printf,
	})
	if err != nil {
		return err
	}
	a.VPN = m
	m.Resume()
	return nil
}

// applyTorrentNetwork hands the engine the torrent config again, for a change
// of the VPN rather than of the settings.
func (a *App) applyTorrentNetwork() {
	if err := a.Engine.SetTorrentConfig(a.withVPN(torrentConfig(a.Settings.Get().Torrent))); err != nil {
		log.Printf("torrent network not applied: %v", err)
	}
}

// withVPN puts the torrent client on the VPN while a provider is in use, on
// the port it forwards once it does. A provider that is down keeps torrents
// waiting rather than letting them out over the machine's own connection.
func (a *App) withVPN(c engine.TorrentConfig) engine.TorrentConfig {
	if a.VPN == nil || a.VPN.Active() == "" {
		return c
	}
	c.Interface = vpnconn.NetworkName
	if p := a.VPN.ForwardedPort(); p > 0 {
		c.Port = p
	}
	return c
}
