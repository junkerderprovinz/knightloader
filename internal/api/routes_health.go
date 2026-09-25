package api

// The detailed health readout: one JSON document describing every part of this
// instance, and the same document as Prometheus exposition text.
//
// /api/health itself must keep answering {"status":"ok",...} with a 200 while
// the process is up. The phone app's LAN discovery compares the literal "ok",
// the Dockerfile's HEALTHCHECK would restart KnightLoader in a loop over a
// fault in the JD sidecar, and internal/bridge refuses to start on anything
// but a 200. The real state lives here instead.
//
// Neither route is forwarded to peers (routes_federation.go, routes_relay.go),
// since every number describes this machine. Both need a session; a collector
// sends one of this instance's API tokens as a Bearer header, and the
// exposition carries folder paths as label values. While Settings.Metrics is
// off, /api/metrics answers like a route that does not exist.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// metricsPath is the one address the exposition text answers on.
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
			"answers 404 unless \"Metrics address for a monitoring system\" is switched on (Health page or Modules page)",
		func(w http.ResponseWriter, r *http.Request) {
			// The /api/ catch-all's wording, so a closed door does not reveal
			// whether a key would have worked.
			if !a.Settings.Get().Metrics {
				http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
				return
			}
			// Set before the first Write, which commits the headers. The version
			// parameter tells a collector which exposition format this is.
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			_, _ = w.Write([]byte(prometheusText(a.HealthReport())))
		})
}

// metricsDetail is the live line of the metrics module row: where to point
// the collector, and a warning when a password is set but no token can read.
func metricsDetail(a *app.App, s settings.Settings) line {
	path := map[string]string{"path": metricsPath}
	if !s.Metrics {
		return line{
			text: "off; " + metricsPath + " answers 404, the same as an endpoint that does not exist",
			code: "metricsOff", args: path,
		}
	}
	if a.Auth != nil && a.Auth.Enabled() {
		tokens := a.APITokens.List()
		if len(tokens) == 0 {
			return line{
				text: "this instance has a password and no API token yet, so a collector has nothing to authenticate with; create one on the Remote access page",
				code: "metricsNoToken",
			}
		}
		if !someTokenHolds(tokens, apitoken.ScopeRead) {
			return line{
				text: "this instance has a password and no API token that can read, so a collector is refused; create one with \"Read only\" on the Remote access page",
				code: "metricsNoReadToken",
			}
		}
	}
	return line{
		text: "reachable at " + metricsPath + "; a collector on a password-protected instance sends one of this instance's API tokens as a Bearer header",
		code: "metricsReady", args: path,
	}
}
