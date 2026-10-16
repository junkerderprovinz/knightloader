// Package playback watches media servers for somebody watching something, so
// the queue can step aside while they do.
//
// A download manager on the box that also runs Plex or Jellyfin competes with
// every stream it serves: for the uplink when the viewer is away from home, and
// for the disk and the processor when an archive is unpacked next to a
// transcode. The queue already has a second set of limits for that, quiet mode;
// what it lacked was anybody to press the button when a film starts. This
// package is that somebody. It asks each configured server what is playing
// every few seconds and reports one bit, "something that counts is playing",
// and internal/app turns the bit into quiet mode.
//
// Unlike internal/mediahook, this package names products. A library refresh is
// one HTTP call any server takes in the same shape, so an address and a header
// cover them all; a list of sessions comes back in a different document from
// each, and reading it means knowing whose it is.
//
// The token each server needs is sealed in accounts.Store under Service, the
// arrangement mediahook uses, for the reasons its package comment gives:
// settings.json travels into the diagnostics bundle and the Advanced key table.
package playback

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Service is the pseudo catalogue id the tokens are filed under in the shared
// accounts.Store, with the server id as the account half of the key.
const Service = "playback"

// Kind is which media server a row talks to, and so which document comes back.
type Kind string

const (
	KindPlex     Kind = "plex"
	KindJellyfin Kind = "jellyfin"
	KindEmby     Kind = "emby"
)

// Kinds is the menu GET /api/options serves, in the order a picker shows it.
func Kinds() []Kind { return []Kind{KindPlex, KindJellyfin, KindEmby} }

func (k Kind) valid() bool {
	switch k {
	case KindPlex, KindJellyfin, KindEmby:
		return true
	}
	return false
}

// MaxServers caps the table. A home has one media server and sometimes two.
const MaxServers = 8

const (
	maxServerID   = 64
	maxServerName = 120
)

// MaxPausedMinutes is the longest a paused stream may hold quiet mode: a day,
// past which "paused" means somebody went to bed with the player open.
const MaxPausedMinutes = 24 * 60

// DefaultPausedMinutes is how long a paused stream keeps counting. Half an hour
// covers a phone call or a meal, and a stream left paused longer than that is
// usually forgotten rather than about to resume.
const DefaultPausedMinutes = 30

// Server is one media server to watch. Every field is safe to write to disk,
// show on a page and put in a bug report; the token is not a field, see the
// package comment.
type Server struct {
	// ID is the key the token is sealed under. Nothing else points at it, so
	// the route makes one up from the kind when a new row arrives without one.
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Kind Kind   `json:"kind"`
	// URL is the server's own address, the one its web interface answers on,
	// path prefix included when it sits behind one (http://host/jellyfin).
	URL string `json:"url"`
}

// Settings is the feature's switches as settings.json holds them. The servers
// are a list of their own beside it, written only by their own route, so the
// settings page saving a switch cannot put back a server list it loaded before
// a row was added.
type Settings struct {
	Enabled bool `json:"enabled"`
	// IgnoreLocal leaves out streams to a player on the server's own network.
	// Off by default: a stream on the sofa takes nothing from the uplink but
	// takes as much disk as any other, and the unpacking pause is about the
	// disk.
	IgnoreLocal bool `json:"ignoreLocal"`
	// PausedMinutes is how long a paused stream keeps counting; 0 lets go of a
	// stream the moment it pauses.
	PausedMinutes int `json:"pausedMinutes"`
	// HoldExtraction keeps the unpacking queue waiting while a stream counts.
	// It is read by internal/app; it lives here so the feature is one key.
	HoldExtraction bool `json:"holdExtraction"`
}

// Defaults is the feature switched off with the answers it ships with.
func Defaults() Settings {
	return Settings{PausedMinutes: DefaultPausedMinutes, HoldExtraction: true}
}

func (s Settings) pausedFor() time.Duration { return time.Duration(s.PausedMinutes) * time.Minute }

// ServerID folds the spellings of one id into one, under the rule
// mediahook.HookID applies, and answers "" for anything unusable.
func ServerID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" || len(id) > maxServerID {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return ""
		}
	}
	return id
}

// FreeID is the first of kind, kind-2, kind-3... that no row in taken holds.
func FreeID(kind Kind, taken []Server) string {
	used := make(map[string]bool, len(taken))
	for _, s := range taken {
		used[ServerID(s.ID)] = true
	}
	id := string(kind)
	for n := 2; used[id]; n++ {
		id = fmt.Sprintf("%s-%d", kind, n)
	}
	return id
}

// Validate reports the first thing wrong with one row, in words meant for the
// person looking at the form. The address is never quoted back: a pasted Plex
// address can carry its token in the query.
func (s Server) Validate() error {
	if ServerID(s.ID) == "" {
		return fmt.Errorf("the id %q holds something other than letters, digits and - _ or . (at most %d characters)", s.ID, maxServerID)
	}
	if !s.Kind.valid() {
		return fmt.Errorf("%s: %q is not a media server this build can read; pick plex, jellyfin or emby", s.ID, s.Kind)
	}
	u, err := url.Parse(strings.TrimSpace(s.URL))
	if err != nil {
		return fmt.Errorf("%s: that is not an address. Give the server's address with the scheme, http://192.168.1.10:8096 for example", s.ID)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("%s: the address has to start with http:// or https://", s.ID)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: the address names no host", s.ID)
	}
	if u.RawQuery != "" {
		return fmt.Errorf("%s: leave the query out of the address. The token goes in its own field, where it is sealed instead of being written to settings.json", s.ID)
	}
	return nil
}

// Sanitize keeps the switches' one figure in range.
func Sanitize(in Settings) Settings {
	in.PausedMinutes = min(max(in.PausedMinutes, 0), MaxPausedMinutes)
	return in
}

// ValidateServers reports the first thing wrong with the list, for a save that
// would otherwise lose a row to SanitizeServers without saying so.
func ValidateServers(in []Server) error {
	if len(in) > MaxServers {
		return fmt.Errorf("there are %d media servers; the limit is %d", len(in), MaxServers)
	}
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if err := s.Validate(); err != nil {
			return err
		}
		id := ServerID(s.ID)
		if seen[id] {
			return fmt.Errorf("%s: two media servers have this id", id)
		}
		seen[id] = true
	}
	return nil
}

// SanitizeServers bounds a list read from disk: it drops rows that could never
// be asked and the second of two rows with one id. The slice is rebuilt, never
// edited in place.
func SanitizeServers(in []Server) []Server {
	out := make([]Server, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s.ID = ServerID(s.ID)
		s.Name = trimTo(s.Name, maxServerName)
		s.URL = strings.TrimSpace(s.URL)
		if s.ID == "" || seen[s.ID] || s.Validate() != nil {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
		if len(out) == MaxServers {
			break
		}
	}
	return out
}

// trimTo trims whitespace and then length, on a rune boundary.
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "")
}

// Host is host[:port] as the server will be reached, for the card's "asks X"
// line, or "" for an address that does not parse.
func (s Server) Host() string {
	u, err := url.Parse(strings.TrimSpace(s.URL))
	if err != nil {
		return ""
	}
	return u.Host
}

// isLocalAddr reports whether a player's address is on the server's own side
// of the router: loopback, a private range or link-local. An address that does
// not parse is not local, so a stream nobody can place still counts.
func isLocalAddr(raw string) bool {
	raw = strings.TrimSpace(raw)
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	ip := net.ParseIP(strings.Trim(raw, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
