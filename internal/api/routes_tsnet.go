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
// inbound side: routes_remote.go's address list needs to read it too).

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
			_ = decodeBody(r, &body)
			if err := mgr.Start(body.Hostname); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			info := mgr.Info()
			enabled, err := json.Marshal(true)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// info.Hostname, not body.Hostname: Start applies the
			// ""->"knightloader" default internally, and persisting the
			// raw, pre-default value left Settings and the live connection
			// disagreeing about this instance's own hostname whenever a
			// caller submitted an empty one - caught in review before this
			// fix.
			hostname, err := json.Marshal(info.Hostname)
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
			writeJSON(w, info)
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
}

// applyTsnet reconnects a previously-authorized instance at boot, the same
// reasoning applyRelay's own doc comment gives for the relay client: a
// configuration from an earlier run must not need the settings page
// revisited before it takes effect again.
//
// Called from Handler AFTER a.SetSelfServeHandler, deliberately not from
// registerTsnet (which runs inside registerAll, before that) - Start spawns
// a goroutine that eventually calls a.SelfServeHandler() itself, exactly
// once, to serve the funnel listener; calling it any earlier would risk that
// one read happening before the real handler exists. In practice Start's own
// network round trip to Tailscale is far slower than the handful of
// register* calls this used to run ahead of, but this ordering removes the
// assumption instead of relying on it.
func applyTsnet(a *app.App) {
	if a.Settings.Get().TsnetEnabled {
		_ = a.Tsnet.Start(a.Settings.Get().TsnetHostname)
	}
}
