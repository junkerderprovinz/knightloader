package api

// The VPN page: the providers downloads can go through, the one in use now and
// which downloads use it (internal/vpnconn). A login never comes back to
// the page, only whether one is stored.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/vpnconn"
)

func registerVPN(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/vpn",
		"the VPN providers, which one is in use now, its state, exit address and forwarded port, and which downloads go through it; never a login",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.VPN.State())
		})

	reg.Add(http.MethodPost, "/api/vpn/{id}/connect",
		"make one provider the one the VPN uses, ending any other, and seal its login; empty fields keep the stored login. The connection follows, GET /api/vpn tells how it goes",
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
		"end the VPN connection; every download it carried uses the machine's own connection again",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.VPN.Disconnect())
		})

	reg.Add(http.MethodPut, "/api/vpn/routes",
		"say which downloads go through the VPN while a provider is in use: torrents, hosters and direct links, yt-dlp, debrid and Usenet; each one switched on waits while the tunnel is down",
		func(w http.ResponseWriter, r *http.Request) {
			var routes vpnconn.Routes
			if !decodeJSON(w, r, &routes) {
				return
			}
			writeJSON(w, a.VPN.SetRoutes(routes))
		})

	reg.Add(http.MethodDelete, "/api/vpn/{id}",
		"delete a provider's sealed login, disconnecting it first when it is the one in use",
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
