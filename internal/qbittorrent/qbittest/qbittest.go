// Package qbittest is a qBittorrent WebUI that lives in memory, for the tests
// of everything that drives one: the login with its session cookie or an API
// key, the version calls, the preferences and the shutdown.
package qbittest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"sync"
)

// Server answers the part of the Web API v2 KnightLoader calls.
type Server struct {
	// Check decides a login; nil accepts Username with Password.
	Check              func(user, pass string) bool
	Username, Password string
	// APIKey, when set, is taken as a Bearer token, as 5.2 and later do.
	APIKey string
	// Version is what app/version answers, such as "v4.5.5".
	Version    string
	WebAPI     string
	OnShutdown func()

	mu       sync.Mutex
	sessions map[string]bool
	prefs    map[string]any
	logins   int
	shutdown bool
}

// New is a server that knows one login and runs 4.5.5.
func New(user, pass string) *Server {
	return &Server{Username: user, Password: pass, Version: "v4.5.5", WebAPI: "2.8.19"}
}

// Logins counts the logins that were accepted.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// ShutDown reports whether app/shutdown was called.
func (s *Server) ShutDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}

// Preferences is what app/setPreferences has set so far.
func (s *Server) Preferences() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.prefs)
}

// ForgetSessions ends every session, as a restart of qBittorrent does.
func (s *Server) ForgetSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = nil
}

func (s *Server) authorized(r *http.Request) bool {
	if s.APIKey != "" && r.Header.Get("Authorization") == "Bearer "+s.APIKey {
		return true
	}
	c, err := r.Cookie("SID")
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[c.Value]
}

// ServeHTTP answers like qBittorrent: 403 to a call without a session.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v2/auth/login" {
		s.login(w, r)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/api/v2/app/version":
		io.WriteString(w, s.Version)
	case "/api/v2/app/webapiVersion":
		io.WriteString(w, s.WebAPI)
	case "/api/v2/app/preferences":
		s.mu.Lock()
		b, _ := json.Marshal(s.prefs)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	case "/api/v2/app/setPreferences":
		var p map[string]any
		if r.Method != http.MethodPost || json.Unmarshal([]byte(r.FormValue("json")), &p) != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if s.prefs == nil {
			s.prefs = map[string]any{}
		}
		maps.Copy(s.prefs, p)
		s.mu.Unlock()
	case "/api/v2/app/shutdown":
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		if s.OnShutdown != nil {
			defer s.OnShutdown()
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	user, pass := r.FormValue("username"), r.FormValue("password")
	ok := user == s.Username && pass == s.Password
	if s.Check != nil {
		ok = s.Check(user, pass)
	}
	if r.Method != http.MethodPost || !ok {
		io.WriteString(w, "Fails.")
		return
	}
	raw := make([]byte, 16)
	rand.Read(raw)
	sid := hex.EncodeToString(raw)
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]bool{}
	}
	s.sessions[sid] = true
	s.logins++
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid, Path: "/", HttpOnly: true})
	io.WriteString(w, "Ok.")
}
