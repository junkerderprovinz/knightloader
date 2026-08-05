// Package settings persists user-tunable behaviour (concurrency, speed limit,
// extraction) as JSON in the data dir and hands out consistent snapshots.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the user-visible configuration. Zero values mean "unlimited/off"
// where noted.
type Settings struct {
	MaxConcurrent int   `json:"maxConcurrent"` // global simultaneous downloads
	MaxPerHost    int   `json:"maxPerHost"`    // simultaneous downloads per host
	SpeedLimit    int64 `json:"speedLimit"`    // bytes/s for the embedded engine, 0 = unlimited
	Extract       bool  `json:"extract"`       // extract archives after download
	DeleteArchive bool  `json:"deleteArchive"` // remove the archive after successful extraction
}

// Defaults returns the settings a fresh install starts with.
func Defaults() Settings {
	return Settings{
		MaxConcurrent: 4,
		MaxPerHost:    2,
		SpeedLimit:    0,
		Extract:       true,
		DeleteArchive: false,
	}
}

type Store struct {
	path string

	mu  sync.Mutex
	cur Settings
}

// Load reads settings.json from dir, falling back to defaults.
func Load(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "settings.json"), cur: Defaults()}
	if b, err := os.ReadFile(s.path); err == nil {
		// Unmarshal over defaults so new fields keep their default value.
		if err := json.Unmarshal(b, &s.cur); err != nil {
			s.cur = Defaults()
		}
	}
	s.cur = sanitize(s.cur)
	return s, nil
}

// Get returns the current settings snapshot.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Set validates, persists and applies new settings.
func (s *Store) Set(n Settings) (Settings, error) {
	n = sanitize(n)
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return s.cur, err
	}
	if err := os.WriteFile(s.path, b, 0o600); err != nil {
		return s.cur, err
	}
	s.cur = n
	return n, nil
}

func sanitize(n Settings) Settings {
	if n.MaxConcurrent < 1 {
		n.MaxConcurrent = 1
	}
	if n.MaxConcurrent > 64 {
		n.MaxConcurrent = 64
	}
	if n.MaxPerHost < 1 {
		n.MaxPerHost = 1
	}
	if n.MaxPerHost > n.MaxConcurrent {
		n.MaxPerHost = n.MaxConcurrent
	}
	if n.SpeedLimit < 0 {
		n.SpeedLimit = 0
	}
	return n
}
