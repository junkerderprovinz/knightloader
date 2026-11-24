package api

import (
	"net/netip"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// instanceKind is what this instance tells the group and the Instances page it
// is: "desktop" for the desktop app, "container" for the server in a container
// and "server" for the server run as a plain program. buildinfo.Deployment
// names the binary only, and both of the last two run the same one.
func instanceKind() string {
	if buildinfo.Deployment == "container" && !buildinfo.InContainer() {
		return "server"
	}
	return buildinfo.Deployment
}

// trustedProxies is the setting of that name, read on every request so a
// change applies at once.
func trustedProxies(a *app.App) func() []netip.Prefix {
	return func() []netip.Prefix { return a.Settings.Get().TrustedProxyPrefixes() }
}
