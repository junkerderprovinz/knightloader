package api

// The detailed health readout: one JSON document describing every part of this
// instance, and the same document again as Prometheus exposition text.
//
// WHY IT IS A ROUTE AT ALL, when /api/health already exists. That one answers
// {"status":"ok","version":...} and it MUST GO ON ANSWERING EXACTLY THAT, on a
// 200, for as long as the process is up. Three shipped things read it and one
// of them cannot be patched from here:
//
//   - mobile/src/api/discover.ts tests `body?.status !== 'ok'` as a literal
//     string and gives up otherwise. The phone app's LAN discovery is a sweep
//     of 253 addresses looking for that exact word, so an instance that
//     answered "degraded" would stop being findable by every phone in the
//     wild - and phones update on their own schedule, not with the container.
//   - the Dockerfile's HEALTHCHECK reads the exit code of a wget against it. A
//     503 marks the container unhealthy in Unraid, which is exactly what an
//     auto-restart policy acts on: a dead JD sidecar would restart
//     KnightLoader in a loop for a fault in a different container.
//   - internal/bridge refuses to start the Click'n'Load listener on any status
//     other than 200.
//
// So the two fields there are frozen. New fields could be added to it safely;
// the two that are there may not move, and the status may not stop being "ok".
// The detail is a second readout instead, and this file is it. Do not "fix"
// /api/health to report the real state - that is the bug, not the feature.
//
// THIS MACHINE'S ANSWER, AND ONLY THIS MACHINE'S. Both routes are deliberately
// off the federation forwarder's list (routes_federation.go) and off
// relayForwardable (routes_relay.go). Both are allowlists a new route is
// outside of by default, and that default is correct here for the reason
// routes_diskspace.go already writes down: a peer's answer describes THAT box's
// disks, sidecar and queue, and a row of them drawn under a peer's name names
// the wrong machine with total confidence. Every number on this page has that
// property, not just the disk ones.
//
// SECURITY. Both routes are reg.Add and never reg.AddOpen. The owner settled
// this rather than the spec: the item asked for a second OPEN endpoint and does
// not get one, because routes_test.go pins the open list with a written
// justification per entry and an unauthenticated metrics route on a
// password-locked instance is a hole somebody would have to have chosen
// deliberately. A collector reaches /api/metrics with one of this instance's
// own API tokens as a Bearer header, which every scraper can be told to send.
// Note what the guarded choice buys beyond the obvious: the exposition text
// carries the target folders' PATHS as label values, which is the same exposure
// /api/folders and the settings page already have and is still not something to
// hand to an unauthenticated caller.
//
// AND THE METRICS ADDRESS DOES NOT EXIST UNTIL SOMEBODY OPENS IT. While
// Settings.Metrics is false the route answers 404 with the /api/ catch-all's
// own wording, exactly as the SABnzbd door does while its module is off: a door
// that is closed should not be able to tell anybody whether a key would have
// worked.

import (
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// metricsPath is the one address the exposition text answers on. Named so the
// module row, the 404 branch and the tests cannot spell it three ways.
const metricsPath = "/api/metrics"

func registerHealth(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/health/detail",
		"every part of this instance with its own state, the queue by why it is waiting and why it failed, "+
			"room on the target folders, and how long this process has been up",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.HealthReport())
		})

	reg.Add(http.MethodGet, metricsPath,
		"the same reading as /api/health/detail in Prometheus exposition format, for a monitoring system to fetch; "+
			"answers 404 unless the metrics switch on the Health settings page is on",
		func(w http.ResponseWriter, r *http.Request) {
			// The switch first, and before anything touches the body. See the
			// file comment: 404 is what "this endpoint is not here" means
			// everywhere else in this app, and this wording is the /api/
			// catch-all's own, matched on purpose.
			if !a.Settings.Get().Metrics {
				http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
				return
			}
			// Set before a single byte is written. net/http commits the header
			// block on the first Write, so a Content-Type set afterwards is
			// silently dropped and this would be served as text/plain with
			// sniffed encoding - which is the same trap writeJSONStatus
			// documents from the other side. The version parameter is part of
			// the contract: it is what tells a collector which exposition
			// format this is.
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			_, _ = w.Write([]byte(prometheusText(a.HealthReport())))
		})
}

// metricsDetail is the one live line the metrics module row shows.
//
// It says the two things somebody switching this on cannot find out any other
// way: where to point the collector, and that the fetch needs a token on an
// instance with a password - the same shape and the same reason
// downloadClientDetail carries for the SABnzbd door.
func metricsDetail(a *app.App, s settings.Settings) string {
	if !s.Metrics {
		return "off; " + metricsPath + " answers 404, the same as an endpoint that does not exist"
	}
	var notes []string
	if a.Auth != nil && a.Auth.Enabled() && len(a.APITokens.List()) == 0 {
		notes = append(notes, "this instance has a password and no API token yet, so a collector has nothing to authenticate with; create one on the Access page")
	}
	if len(notes) == 0 {
		return "reachable at " + metricsPath + "; a collector on a password-protected instance sends one of this instance's API tokens as a Bearer header"
	}
	return strings.Join(notes, "; ")
}
