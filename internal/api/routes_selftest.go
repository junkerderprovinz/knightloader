package api

// The self-test: one sweep of everything this instance can find out about
// itself, plus a request echo so the browser can see what the reverse proxy in
// front of it changed.
//
// Results are polled rather than pushed over the WebSocket, because the
// WebSocket is one of the things under test and a proxy missing the Upgrade
// header would swallow the results. Polling also survives a proxy's per-request
// read timeout.
//
// The proxy check is an echo rather than a server-side probe: a probe from here
// would dial loopback and bypass the proxy. web/src/lib/selftest.ts compares
// the echo with what the browser sent.
//
// None of the routes is forwarded to peers (routes_federation.go,
// routes_relay.go), since the answer describes this machine's disks, clock and
// proxy. The results carry account labels, often email addresses, so they must
// not go into the diagnostics bundle, which is meant for public bug reports.

import (
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/selftest"
)

// selfTestRequestView is what this instance saw of one request. It holds facts
// only; the browser makes the comparison.
type selfTestRequestView struct {
	// Host is r.Host as received. sameOrigin compares Origin against it, so a
	// proxy rewriting it yields a UI that renders and then refuses every write.
	Host string `json:"host"`
	// ForwardedHost is X-Forwarded-Host, "" when absent.
	ForwardedHost string `json:"forwardedHost"`
	// ForwardedProto is X-Forwarded-Proto, "" when absent.
	ForwardedProto string `json:"forwardedProto"`
	// TLS is whether the connection into this process was TLS, which differs
	// from whether the browser is on https.
	TLS bool `json:"tls"`
	// Path is r.URL.Path, showing that nothing rewrote the path.
	Path string `json:"path"`
	// ForwardedPrefix is X-Forwarded-Prefix, "" when absent. It is the only
	// trace of a path prefix a proxy stripped before the request arrived.
	ForwardedPrefix string `json:"forwardedPrefix"`
	// ForwardedForHops is how many hops X-Forwarded-For names. The addresses
	// themselves would map somebody's internal network, so they are not sent.
	ForwardedForHops int `json:"forwardedForHops"`
	// Now is this instance's clock when it answered. The browser compares it
	// with the midpoint of its round trip.
	Now time.Time `json:"now"`
	// Zone, ZoneOffsetSeconds and ZoneReadable repeat selftest.ZoneReport,
	// because this route answers before any sweep has run.
	Zone              string `json:"zone"`
	ZoneOffsetSeconds int    `json:"zoneOffsetSeconds"`
	ZoneReadable      bool   `json:"zoneReadable"`
	// Deployment lets the page hide the proxy card on the desktop build, which
	// has no proxy in front of it.
	Deployment string `json:"deployment"`
}

func registerSelfTest(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/selftest",
		"start one self-test sweep of this instance, or join the one already running",
		func(w http.ResponseWriter, r *http.Request) {
			// 202 either way: starting a sweep and joining one lead to the same
			// next step, polling until finishedAt appears.
			run, _ := a.SelfTestStart()
			writeJSONStatus(w, http.StatusAccepted, run)
		})

	reg.Add(http.MethodGet, "/api/selftest",
		"the current or last self-test sweep of this instance, with each check's result as it lands",
		func(w http.ResponseWriter, r *http.Request) {
			// The zero Run means nothing has been swept yet, and is a 200.
			writeJSON(w, a.SelfTestLatest())
		})

	reg.Add(http.MethodGet, "/api/selftest/request",
		"what this instance saw of the very request that asked - host, forwarded headers, path and clock - for comparing against what the browser sent",
		func(w http.ResponseWriter, r *http.Request) {
			// The browser times the round trip, so a cached answer would read
			// as clock drift.
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, requestViewOf(r))
		})
}

// requestViewOf reads one request into the view. It is pure apart from the
// clock and the zone, so a test can drive it with a hand-built request.
func requestViewOf(r *http.Request) selfTestRequestView {
	z := selftest.ZoneReport()
	return selfTestRequestView{
		Host:              r.Host,
		ForwardedHost:     strings.TrimSpace(r.Header.Get("X-Forwarded-Host")),
		ForwardedProto:    strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))),
		TLS:               r.TLS != nil,
		Path:              r.URL.Path,
		ForwardedPrefix:   normalisePrefix(r.Header.Get("X-Forwarded-Prefix")),
		ForwardedForHops:  forwardedForHops(r),
		Now:               time.Now(),
		Zone:              z.Name,
		ZoneOffsetSeconds: z.OffsetSeconds,
		ZoneReadable:      z.TZResolved,
		Deployment:        buildinfo.Deployment,
	}
}

// normalisePrefix reduces X-Forwarded-Prefix to "" when it names no prefix.
// Several proxies send a bare "/" for the root.
func normalisePrefix(v string) string {
	p := strings.TrimSpace(v)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}

// forwardedForHops counts the addresses X-Forwarded-For names across every
// copy of the header, since each copy may hold a comma-separated list.
func forwardedForHops(r *http.Request) int {
	n := 0
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if strings.TrimSpace(part) != "" {
				n++
			}
		}
	}
	return n
}
