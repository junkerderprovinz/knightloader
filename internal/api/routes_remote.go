package api

// The Remote access page's data source: which addresses this instance answers
// on, whether anything protects them, and a QR code for the LAN case. Nothing
// here pairs, issues a relay identity or reaches off the LAN (see
// routes_help.go); the tokens the page writes live in routes_tokens.go.

import (
	"encoding/json"
	"net"
	"net/http"
	neturl "net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"rsc.io/qr"
)

// RemoteAccessInfo is what GET /api/remote-access answers with.
type RemoteAccessInfo struct {
	// Deployment is "container" or "desktop". The desktop build opens no TCP
	// port, so everything below stays empty for it.
	Deployment string `json:"deployment"`
	// PasswordSet repeats GET /api/auth's "enabled" so the warning needs no
	// second request.
	PasswordSet bool `json:"passwordSet"`
	// Addresses lists what this instance answers on, the address the request
	// arrived on first.
	Addresses []ReachableAddress `json:"addresses"`
	// Exposed means no password, and either this request came from another
	// machine or the listener is bound wider than loopback on a machine with a
	// real interface (buildinfo.ListensWidely). The second condition lets an
	// admin on 127.0.0.1 see the warning too.
	Exposed bool `json:"exposed"`
	// QR encodes the preferred address, nil when there is none.
	QR *QRMatrix `json:"qr,omitempty"`
}

// ReachableAddress is one URL this instance might answer on.
type ReachableAddress struct {
	// Label is "this connection" for the request's own address, "known" for a
	// remembered or typed-in domain, otherwise the interface IP.
	Label string `json:"label"`
	URL   string `json:"url"`
	// Loopback marks an address reachable only from this machine, which a QR
	// code must never encode.
	Loopback bool `json:"loopback"`
	// Domain marks a hostname rather than a bare IP; it still works once the
	// phone has left the LAN, so it outranks a LAN IP.
	Domain bool `json:"domain"`
}

// QRMatrix is a QR code as the module grid rsc.io/qr computed. The frontend
// draws it as inline SVG (QRCode.tsx), so there is no image endpoint that could
// fall out of step with the address list.
type QRMatrix struct {
	Size int `json:"size"`
	// Bits is one string per row, '1' for a dark module and '0' for a light
	// one, a quarter the size of a boolean grid in JSON.
	Bits []string `json:"bits"`
}

func registerRemoteAccess(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/remote-access",
		"the addresses this instance actually answers requests on, whether a password protects them, and a QR code for the LAN case",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, remoteAccessInfo(a, r))
		})
}

func remoteAccessInfo(a *app.App, r *http.Request) RemoteAccessInfo {
	info := RemoteAccessInfo{
		Deployment:  buildinfo.Deployment,
		PasswordSet: a.Auth.Enabled(),
	}
	// The desktop build serves the handler through Wails' asset server and is
	// reachable only over the relay.
	if buildinfo.Deployment == "desktop" {
		return info
	}
	known := a.Settings.Get().KnownDomains
	info.Addresses = remoteAddresses(r, known)
	info.Exposed = !info.PasswordSet && (requestIsNonLoopback(r) || buildinfo.ListensWidely)
	rememberDomain(a, info.Addresses, known)
	// Not simply Addresses[0], which is loopback whenever the admin views the
	// page locally; a phone scanning that would reach its own loopback.
	if addr, ok := preferredAddress(info.Addresses); ok {
		info.QR = renderQR(addr)
	}
	return info
}

// preferredAddress is the address worth putting into a QR code or pairing
// code: a known domain first, since it still works away from the LAN, else the
// first non-loopback entry.
func preferredAddress(addrs []ReachableAddress) (string, bool) {
	for _, a := range addrs {
		if !a.Loopback && a.Domain {
			return a.URL, true
		}
	}
	for _, a := range addrs {
		if !a.Loopback {
			return a.URL, true
		}
	}
	return "", false
}

// remoteAddresses is every address this build can name for this instance,
// most trustworthy first: the one the request arrived on, then the known
// domains (Settings.KnownDomains, full base URLs), then every non-loopback
// IPv4 address of a local interface with the request's port and scheme. Each
// carries the base path it is served under.
func remoteAddresses(r *http.Request, known []string) []ReachableAddress {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// A TLS-terminating reverse proxy talks plain HTTP to the container, so
	// the header is the only sign the browser used https (as in
	// requestOrigin).
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd == "https" {
		scheme = "https"
	}
	var out []ReachableAddress
	seen := map[string]bool{}
	add := func(label, urlScheme, hostport, base string, loopback bool) {
		if hostport == "" || seen[hostport] {
			return
		}
		seen[hostport] = true
		out = append(out, ReachableAddress{Label: label, URL: urlScheme + "://" + hostport + base, Loopback: loopback, Domain: !loopback && isDomainHost(hostport)})
	}

	if r.Host != "" {
		add("this connection", scheme, r.Host, requestBasePath(r), isLoopbackHost(r.Host))
	}
	for _, d := range known {
		// A known domain keeps the scheme and the path it was stored with,
		// since it sits behind a proxy regardless of how this request arrived.
		if u, err := neturl.Parse(d); err == nil && u.Host != "" {
			add("known", u.Scheme, u.Host, strings.TrimRight(u.Path, "/"), isLoopbackHost(u.Host))
		}
	}
	// A call through the relay has no Host, so the port falls back to the
	// listener's own (buildinfo.ListenPort).
	port := portOf(r.Host)
	if port == "" && buildinfo.ListenPort > 0 {
		port = strconv.Itoa(buildinfo.ListenPort)
	}
	if port != "" {
		for _, ip := range localIPv4s() {
			add(ip, scheme, ip+":"+port, buildinfo.BasePath, false)
		}
	}
	return out
}

// rememberDomain saves the domain the request arrived on into
// Settings.KnownDomains, so it stays listed when later requests come in over
// the LAN IP. It replaces an entry for the same scheme and host, which a new
// base path has made stale and remoteAddresses would list in its place. addrs
// is what remoteAddresses just built.
func rememberDomain(a *app.App, addrs []ReachableAddress, known []string) {
	if len(addrs) == 0 || addrs[0].Label != "this connection" || addrs[0].Loopback || !addrs[0].Domain {
		return
	}
	current := addrs[0].URL
	origin := originOf(current)
	next := make([]string, 0, len(known)+1)
	placed := false
	for _, k := range known {
		if k != current && originOf(k) != origin {
			next = append(next, k)
		} else if !placed {
			next, placed = append(next, current), true
		}
	}
	if !placed {
		next = append(next, current)
	}
	if slices.Equal(next, known) {
		return
	}
	patch, err := json.Marshal(next)
	if err != nil {
		return
	}
	_, _ = a.Settings.SetPartial(map[string]json.RawMessage{"knownDomains": patch})
}

// originOf is a known domain without its path, "" for one without a scheme.
func originOf(address string) string {
	u, err := neturl.Parse(address)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// isDomainHost reports whether hostport's host is a hostname rather than an
// IP literal.
func isDomainHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return net.ParseIP(host) == nil
}

// requestIsNonLoopback reports whether this request arrived from outside this
// machine. KL_ADDR is not used as a signal: a container has to listen on all
// interfaces of its own namespace whatever the host publishes, so it would
// warn on nearly every install.
func requestIsNonLoopback(r *http.Request) bool {
	return r.Host != "" && !isLoopbackHost(r.Host)
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func portOf(hostport string) string {
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return ""
	}
	return port
}

// localIPv4s is every non-loopback IPv4 address of a local interface, sorted.
// IPv6 literals are left out; they are not what a person types or a camera app
// expects, and every targeted deployment has an IPv4 LAN address.
func localIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		// Link-local 169.254.x.x appears when DHCP fails and is unusable,
		// yet would sort ahead of the real 192.168.x address.
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	sort.Strings(out)
	return out
}

// renderQR encodes text at error-correction level M, which survives glare and
// moiré without the denser grid of level H. It returns nil if the text is too
// long to encode.
func renderQR(text string) *QRMatrix {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil
	}
	bits := make([]string, code.Size)
	row := make([]byte, code.Size)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				row[x] = '1'
			} else {
				row[x] = '0'
			}
		}
		bits[y] = string(row)
	}
	return &QRMatrix{Size: code.Size, Bits: bits}
}
