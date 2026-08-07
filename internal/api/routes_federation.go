package api

// Other KnightLoader instances on the same network: registering them, and
// forwarding task operations to one.

import (
	"io"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

func registerFederation(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/instances", "the peer instances this one knows about",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.Federation.List())
		})
	reg.Add(http.MethodPost, "/api/instances", "register a peer instance and report whether it answered",
		func(w http.ResponseWriter, r *http.Request) {
			var in federation.Instance
			if !decodeJSON(w, r, &in) {
				return
			}
			if err := a.Federation.Add(in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			online := a.Federation.Ping(r.Context(), in.Name) == nil
			writeJSON(w, map[string]any{"name": in.Name, "url": in.URL, "online": online})
		})
	reg.Add(http.MethodDelete, "/api/instances/{name}", "forget a peer instance",
		func(w http.ResponseWriter, r *http.Request) {
			if err := a.Federation.Remove(r.PathValue("name")); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	// Proxy task operations to a peer instance: only the task/link routes are
	// forwarded, so a peer's settings/accounts stay local to that peer.
	reg.Add(AnyMethod, "/api/instances/{name}/{rest...}", "forward a task or link request to a peer; nothing else is forwarded",
		func(w http.ResponseWriter, r *http.Request) {
			rest := r.PathValue("rest")
			if rest != "links" && rest != "tasks" && !strings.HasPrefix(rest, "tasks/") {
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
