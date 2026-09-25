package api

// The instances on this network, found with nothing configured. The relay
// solves reaching an instance that cannot be reached; for a server and a
// desktop on one home network the missing piece is only the address. See
// internal/discovery for the protocol and for why this pairs nothing by
// itself.

import (
	"net/http"
	"os"
	"strconv"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// discovered is one instance seen on the network, as the Instances page wants
// it: enough to show a row and to fill in the "add" form, and a flag saying
// whether it is already known so the page can leave those out.
type discovered struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Deployment is "container" or "desktop". A desktop build has no address
	// anything could dial and never announces, so this is "container" in
	// practice; it is carried because the announce carries it.
	Deployment string `json:"deployment"`
	// Known is true when this instance is already a stored or relay peer, by
	// name or by address. The page shows it greyed rather than hiding it: "the
	// one I expected is missing" and "it is here and already added" are
	// different answers.
	Known bool `json:"known"`
}

func registerDiscovery(reg *Registry, a *app.App) {
	svc := startDiscovery(a)
	if svc != nil {
		a.SetDiscovery(svc)
		// Rebuilt on a settings save, so renaming an instance reaches the
		// network on the next announce rather than after a restart. Same shape
		// as applyRelay in routes_settings.go.
		reg.refreshDiscovery = func() { svc.SetSelf(discoverySelf(a)) }
	}

	reg.Add(http.MethodGet, "/api/discovery",
		"the KnightLoader instances announcing themselves on this network right now",
		func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				// Not an error: a host with multicast blocked is an ordinary
				// host. An empty list says "none found", which is the truth.
				writeJSON(w, []discovered{})
				return
			}
			known := map[string]bool{}
			for _, in := range a.Federation.List() {
				known[in.Name] = true
				if in.URL != "" {
					known[in.URL] = true
				}
				if in.RelayID != "" {
					known[in.RelayID] = true
				}
			}
			out := []discovered{}
			for _, p := range svc.Peers() {
				// Sanitised on arrival as well as on the way out: the sender
				// is whatever is on the network, and a name this side cannot
				// add is a row with a button that can only fail.
				name := federation.SanitiseName(p.Name)
				if name == "" {
					name = federation.SanitiseName(p.ID)
				}
				if name == "" {
					continue // nothing addressable; not worth a row
				}
				out = append(out, discovered{
					ID:         p.ID,
					Name:       name,
					URL:        p.URL,
					Deployment: p.Deployment,
					Known:      known[p.ID] || known[p.Name] || known[p.URL],
				})
			}
			writeJSON(w, out)
		})
}

// discoverySelf is what this instance announces right now.
func discoverySelf(a *app.App) discovery.Peer {
	// A receiving instance adds the peer by this name, and federation's naming
	// rule is narrower than what a person may have called their box. Without
	// sanitising, the card offers an Add button that can only answer "invalid
	// instance name".
	name := federation.SanitiseName(instanceDisplayName(a))
	if name == "" {
		name = a.Settings.Get().InstanceID
	}
	self := discovery.Peer{
		ID:         a.Settings.Get().InstanceID,
		Name:       name,
		Deployment: buildinfo.Deployment,
	}
	// ListensWidely and not just a port: an instance started with
	// KL_ADDR=127.0.0.1:8749, the documented way to run behind a local reverse
	// proxy, has a port that nothing outside the box can reach. The multicast
	// socket is separate from that listener, so without this check it
	// announces a LAN address it does not serve and every other instance
	// offers an Add button for a peer that is always offline.
	if buildinfo.Deployment != "desktop" && buildinfo.ListensWidely && buildinfo.ListenPort > 0 {
		if ip := discovery.LocalIPv4(); ip != "" {
			// http:// because that is what this process serves. An instance
			// behind a proxy terminating TLS is reachable on its domain too,
			// which is what KnownDomains carries; this announces the direct
			// on-network address.
			self.URL = "http://" + ip + ":" + strconv.Itoa(buildinfo.ListenPort) + buildinfo.BasePath
		}
	}
	return self
}

// startDiscovery builds the announce this instance sends, and starts listening
// either way. A build with no address to announce still listens, so a desktop
// can find the server on its network and add it even though nothing can dial
// the desktop back.
func startDiscovery(a *app.App) *discovery.Service {
	// Only when a main package that serves has asked for it: see
	// buildinfo.DiscoveryEnabled for why this stays off in tests.
	if !buildinfo.DiscoveryEnabled {
		return nil
	}
	svc := discovery.New(discoverySelf(a))
	svc.Start()
	return svc
}

// instanceDisplayName is InstanceName if the user set one, else os.Hostname,
// else "KnightLoader". One function rather than the same precedence written
// out twice: this instance announces itself both on the LAN and on the relay
// (routes_relay.go's Announce), and two resolutions eventually disagree.
func instanceDisplayName(a *app.App) string {
	if name := a.Settings.Get().InstanceName; name != "" {
		return name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "KnightLoader"
}
