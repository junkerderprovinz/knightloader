// Package mcpclient keeps the AI clients set up to reach this instance over
// the Model Context Protocol, each tied to the API token made for it.
//
// Only a token named here is let in at /mcp, so the key typed into Sonarr
// cannot drive an assistant's tools, and removing the last client closes the
// endpoint again.
package mcpclient

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const fileName = "mcp-clients.json"

// MaxClients bounds the list the way apitoken.MaxTokens bounds the tokens.
const MaxClients = 20

// touchEvery is how old LastCall has to be before Touch writes the file, so
// an assistant at work does not cost a disk write per request.
const touchEvery = time.Minute

var (
	ErrNotFound    = errors.New("mcpclient: no such client")
	ErrNoKind      = errors.New("mcpclient: a client needs a kind")
	ErrNoToken     = errors.New("mcpclient: a client needs a token")
	ErrTooMany     = errors.New("mcpclient: too many clients; remove one before adding another")
	ErrTokenInUse  = errors.New("mcpclient: that token belongs to another client")
	errKindTooLong = errors.New("mcpclient: the client kind is too long")
)

// Client is one assistant set up here.
type Client struct {
	ID string `json:"id"`
	// Kind is the id of the client in the interface's list, such as
	// "claude-code", or "other".
	Kind    string    `json:"kind"`
	TokenID string    `json:"tokenId"`
	AddedAt time.Time `json:"addedAt"`
	// LastCall is when the client last reached /mcp, nil until its first call.
	LastCall *time.Time `json:"lastCall,omitempty"`
}

// Store persists the list in the data dir.
type Store struct {
	path string

	mu      sync.Mutex
	clients []Client
	written map[string]time.Time
}

// Open loads (or creates) the list in dir. A file that does not parse starts
// an empty list, so a damaged file closes the endpoint instead of the server.
func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, fileName), written: map[string]time.Time{}}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if json.Unmarshal(b, &s.clients) != nil {
		s.clients = nil
	}
	return s, nil
}

// List returns every client, oldest first.
func (s *Store) List() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, len(s.clients))
	copy(out, s.clients)
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAt.Before(out[j].AddedAt) })
	return out
}

// Add records a client of kind reached with tokenID.
func (s *Store) Add(kind, tokenID string) (Client, error) {
	kind = strings.TrimSpace(kind)
	switch {
	case kind == "":
		return Client{}, ErrNoKind
	case len(kind) > 64:
		return Client{}, errKindTooLong
	case tokenID == "":
		return Client{}, ErrNoToken
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.clients) >= MaxClients {
		return Client{}, ErrTooMany
	}
	for _, c := range s.clients {
		if c.TokenID == tokenID {
			return Client{}, ErrTokenInUse
		}
	}
	c := Client{ID: newID(), Kind: kind, TokenID: tokenID, AddedAt: time.Now().UTC()}
	s.clients = append(s.clients, c)
	if err := s.flushLocked(); err != nil {
		s.clients = s.clients[:len(s.clients)-1]
		return Client{}, err
	}
	return c, nil
}

// Remove drops a client by id and returns it, so the caller can revoke its
// token.
func (s *Store) Remove(id string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.clients {
		if c.ID != id {
			continue
		}
		prev := s.clients
		s.clients = append(append([]Client{}, prev[:i]...), prev[i+1:]...)
		if err := s.flushLocked(); err != nil {
			s.clients = prev
			return Client{}, err
		}
		delete(s.written, id)
		return c, nil
	}
	return Client{}, ErrNotFound
}

// ByToken returns the client reached with tokenID.
func (s *Store) ByToken(tokenID string) (Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		if c.TokenID == tokenID {
			return c, true
		}
	}
	return Client{}, false
}

// Empty reports whether no client is set up, which is when /mcp is closed.
func (s *Store) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients) == 0
}

// Touch stamps the client's last call. The list in memory always has it; the
// file gets it at most once per touchEvery.
func (s *Store) Touch(id string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.clients {
		if s.clients[i].ID != id {
			continue
		}
		at := now.UTC()
		s.clients[i].LastCall = &at
		if now.Sub(s.written[id]) >= touchEvery {
			s.written[id] = now
			// A failed write costs the stamp after a restart, nothing more.
			_ = s.flushLocked()
		}
		return
	}
}

func (s *Store) flushLocked() error {
	b, err := json.MarshalIndent(s.clients, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
