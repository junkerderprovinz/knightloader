package api

// Tailscale/Funnel: log in once, this instance answers on a real, public
// https://<name>.<tailnet>.ts.net address - the "log in on each instance,
// the rest just works" alternative to the relay/pairing card (jdp,
// 2026-08-26: "das ist alles viel zu kompliziert für User... man soll auf
// dem handy nicht auch noch tailscale installieren müssen"). See
// internal/tsnetsrv's own package comment for the full reasoning and for
// why this is server-side only - a phone or the browser extension never
// needs anything installed, because Funnel's address is an ordinary
// https:// URL any client already knows how to reach.
//
// One *tsnetsrv.Manager for the process lifetime, held on app.App itself
// (a.Tsnet - see that field's own comment for why this cannot be a local
// variable the way registerRelay's own srv := relay.New() is for its
// inbound side: routes_remote.go's address list needs to read it too). If
// this instance was already connected in an earlier run, Start reconnects
// immediately below, so a container restart does not mean logging in again.

import (
	"encoding/json"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerTsnet(reg *Registry, a *app.App) {
	mgr := a.Tsnet

	reg.Add(http.MethodGet, "/api/tsnet/status",
		"this instance's Tailscale connection: off, connecting (with a login link), connected (with its public funnel address), or a failure",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, mgr.Info())
		})

	reg.Add(http.MethodPost, "/api/tsnet/start",
		"begin (or resume) the Tailscale connection; poll GET /api/tsnet/status for the login link and, once connected, the public address",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Hostname string `json:"hostname"`
			}
			// A body is optional here - pressing "Verbinden" with nothing
			// configured yet is the ordinary case, not a malformed request,
			// so a missing/empty/invalid body is read as "use the default"
			// rather than rejected the way decodeJSON's callers elsewhere
			// require a real body.
			_ = json.NewDecoder(r.Body).Decode(&body)
			if err := mgr.Start(body.Hostname); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			enabled, err := json.Marshal(true)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hostname, err := json.Marshal(body.Hostname)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := a.Settings.SetPartial(map[string]json.RawMessage{
				"tsnetEnabled":  enabled,
				"tsnetHostname": hostname,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, mgr.Info())
		})

	reg.Add(http.MethodPost, "/api/tsnet/stop",
		"log this instance out of Tailscale - a later start reconnects as the same node, this only ends the current session",
		func(w http.ResponseWriter, r *http.Request) {
			if err := mgr.Stop(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			disabled, err := json.Marshal(false)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := a.Settings.SetPartial(map[string]json.RawMessage{"tsnetEnabled": disabled}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, mgr.Info())
		})

	// Reconnect automatically at boot, the same reasoning applyRelay's own
	// doc comment gives for the relay client: a configuration from an
	// earlier run must not need the settings page revisited before it
	// takes effect again.
	if a.Settings.Get().TsnetEnabled {
		_ = mgr.Start(a.Settings.Get().TsnetHostname)
	}
}
