package api

// The self-test: one sweep of everything this instance can find out about
// itself, plus the one thing only the BROWSER can find out - what the reverse
// proxy in front of this process did to the request on the way in.
//
// THREE ROUTES AND NOT ONE, and the split is the design rather than an
// accident of implementation.
//
//   POST /api/selftest          start a sweep, or join the one already running
//   GET  /api/selftest          the current or last sweep, as each check lands
//   GET  /api/selftest/request  what this instance saw of the very request that
//                               asked, for the browser to compare against what
//                               it knows it sent
//
// WHY THE RESULTS ARE POLLED AND NOT BROADCAST. internal/hub makes pushing
// them over the WebSocket trivial, and doing so would be exactly wrong: the
// WebSocket is one of the things under test. A result delivered over it
// disappears in precisely the case the operator most needs it - an nginx that
// forgot `proxy_set_header Upgrade`, where the app loads, looks perfect and
// never updates. Polling also survives the sixty-second read timeout a reverse
// proxy puts on a single request, which is a failure this repo has already hit
// once, on POST /api/links.
//
// WHY THE PROXY HALF IS A REQUEST ECHO AND NOT A SERVER-SIDE PROBE. A probe
// run here would dial this process's own listener on loopback, bypass the
// proxy entirely and prove nothing at all - it would report "WebSocket fine"
// on an instance whose users are staring at a frozen list. The server can only
// ever see what the proxy handed it; the whole question is whether that matches
// what the browser sent. So this route reports the server's half honestly and
// says nothing about the verdict, and web/src/lib/selftest.ts does the
// comparison in the one place that holds both halves.
//
// THIS MACHINE'S ANSWER, AND ONLY THIS MACHINE'S. None of the three is on the
// federation forwarder's list (routes_federation.go) or the relay allowlist
// (routes_relay.go), for the reason routes_diskspace.go already gives about
// DiskReport and with more force: a peer's answer describes the wrong machine's
// disks, the wrong machine's clock and somebody else's reverse proxy, and a row
// of it shown while a peer's list is on screen would be confidently wrong about
// all three.
//
// SECURITY. reg.Add and never AddOpen: there is no credential of its own in any
// of these requests, so they ride the session guard like everything else under
// /api/. What they send is folder paths (the same ones /api/folders already
// lists), byte counts, a JD address, account LABELS, provider error sentences,
// and a handful of headers this instance received. Two deliberate omissions:
// no credential or any part of one ever appears, and X-Forwarded-For travels as
// a hop COUNT rather than as addresses - the chain names a network's internal
// proxies, which is a fact about somebody's infrastructure that a row saying
// "two hops" conveys just as well.
//
// AND IT IS NOT IN THE DIAGNOSTICS BUNDLE. routes_diagnostics.go is explicit
// that the bundle is meant to be attached to a PUBLIC bug report, and clears
// the archive passwords for that reason. Account labels are user-chosen and are
// very often email addresses. If a later pass ever folds a self-test result
// into that bundle, the labels are the thing that has to go first.

import (
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/selftest"
)

// selfTestRequestView is what this instance saw of one request.
//
// Every field is a FACT and none of them is a verdict. The browser knows what
// it sent; only the comparison of the two is worth anything, and the comparison
// is not made here - see the file comment.
type selfTestRequestView struct {
	// Host is r.Host as this instance received it. internal/api's sameOrigin
	// compares the browser's Origin against exactly this value and 403s a
	// mismatch, so a proxy rewriting it produces a UI that renders and then
	// refuses every write - which is why this is the first field.
	Host string `json:"host"`
	// ForwardedHost is X-Forwarded-Host, "" when absent. When it is present
	// and differs from Host, the proxy has told this instance both what the
	// browser asked for and what it decided to say instead, which makes the
	// diagnosis exact rather than a shrug.
	ForwardedHost string `json:"forwardedHost"`
	// ForwardedProto is X-Forwarded-Proto, "" when absent.
	ForwardedProto string `json:"forwardedProto"`
	// TLS is whether the connection into THIS process was itself TLS. It is a
	// different fact from whether the browser is on https, and the difference
	// is the whole point of the header above.
	TLS bool `json:"tls"`
	// Path is r.URL.Path - evidence that nothing rewrote the path on the way
	// in. It can only ever be the route's own path when the handler ran at
	// all, which is exactly what makes it worth sending: a proxy that passes a
	// prefix through unstripped never reaches this handler in the first place.
	Path string `json:"path"`
	// ForwardedPrefix is X-Forwarded-Prefix, "" when absent - the header
	// Traefik's StripPrefix middleware sets, and the one signature of "this
	// app is being served under a path prefix" that survives the stripping.
	// It is the only way this instance can see a prefix at all, because a
	// prefix that is NOT stripped means every request 404s before it gets
	// here, and one that is stripped is invisible by construction.
	ForwardedPrefix string `json:"forwardedPrefix"`
	// ForwardedForHops is how many hops X-Forwarded-For names. The addresses
	// are deliberately not sent - see the file comment.
	ForwardedForHops int `json:"forwardedForHops"`
	// Now is this instance's clock when it answered. The browser notes the
	// time before and after the call and takes the midpoint, so that half a
	// slow round trip is not read as clock drift.
	Now time.Time `json:"now"`
	// Zone, ZoneOffsetSeconds and ZoneReadable are selftest.ZoneReport's three
	// facts. They are repeated here rather than only in the sweep's clock row
	// because this route answers on mount, before anybody has pressed
	// anything, and the proxy card needs the offset to make sense of Now.
	Zone              string `json:"zone"`
	ZoneOffsetSeconds int    `json:"zoneOffsetSeconds"`
	ZoneReadable      bool   `json:"zoneReadable"`
	// Deployment is buildinfo.Deployment, so the page can hide the whole proxy
	// card on the desktop build - which serves its own interface out of Wails'
	// asset server, opens no TCP listener and has no proxy in front of it. A
	// WebSocket probe there fails for reasons that have nothing to do with
	// nginx, and a row telling somebody with no proxy to go and fix theirs is
	// worse than no row.
	Deployment string `json:"deployment"`
}

func registerSelfTest(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/selftest",
		"start one self-test sweep of this instance, or join the one already running",
		func(w http.ResponseWriter, r *http.Request) {
			// 202 whether or not this call is the one that started it. From
			// the caller's side "your sweep is under way" and "somebody else's
			// sweep is under way and you may watch it" ask for the same next
			// step: poll GET /api/selftest until finishedAt appears. A 409 for
			// the second case would make a page opened in two tabs look broken
			// in one of them for no gain at all - and unlike a database
			// compaction, joining costs nothing and changes nothing.
			run, _ := a.SelfTestStart()
			writeJSONStatus(w, http.StatusAccepted, run)
		})

	reg.Add(http.MethodGet, "/api/selftest",
		"the current or last self-test sweep of this instance, with each check's result as it lands",
		func(w http.ResponseWriter, r *http.Request) {
			// The zero Run - empty id, empty lists - is a real answer and
			// means "nothing has been swept here yet". Answered 200 rather
			// than 404 so the page's own decoder does not throw on the state
			// every install is in before the button is first pressed.
			writeJSON(w, a.SelfTestLatest())
		})

	reg.Add(http.MethodGet, "/api/selftest/request",
		"what this instance saw of the very request that asked - host, forwarded headers, path and clock - for comparing against what the browser sent",
		func(w http.ResponseWriter, r *http.Request) {
			// No-store on the way out as well as on the way in. The browser
			// asks with cache: 'no-store' because it times the round trip, and
			// a proxy or a service worker answering from a cache would hand
			// back a timestamp minutes old and report it as clock drift.
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, requestViewOf(r))
		})
}

// requestViewOf reads one request into the view. Pure apart from the clock and
// the zone, which is what lets a test drive it with a hand-built request.
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

// normalisePrefix reduces X-Forwarded-Prefix to "" when it names no prefix at
// all. A bare "/" is what several proxies send for "the root", and reporting
// that as a path prefix would put a red row on a correctly configured install.
func normalisePrefix(v string) string {
	p := strings.TrimSpace(v)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}

// forwardedForHops counts the addresses X-Forwarded-For names, across every
// copy of the header, without sending any of them.
//
// The count alone answers the only question the proxy card asks of it - "is
// there a proxy in front of this at all, and how many" - while the addresses
// themselves are a map of somebody's internal network. The header may appear
// more than once and each copy may hold a comma-separated list, which is why
// this walks Values rather than Get.
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
