package api

// Other KnightLoader instances on the same network: registering them, and
// forwarding task operations to one.

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// errFederationOff answers every call that would reach a peer while the
// module is switched off. The peers stay stored and come back with it.
var errFederationOff = errors.New("\"Peer instances\" is switched off on the Modules page")

func registerFederation(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/instances", "the peer instances this one knows about; none while the module is switched off",
		func(w http.ResponseWriter, r *http.Request) {
			if a.ModuleOff("federation") {
				writeJSON(w, []federation.Instance{})
				return
			}
			writeJSON(w, a.Federation.List())
		})
	reg.Add(http.MethodPost, "/api/instances", "register a peer instance and report whether it answered",
		func(w http.ResponseWriter, r *http.Request) {
			var in federation.Instance
			if !decodeJSON(w, r, &in) {
				return
			}
			if a.ModuleOff("federation") {
				http.Error(w, errFederationOff.Error(), http.StatusServiceUnavailable)
				return
			}
			if err := addPeer(a, in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Three outcomes, not two: "reached it and it refused this
			// instance's credentials" needs a different fix from "could not
			// reach it at all", and adding a peer by address exchanges no
			// credential. Collapsing both into "offline" makes a
			// password-locked peer look switched off. The answer is the
			// connection phrase, which authenticates both ends by the same
			// key with nothing to copy per peer; refused says which of the
			// two happened.
			err := a.Federation.Ping(r.Context(), in.Name)
			writeJSON(w, map[string]any{
				"name": in.Name, "url": in.URL,
				"online":  err == nil,
				"refused": errors.Is(err, federation.ErrUnauthorized),
			})
		})
	reg.Add(http.MethodDelete, "/api/instances/{name}", "forget a peer instance",
		func(w http.ResponseWriter, r *http.Request) {
			name := r.PathValue("name")
			if err := a.Federation.Remove(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Removing a peer has to end the credentials too. Otherwise the
			// peer keeps a live full-power token on this instance, and the
			// token this instance held gets re-attached to whatever is
			// registered under that name next.
			forgetPeerCredentials(a, name)
			w.WriteHeader(http.StatusNoContent)
		})
	// Proxy task operations to a peer instance: only the task/link routes are
	// forwarded, so a peer's settings/accounts stay local to that peer.
	reg.Add(AnyMethod, "/api/instances/{name}/{rest...}", "forward a task or link request to a peer; nothing else is forwarded",
		func(w http.ResponseWriter, r *http.Request) {
			rest := r.PathValue("rest")
			if a.ModuleOff("federation") {
				http.Error(w, errFederationOff.Error(), http.StatusServiceUnavailable)
				return
			}
			// The queue travels with the task list, being that list's master
			// switch: showing a peer's downloads and then ordering, forcing or
			// stopping them on this box would act on the wrong machine.
			// Settings and accounts stay where they are configured.
			if rest != "links" && rest != "tasks" && rest != "queue" &&
				!strings.HasPrefix(rest, "tasks/") && !strings.HasPrefix(rest, "queue/") {
				http.Error(w, "route not proxied", http.StatusForbidden)
				return
			}
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			}
			resp, code, err := a.Federation.Proxy(r.Context(), r.PathValue("name"), r.Method, "/api/"+rest, body)
			if err != nil {
				http.Error(w, err.Error(), code)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(resp)
		})
}
