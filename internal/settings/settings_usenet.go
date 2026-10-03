package settings

// The Usenet servers KnightLoader fetches an .nzb from itself. The host, port
// and limits are configuration and live here; the username and password are
// sealed in accounts.Store under UsenetService and the server's id.

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// UsenetService is the pseudo catalogue id the server logins are sealed under.
const UsenetService = "nntp"

const (
	// MaxUsenetServers bounds the table. Nobody has twenty providers, and
	// every one of them gets its own connections.
	MaxUsenetServers = 20
	// DefaultUsenetConnections is what a server gets when the form leaves the
	// field empty, below what most providers allow.
	DefaultUsenetConnections = 8
	// MaxUsenetConnections and MaxUsenetLevel bound what a hand-edited file
	// can ask for.
	MaxUsenetConnections = 100
	MaxUsenetLevel       = 9
)

// UsenetServer is one server's settings row.
type UsenetServer struct {
	// ID names the row and the sealed login. It is made when the row is
	// added and never changes.
	ID   string `json:"id"`
	Host string `json:"host"`
	// Port is 0 for the usual one: 563 with TLS, 119 without.
	Port        int  `json:"port"`
	TLS         bool `json:"tls"`
	Connections int  `json:"connections"`
	// Level 0 is asked first; a higher level only for what the levels below
	// lack (see nntp.Server).
	Level         int  `json:"level"`
	RetentionDays int  `json:"retentionDays"`
	Optional      bool `json:"optional"`
	Enabled       bool `json:"enabled"`
}

// UsenetServerID turns raw into a row id, or "" when it cannot be one: letters,
// digits and - _ . only, at most 64 of them.
func UsenetServerID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" || len(id) > 64 {
		return ""
	}
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return ""
		}
	}
	return id
}

// UsenetHost turns raw into a host to dial, or "" when it cannot be one: a
// name of letters, digits and - _ . or an IP address, IPv6 with or without
// its brackets.
func UsenetHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if inner, ok := strings.CutPrefix(host, "["); ok {
		host, ok = strings.CutSuffix(inner, "]")
		if !ok || net.ParseIP(host) == nil {
			return ""
		}
	}
	if net.ParseIP(host) != nil {
		return host
	}
	if host == "" || len(host) > 253 {
		return ""
	}
	for _, r := range host {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return ""
		}
	}
	return host
}

// Validate reports the first thing wrong with one row, in words meant for the
// form.
func (s UsenetServer) Validate() error {
	switch {
	case UsenetServerID(s.ID) == "":
		return errors.New("id: letters, digits and - _ . only, at most 64 characters")
	case UsenetHost(s.Host) == "":
		return errors.New("host: the server's name without a port or scheme, such as news.example.com")
	case s.Port < 0 || s.Port > 65535:
		return errors.New("port: 1 to 65535, or empty for the usual one")
	case s.Connections < 1 || s.Connections > MaxUsenetConnections:
		return fmt.Errorf("connections: 1 to %d", MaxUsenetConnections)
	case s.Level < 0 || s.Level > MaxUsenetLevel:
		return fmt.Errorf("level: 0 to %d", MaxUsenetLevel)
	case s.RetentionDays < 0:
		return errors.New("retention: a number of days, or 0 for no limit")
	}
	return nil
}

// ValidateUsenetServers checks every row and that no id is used twice.
func (s Settings) ValidateUsenetServers() error {
	if len(s.UsenetServers) > MaxUsenetServers {
		return &FieldError{Field: "usenetServers", Err: fmt.Errorf("there are %d servers; the limit is %d", len(s.UsenetServers), MaxUsenetServers)}
	}
	seen := map[string]bool{}
	for i, srv := range s.UsenetServers {
		if err := srv.Validate(); err != nil {
			return &FieldError{Field: fmt.Sprintf("usenetServers.%d", i), Err: err}
		}
		if seen[srv.ID] {
			return &FieldError{Field: fmt.Sprintf("usenetServers.%d", i), Err: fmt.Errorf("id: %q is used twice", srv.ID)}
		}
		seen[srv.ID] = true
	}
	return nil
}

// sanitizeUsenet drops rows that could never connect and pulls the numbers of
// a hand-edited file into range.
func sanitizeUsenet(n Settings) Settings {
	out := n.UsenetServers[:0:0]
	seen := map[string]bool{}
	for _, srv := range n.UsenetServers {
		srv.ID = UsenetServerID(srv.ID)
		srv.Host = UsenetHost(srv.Host)
		if srv.ID == "" || srv.Host == "" || seen[srv.ID] || len(out) == MaxUsenetServers {
			continue
		}
		seen[srv.ID] = true
		if srv.Port < 0 || srv.Port > 65535 {
			srv.Port = 0
		}
		if srv.Connections <= 0 {
			srv.Connections = DefaultUsenetConnections
		}
		srv.Connections = min(srv.Connections, MaxUsenetConnections)
		srv.Level = min(max(srv.Level, 0), MaxUsenetLevel)
		srv.RetentionDays = max(srv.RetentionDays, 0)
		out = append(out, srv)
	}
	n.UsenetServers = out
	return n
}
