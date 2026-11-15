// Package qbittest is a qBittorrent WebUI that lives in memory, for the tests
// of everything that drives one: the login with its session cookie or an API
// key, the version calls, the preferences, the shutdown and the torrents.
package qbittest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
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
	// Modern answers torrents/stop and torrents/start, as 5.0 and later do,
	// instead of torrents/pause and torrents/resume.
	Modern bool
	// Interfaces are the network interfaces app/networkInterfaceList names.
	Interfaces []Interface
	// IgnoreProxy leaves the proxy off whatever setPreferences asks, as a
	// version that names the setting otherwise would.
	IgnoreProxy bool

	mu       sync.Mutex
	torrents map[string]*Torrent
	added    []AddCall
	deleted  []string
	sessions map[string]bool
	prefs    map[string]any
	switches []string
	logins   int
	shutdown bool
}

// Interface is a network interface as qBittorrent lists it, with its
// addresses.
type Interface struct {
	Name, Value string
	Addrs       []string
}

// New is a server that knows one login and runs 4.5.5.
func New(user, pass string) *Server {
	return &Server{Username: user, Password: pass, Version: "v4.5.5", WebAPI: "2.8.19"}
}

// legacyProxy reports whether the server names the proxy's type by number,
// with the types that log in as types of their own, as 4.5 and earlier do.
// Later versions name it and keep the login apart in proxy_auth_enabled.
func (s *Server) legacyProxy() bool {
	var major, minor int
	fmt.Sscanf(strings.TrimPrefix(s.Version, "v"), "%d.%d", &major, &minor)
	return major < 4 || major == 4 && minor < 6
}

func (s *Server) defaultsLocked() {
	if s.prefs != nil {
		return
	}
	s.prefs = map[string]any{"proxy_type": "None", "proxy_auth_enabled": false}
	if s.legacyProxy() {
		s.prefs["proxy_type"] = 0.0
	}
}

// setProxyLocked takes the proxy's type out of p the way the server's
// version reads it: a type it cannot read turns the proxy off.
func (s *Server) setProxyLocked(p map[string]any) {
	typ, ok := p["proxy_type"]
	if !ok {
		return
	}
	delete(p, "proxy_type")
	if s.IgnoreProxy {
		delete(p, "proxy_auth_enabled")
		return
	}
	if s.legacyProxy() {
		n, _ := typ.(float64)
		s.prefs["proxy_type"] = n
		s.prefs["proxy_auth_enabled"] = n == 3 || n == 4
		delete(p, "proxy_auth_enabled")
		return
	}
	name, ok := typ.(string)
	if !ok || !slices.Contains([]string{"None", "HTTP", "SOCKS5", "SOCKS4"}, name) {
		name = "None"
	}
	s.prefs["proxy_type"] = name
}

// Switches is every torrents/pause, resume, stop and start so far, each as
// the call and the hashes it named.
func (s *Server) Switches() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.switches)
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
		s.defaultsLocked()
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
		s.defaultsLocked()
		s.setProxyLocked(p)
		maps.Copy(s.prefs, p)
		s.mu.Unlock()
	case "/api/v2/app/networkInterfaceList":
		list := []map[string]string{}
		for _, i := range s.Interfaces {
			list = append(list, map[string]string{"name": i.Name, "value": i.Value})
		}
		writeJSON(w, list)
	case "/api/v2/app/networkInterfaceAddressList":
		addrs := []string{}
		for _, i := range s.Interfaces {
			if v := r.FormValue("iface"); v == "" || v == i.Value {
				addrs = append(addrs, i.Addrs...)
			}
		}
		writeJSON(w, addrs)
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
		if !s.serveTorrents(w, r) {
			http.NotFound(w, r)
		}
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
