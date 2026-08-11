package api

// The Remote access settings page's one data source: which addresses this
// instance actually answers requests on, whether anything is protecting
// them, and a QR code for the LAN case.
//
// There is deliberately no route here that pairs this instance with
// anything, issues a relay identity, or reaches off this LAN by itself. See
// GET /api/help (routes_help.go) for why, in full, and see registerTokens
// (routes_tokens.go) for the one piece of this page that does write
// anything: the named, revocable tokens themselves.

import (
	"net"
	"net/http"
	"sort"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"rsc.io/qr"
)

// RemoteAccessInfo is what GET /api/remote-access answers with.
type RemoteAccessInfo struct {
	// Deployment is "container" or "desktop" (buildinfo.Deployment). The
	// desktop build never opens a TCP port at all, see main's own comment on
	// AssetServer, so everything below is reported empty/false for it rather
	// than guessed at from request fields that mean nothing there.
	Deployment string `json:"deployment"`
	// PasswordSet mirrors GET /api/auth's own "enabled", repeated here so
	// the page has the one fact its warning banner turns on without a second
	// request racing this one.
	PasswordSet bool `json:"passwordSet"`
	// Addresses is every address this build can confirm or infer this
	// instance answers on, the one the request itself arrived on always
	// first (see remoteAddresses' own comment).
	Addresses []ReachableAddress `json:"addresses"`
	// Exposed is the loud warning's own condition: no password, and EITHER
	// this very request just proved this instance is reachable from
	// somewhere other than this machine itself (see requestIsNonLoopback),
	// OR the listener itself is bound wider than loopback on a machine that
	// has a real non-loopback interface to be reached on
	// (buildinfo.ListensWidely - see its own doc comment). The second half
	// exists because the first half alone can never be true for the one
	// person best placed to act on the warning: an admin looking at their
	// own Access page from 127.0.0.1 never generates a non-loopback
	// request, no matter how exposed the instance actually is - reproduced
	// live before this fix: a LAN-reachable, password-less instance showed
	// the warning to a visitor from another machine and never to the admin
	// sitting at the box itself.
	Exposed bool `json:"exposed"`
	// QR renders the primary address (Addresses[0] when there is one) as a
	// scannable code, nil when there is nothing to encode.
	QR *QRMatrix `json:"qr,omitempty"`
}

// ReachableAddress is one URL this instance might answer on.
type ReachableAddress struct {
	// Label names where this address came from: "this connection" for the
	// one the request itself arrived on, otherwise the interface's own IP.
	Label string `json:"label"`
	URL   string `json:"url"`
	// Loopback is true for 127.0.0.1/localhost/::1: reachable only from this
	// same machine, never a phone on the LAN, and never what the QR code
	// should encode.
	Loopback bool `json:"loopback"`
}

// QRMatrix is a QR code as the plain module grid rsc.io/qr computed, not a
// rendered image. The frontend draws it as inline SVG (web/src/components/
// QRCode.tsx) deliberately: a matrix has no format to disagree about between
// light and dark theme framing, no caching semantics of its own to get
// wrong, and no second render path (an <img> endpoint) that could fall out
// of sync with the address list this same response already carries.
type QRMatrix struct {
	Size int `json:"size"`
	// Bits is one string per row, '1' for a dark module and '0' for a light
	// one: a fixed-width string per row rather than size squared individual
	// booleans, because the JSON is a quarter the size and there is nothing
	// to interpret beyond "index into this string".
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
	// The desktop build serves api.Handler as a Wails AssetServer handler,
	// never on a TCP port (desktop/main.go). There is no network address to
	// report and no exposure to warn about, so this stops here rather than
	// reporting fields built from request data that describes an in-process
	// asset fetch, not a network peer.
	if buildinfo.Deployment == "desktop" {
		return info
	}
	info.Addresses = remoteAddresses(r)
	info.Exposed = !info.PasswordSet && (requestIsNonLoopback(r) || buildinfo.ListensWidely)
	// The first NON-loopback address, never Addresses[0] unconditionally:
	// that entry is "this connection", which is 127.0.0.1 whenever the
	// viewer themselves is on loopback - the ordinary case for the admin
	// configuring their own instance. A QR code encoding a loopback address
	// is worthless to the phone scanning it (it gets ITS OWN loopback, not
	// this machine), reproduced live before this fix: the served matrix
	// from a 127.0.0.1 request was bit-identical to a direct encode of
	// "http://127.0.0.1:PORT", sitting directly under a caption promising a
	// LAN address.
	if addr, ok := firstNonLoopback(info.Addresses); ok {
		info.QR = renderQR(addr)
	}
	return info
}

func firstNonLoopback(addrs []ReachableAddress) (string, bool) {
	for _, a := range addrs {
		if !a.Loopback {
			return a.URL, true
		}
	}
	return "", false
}

// remoteAddresses is every address this build can name for this instance,
// most trustworthy first: the address THIS REQUEST actually arrived on is
// always first when known, because it is not a guess, it just worked, and
// every other non-loopback IPv4 address bound to a local interface follows,
// sharing the same port and scheme, deduplicated against it.
func remoteAddresses(r *http.Request) []ReachableAddress {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	var out []ReachableAddress
	seen := map[string]bool{}
	add := func(label, hostport string, loopback bool) {
		if hostport == "" || seen[hostport] {
			return
		}
		seen[hostport] = true
		out = append(out, ReachableAddress{Label: label, URL: scheme + "://" + hostport, Loopback: loopback})
	}

	if r.Host != "" {
		add("this connection", r.Host, isLoopbackHost(r.Host))
	}
	if port := portOf(r.Host); port != "" {
		for _, ip := range localIPv4s() {
			add(ip, ip+":"+port, false)
		}
	}
	return out
}

// requestIsNonLoopback is Exposed's whole non-loopback check: proof, this
// exact request just arrived over a non-loopback path, never a forecast
// built from how the process is configured to bind.
//
// KL_ADDR was tried and deliberately rejected as a second signal. Its
// default (main.go's own ":8749", host part empty) binds every interface
// inside the process's own network namespace, and that is the NORMAL,
// correct default for a container: Docker's whole point is publishing a
// container-internal port to the host, which requires the process inside to
// listen widely on its own namespace regardless of whether the host then
// exposes that port to the LAN, to the internet, or to nothing at all with
// `-p 127.0.0.1:8749:8749`. A signal that reads "exposed" for nearly every
// ordinary container install, whether or not the host actually forwards
// that port anywhere reachable, is not a loud warning, it is noise that
// teaches people to dismiss it. A real non-loopback Host on a real incoming
// request has no such false-positive case: something outside this machine
// really did reach it to produce that request.
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

// localIPv4s is every non-loopback IPv4 address bound to a local interface,
// sorted for a stable, boring order across requests. IPv4-only for now: a
// bracket-and-colon literal ("http://[fe80::1]:8749") is correct but is not
// what a phone's camera app or a person typing an address by hand expects to
// see first, and every deployment this feature targets, a NAS, a home
// server, has an IPv4 LAN address to offer instead.
func localIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		// IsLinkLocalUnicast excludes the 169.254.0.0/16 APIPA range a NIC
		// gives itself when DHCP fails to answer - reachable only from the
		// same broadcast segment by an interface in the identical fallback
		// state, never a real LAN address a phone on the same network would
		// use. Without this, sort.Strings below put every "169." entry
		// ahead of the real "192.168.x" one a user actually wants, since
		// '1' sorts before '9' - live on a machine with even one interface
		// stuck in APIPA, the leading address in the list was unusable.
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

// renderQR encodes text at error-correction level M (38% redundant): high
// enough that a code partly obscured by a phone's own camera glare or a
// screen's moiré pattern still scans, without the noticeably denser grid H
// would produce for the same short URL. nil, not an empty matrix, on any
// encode failure (rsc.io/qr's own, verified, is "text too long to encode as
// QR" once no version fits). Every address built here is well under that
// bound, but a URL this build cannot yet imagine must degrade to "no code",
// not a half-built one.
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
