package api

// The VPN page: the providers the built-in torrent client can go through and
// the one it goes through now (internal/vpnconn). A login never comes back to
// the page, only whether one is stored.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

func registerVPN(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/vpn",
		"the VPN providers for torrents, which one torrents go through now, its state, exit address and forwarded port; never a login",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.VPN.State())
		})

	reg.Add(http.MethodPost, "/api/vpn/{id}/connect",
		"make one provider the one torrents go through, ending any other, and seal its login; empty fields keep the stored login. The connection follows, GET /api/vpn tells how it goes",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Fields map[string]string `json:"fields"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			st, err := a.VPN.Connect(r.PathValue("id"), body.Fields)
			if err != nil {
				writeVPNError(w, err)
				return
			}
			writeJSON(w, st)
		})

	reg.Add(http.MethodPost, "/api/vpn/disconnect",
		"end the VPN connection; torrents use the machine's own connection again",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.VPN.Disconnect())
		})

	reg.Add(http.MethodDelete, "/api/vpn/{id}",
		"delete a provider's sealed login, disconnecting it first when torrents go through it",
		func(w http.ResponseWriter, r *http.Request) {
			st, err := a.VPN.Forget(r.PathValue("id"))
			if err != nil {
				writeVPNError(w, err)
				return
			}
			writeJSON(w, st)
		})
}

func writeVPNError(w http.ResponseWriter, err error) {
	var ref *vpnconn.Refusal
	if !errors.As(err, &ref) {
		http.Error(w, "the VPN login could not be sealed into the credential store ("+err.Error()+
			"). Check that KnightLoader's data directory is writable.", http.StatusInternalServerError)
		return
	}
	out := map[string]any{"error": ref.Text, "code": ref.Code}
	if ref.Field != "" {
		out["field"] = ref.Field
		out["params"] = map[string]string{"field": ref.Field}
	}
	writeJSONStatus(w, ref.Status, out)
}
