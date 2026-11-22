package api

// Which names are this instance's own. A relay hello proves an id only to the
// relay it names, and the relay this instance serves takes the name a
// connection reached it under only when nobody outside the local network can
// make that name resolve here: an address, a name only the local network
// resolves, or a domain the owner listed.

import (
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// localSuffixes are names no public DNS answers for. localhost is resolved by
// the browser itself, local by multicast DNS on the link, and home.arpa and
// internal are reserved for private networks. lan is what many home routers
// hand out.
var localSuffixes = []string{".localhost", ".local", ".home.arpa", ".internal", ".lan"}

// ownHost reports whether hostport names this instance in a way nobody
// outside the local network can make it resolve to.
func ownHost(a *app.App, hostport string) bool {
	// A request built in-process, which is how relay calls arrive.
	if hostport == "" {
		return true
	}
	host := hostName(hostport)
	if net.ParseIP(host) != nil {
		return true
	}
	// A name without a dot is resolved by the local network alone: the
	// machine's own name, a container name on a Docker network, and wails,
	// which is where the desktop window loads from on Linux.
	if !strings.Contains(host, ".") {
		return true
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	if name, err := os.Hostname(); err == nil && strings.EqualFold(host, strings.TrimSuffix(name, ".")) {
		return true
	}
	cfg := a.Settings.Get()
	for _, d := range cfg.KnownDomains {
		if addressHost(d) == host {
			return true
		}
	}
	// The relay address may be this instance itself, serving the relay for
	// its group.
	return cfg.RelayURL != "" && addressHost(cfg.RelayURL) == host
}

// hostName is hostport's host, lower case and without the brackets of an IPv6
// literal or the dot of a fully qualified name.
func hostName(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host)
}

// addressHost is the host of an address as the owner typed it, with or
// without a scheme in front.
func addressHost(address string) string {
	address = strings.TrimSpace(address)
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return hostName(u.Host)
}
