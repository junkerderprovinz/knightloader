// Package settings persists user-tunable behaviour (concurrency, speed limit,
// extraction) as JSON in the data dir and hands out consistent snapshots.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings is the user-visible configuration. Zero values mean "unlimited/off"
// where noted.
type Settings struct {
	MaxConcurrent int   `json:"maxConcurrent"` // global simultaneous downloads
	MaxPerHost    int   `json:"maxPerHost"`    // simultaneous downloads per host
	SpeedLimit    int64 `json:"speedLimit"`    // bytes/s, 0 = unlimited
	Extract       bool  `json:"extract"`       // extract archives after download
	DeleteArchive bool  `json:"deleteArchive"` // remove the archive after successful extraction
	AutoStart     bool  `json:"autoStart"`     // start collected links immediately instead of staging

	// DownloadDir is where finished files land. Empty means the built-in
	// default inside the data directory.
	DownloadDir string `json:"downloadDir"`
	// SubfolderByPackage puts each package in its own folder below DownloadDir.
	SubfolderByPackage bool `json:"subfolderByPackage"`
	// ArchivePasswords are tried in order when extracting an encrypted archive.
	ArchivePasswords []string `json:"archivePasswords"`
	// MaxRetries is how often a failed download is retried automatically.
	MaxRetries int `json:"maxRetries"`
	// Crawl lets a pasted page URL be opened and the files it links to be
	// staged, instead of the page itself becoming one task.
	Crawl bool `json:"crawl"`
	// WatchDir is a folder whose dropped .txt/.crawljob files are picked up.
	// Empty disables the watcher.
	WatchDir string `json:"watchDir"`
	// VerifyChecksums checks a finished download against a checksum file that
	// came with it, when one did.
	VerifyChecksums bool `json:"verifyChecksums"`
}

// Defaults returns the settings a fresh install starts with.
func Defaults() Settings {
	return Settings{
		MaxConcurrent:   4,
		MaxPerHost:      2,
		SpeedLimit:      0,
		Extract:         true,
		DeleteArchive:   false,
		MaxRetries:      3,
		Crawl:           true,
		VerifyChecksums: true,
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
	if n.MaxRetries < 0 {
		n.MaxRetries = 0
	}
	if n.MaxRetries > 20 {
		n.MaxRetries = 20
	}
	n.DownloadDir = strings.TrimSpace(n.DownloadDir)
	n.WatchDir = strings.TrimSpace(n.WatchDir)
	// A relative watch folder has the same problem as a relative download
	// folder: nobody can say where it actually is.
	if n.WatchDir != "" && !filepath.IsAbs(n.WatchDir) {
		n.WatchDir = ""
	}
	// A relative path would be resolved against whatever the process's working
	// directory happens to be, which is not something a user can reason about.
	if n.DownloadDir != "" && !filepath.IsAbs(n.DownloadDir) {
		n.DownloadDir = ""
	}
	var pw []string
	for _, p := range n.ArchivePasswords {
		if p = strings.TrimSpace(p); p != "" {
			pw = append(pw, p)
		}
	}
	n.ArchivePasswords = pw
	return n
}

// Validate reports why a download directory cannot be used, so the API can
// refuse a bad path instead of silently downloading somewhere else.
func Validate(dir string) error {
	if dir == "" {
		return nil // the built-in default is always usable
	}
	if !filepath.IsAbs(dir) {
		return errors.New("the download folder must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".knightloader-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	return os.Remove(probe)
}
