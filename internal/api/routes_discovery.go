package api

// The instances on this network, found with nothing configured.
//
// This is the "out of the box" half of connecting two KnightLoaders: the
// relay solves reaching an instance that cannot be reached, and a pairing
// code solves proving who you are, but the ordinary case - a server and a
// desktop on one home network - was only ever missing the ADDRESS. See
// internal/discovery for the protocol and for why this deliberately does not
// pair anything by itself.

import (
	"net/http"
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
	// Deployment is "container" or "desktop". A desktop build never announces
	// (it has no address anything could dial), so in practice this is always
	// "container" today - carried anyway because the announce does, and
	// because a peer that starts announcing later should not need a new field.
	Deployment string `json:"deployment"`
	// Known is true when this instance is already a stored or relay peer, by
	// name or by address. The page still SHOWS it, greyed, rather than hiding
	// it: "the one I expected is missing" and "it is here and already added"
	// are different answers, and silently omitting the second one makes the
	// list look broken.
	Known bool `json:"known"`
}

func registerDiscovery(reg *Registry, a *app.App) {
	svc := startDiscovery(a)
	if svc != nil {
		a.SetDiscovery(svc)
	}
	// Rebuilt on a settings save, so renaming an instance is visible to the
	// network on the next announce rather than after a restart. Same call
	// shape as applyRelay, which exists for the identical reason on the relay's
	// own announce - see routes_settings.go.
	discoveryRefresh = func() {
		if svc != nil {
			svc.SetSelf(discoverySelf(a))
		}
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
				// Sanitised again on arrival, not only on the way out: the
				// sender is whatever is on the network - an instance that
				// predates the sanitising announce, or something that is not
				// a KnightLoader at all. A name this side cannot add is a row
				// with a button that can only fail.
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

// discoveryRefresh re-reads what this instance announces. Set by
// registerDiscovery, a no-op until then and on a build with no discovery.
var discoveryRefresh = func() {}

// discoverySelf is what this instance announces right now.
func discoverySelf(a *app.App) discovery.Peer {
	// Sanitised for the same reason pairingSelf does it: this name is what a
	// receiving instance will try to add a peer BY, and federation's naming
	// rule is narrower than what a person may reasonably have called their
	// box. An unsanitised name here produced a card with an Add button that
	// could only ever answer "invalid instance name".
	name := federation.SanitiseName(instanceDisplayName(a))
	if name == "" {
		name = a.Settings.Get().InstanceID
	}
	self := discovery.Peer{
		ID:         a.Settings.Get().InstanceID,
		Name:       name,
		Deployment: buildinfo.Deployment,
	}
	// ListensWidely, not just a port: an instance started with
	// KL_ADDR=127.0.0.1:8749 - the documented way to run behind a local
	// reverse proxy - has a port, but nothing outside the box can reach it.
	// The multicast socket is separate from that listener, so without this
	// check such an instance still announced a LAN address it does not serve,
	// and every other instance on the network offered a live Add button for a
	// peer that can only ever be offline.
	if buildinfo.Deployment != "desktop" && buildinfo.ListensWidely && buildinfo.ListenPort > 0 {
		if ip := discovery.LocalIPv4(); ip != "" {
			// http:// because that is what this process serves; an instance
			// behind a reverse proxy terminating TLS is reachable on its
			// domain too, and that path is what KnownDomains and the pairing
			// code already carry. What is announced here is the direct,
			// on-this-network address, which is the only one discovery is
			// about.
			self.URL = "http://" + ip + ":" + strconv.Itoa(buildinfo.ListenPort)
		}
	}
	return self
}

// startDiscovery builds the announce this instance sends, and starts
// listening either way.
//
// A build with no address to announce still listens: the desktop can then
// FIND the server on its network and add it, even though nothing can dial the
// desktop back. That asymmetry is the honest one - it is the same reason the
// desktop cannot issue a pairing code (routes_pairing.go's own gate).
func startDiscovery(a *app.App) *discovery.Service {
	// Only when a main package that actually serves has asked for it: see
	// buildinfo.DiscoveryEnabled for why this must stay off in tests.
	if !buildinfo.DiscoveryEnabled {
		return nil
	}
	svc := discovery.New(discoverySelf(a))
	svc.Start()
	return svc
}
