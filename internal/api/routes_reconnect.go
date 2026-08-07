package api

// Asking the router for a new public address.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerReconnect(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/reconnect", "whether a reconnect is configured, and whether one is running",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.ReconnectState())
		})
	reg.Add(http.MethodPost, "/api/reconnect", "run one reconnect now and report the addresses either side of it",
		func(w http.ResponseWriter, r *http.Request) {
			// A second request while one is running is refused rather than queued
			// behind it: the reconnect package would make the caller wait for the
			// first run's verdict, and an HTTP request that hangs for two minutes with
			// no explanation is worse than a plain "already running".
			if a.ReconnectState().Busy {
				http.Error(w, "a reconnect is already running", http.StatusConflict)
				return
			}
			res, err := a.Reconnect(r.Context())
			if err != nil {
				// Safe to hand back verbatim: the reconnect package filters the router
				// password out of every error on its way out.
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{
				"oldIp":  res.OldIP.String(),
				"newIp":  res.NewIP.String(),
				"checks": res.Checks,
				"tookMs": res.Took.Milliseconds(),
			})
		})
}
