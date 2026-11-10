package api

// Where this instance answers on its network, and the multicast service its
// group finds the members on this network through. See kit/lan for the
// protocol.

import (
	"os"
	"strconv"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

func registerDiscovery(_ *Registry, a *app.App) {
	if svc := startDiscovery(a); svc != nil {
		a.SetDiscovery(svc)
	}
}

// networkAddress is where this instance answers on its network, or "" where
// nothing outside the machine can reach it.
//
// ListensWidely and not just a port: an instance started with
// KL_ADDR=127.0.0.1:8749, the documented way to run behind a local reverse
// proxy, has a port that nothing outside the box can reach. The multicast
// socket is separate from that listener, so without this check it would
// announce a LAN address it does not serve, and the members of its group
// would try it on every call.
func networkAddress() string {
	if buildinfo.Deployment == "desktop" || !buildinfo.ListensWidely || buildinfo.ListenPort <= 0 {
		return ""
	}
	ip := lan.LocalIPv4()
	if ip == "" {
		return ""
	}
	// http:// because that is what this process serves; a proxy terminating
	// TLS in front of it is what KnownDomains records.
	return "http://" + ip + ":" + strconv.Itoa(buildinfo.ListenPort) + buildinfo.BasePath
}

// groupAddress is the address this instance tells its group, which the
// Instances page shows and opens: the first known domain, since that works
// away from this network too, else its address on the network.
func groupAddress(a *app.App) string {
	for _, d := range a.Settings.Get().KnownDomains {
		if d = relay.FitAddress(d); d != "" {
			return d
		}
	}
	return networkAddress()
}

// startDiscovery starts the multicast service the group announces this
// instance and finds the others through. A build with no address to announce
// still listens, so a desktop reaches the server on its network directly even
// though nothing can dial the desktop back.
func startDiscovery(a *app.App) *lan.Service {
	// Only when a main package that serves has asked for it: see
	// buildinfo.DiscoveryEnabled for why this stays off in tests.
	if !buildinfo.DiscoveryEnabled {
		return nil
	}
	svc := lan.New(federation.LAN)
	svc.Start()
	return svc
}

// instanceDisplayName is InstanceName if the user set one, else os.Hostname,
// else "KnightLoader". One function rather than the same precedence written
// out twice: this instance announces itself both on the LAN and to its group
// (routes_relay.go's groupSelf), and two resolutions eventually disagree.
func instanceDisplayName(a *app.App) string {
	if name := a.Settings.Get().InstanceName; name != "" {
		return name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "KnightLoader"
}
