// Package api exposes the app over HTTP: a small REST surface plus a WebSocket
// stream under /api, and the embedded SPA everywhere else.
package api

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/hub"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/web"
)

// Handler builds the full HTTP handler.
func Handler(a *app.App) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Tasks())
	})
	mux.HandleFunc("POST /api/links", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Links   string `json:"links"` // newline-separated, like JD's paste box
			Package string `json:"package"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		urls := strings.FieldsFunc(body.Links, func(r rune) bool { return r == '\n' || r == '\r' })
		writeJSON(w, a.AddLinks(urls, body.Package))
	})
	mux.HandleFunc("POST /api/tasks/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // empty/absent = start all collected
		a.StartTasks(body.Ids)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		a.Pause(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		a.Resume(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.Remove(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Settings.Get())
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var s settings.Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		applied, err := a.ApplySettings(s)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, applied)
	})
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Accounts.Services())
	})
	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Service string `json:"service"`
			Secret  string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Service == "" {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.SetAccount(body.Service, body.Secret); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Federation.List())
	})
	mux.HandleFunc("POST /api/instances", func(w http.ResponseWriter, r *http.Request) {
		var in federation.Instance
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.Federation.Add(in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		online := a.Federation.Ping(r.Context(), in.Name) == nil
		writeJSON(w, map[string]any{"name": in.Name, "url": in.URL, "online": online})
	})
	mux.HandleFunc("DELETE /api/instances/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Federation.Remove(r.PathValue("name")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Proxy task operations to a peer instance: only the task/link routes are
	// forwarded, so a peer's settings/accounts stay local to that peer.
	mux.HandleFunc("/api/instances/{name}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET /api/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(a, w, r)
	})

	mux.Handle("/", spaHandler())
	return cors(mux)
}

func serveWS(a *app.App, w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	a.Hub.Add(c)
	defer func() {
		a.Hub.Remove(c)
		c.CloseNow()
	}()
	_ = hub.Send(r.Context(), c, "snapshot", a.Tasks())
	for {
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

// spaHandler serves the embedded build, falling back to index.html for
// client-side routes.
func spaHandler() http.Handler {
	sub, _ := fs.Sub(web.Dist, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
