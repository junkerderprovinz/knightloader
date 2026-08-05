// Package api exposes the app over HTTP: a small REST surface plus a WebSocket
// stream under /api, and the embedded SPA everywhere else.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/hub"
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
