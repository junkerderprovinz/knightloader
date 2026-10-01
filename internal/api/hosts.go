package api

// Which names an instance without a password answers on.
//
// sameOrigin compares Origin with Host, and a page can pass that check from a
// domain of its own: it re-points the domain at this machine (DNS rebinding),
// and from then on both headers name the attacker's domain. With a password
// set the page gets nowhere, since the browser keeps the session cookie for
// the real name. Without one nothing else stands in the way, so a request is
// only served when its Host is a name the attacker cannot point here: an
// address, a name only the local network resolves, or a domain the owner
// listed.

import (
	"net"
	"net/http"
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

// knownHost refuses a request on a name this instance does not know while no
// password is set. A valid API token passes on any name: a page that rebound
// a domain does not have one.
func knownHost(a *app.App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Auth.Enabled() || hostAllowed(a, r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		if secret := bearerToken(r); secret != "" {
			if _, ok := a.APITokens.Check(secret); ok {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "KnightLoader has no password yet, so it only answers on its IP address, on local names "+
			"and on the domains under Settings > Pairing > Known domains. Open it by its IP address and "+
			"add "+hostName(r.Host)+" there, or set a password.", http.StatusMisdirectedRequest)
	})
}

// hostAllowed reports whether hostport names this instance in a way nobody
// outside the local network can make it resolve to.
func hostAllowed(a *app.App, hostport string) bool {
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
