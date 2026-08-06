// Package settings persists user-tunable behaviour (concurrency, speed limit,
// extraction) as JSON in the data dir and hands out consistent snapshots.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// Shape is how rounded the whole interface is: "round", "soft" or "square".
	// One knob drives every corner, so the app never looks half-converted.
	Shape string `json:"shape"`
	// Accent is the one colour the interface uses for activity, as #rrggbb.
	// Empty means the built-in heraldic gold.
	Accent string `json:"accent"`

	// Rainbow replaces the single accent with a palette handed out by position,
	// so a long list of downloads reads as distinct rows instead of one gold
	// wall. It colours activity only, exactly like the accent it stands in for.
	Rainbow bool `json:"rainbow"`
	// RainbowReactive rests everything neutral and colours only what is hovered
	// or active: the restrained reading of the mode.
	RainbowReactive bool `json:"rainbowReactive"`
	// RainbowRotate offsets the palette by RainbowSeed, so a run does not always
	// begin on the same hue.
	RainbowRotate bool `json:"rainbowRotate"`
	// RainbowSeed is that offset. It is stored with the instance rather than in
	// the browser because two clients of one server showing different colours
	// for the same download is a bug, not a preference.
	RainbowSeed int `json:"rainbowSeed"`
	// RainbowPalette overrides the eight built-in hues. Empty means the default.
	RainbowPalette []string `json:"rainbowPalette"`
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
		Shape:           ShapeRound,
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

// The three shapes the interface offers. Anything else falls back to round
// rather than producing an interface with no radius rule at all.
const (
	ShapeRound  = "round"
	ShapeSoft   = "soft"
	ShapeSquare = "square"
)

// accentPattern is a plain six-digit hex colour. Accepting anything else would
// put attacker-chosen text straight into a CSS custom property.
var accentPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// RainbowSize is how many hues the palette has. It is fixed: the colours are
// handed out by position, so a palette that can change length would silently
// re-colour every existing row whenever the user added one.
const RainbowSize = 8

// sanitizePalette accepts a custom palette only in full. A palette with one
// unusable entry is not a palette with seven good colours, it is a palette that
// turns one row invisible, so the whole override is dropped back to the
// built-in hues.
func sanitizePalette(p []string) []string {
	if len(p) != RainbowSize {
		return nil
	}
	out := make([]string, 0, RainbowSize)
	for _, c := range p {
		c = strings.TrimSpace(c)
		if !accentPattern.MatchString(c) {
			return nil
		}
		out = append(out, c)
	}
	return out
}

func sanitize(n Settings) Settings {
	switch n.Shape {
	case ShapeRound, ShapeSoft, ShapeSquare:
	default:
		n.Shape = ShapeRound
	}
	n.Accent = strings.TrimSpace(n.Accent)
	if n.Accent != "" && !accentPattern.MatchString(n.Accent) {
		n.Accent = ""
	}
	n.RainbowPalette = sanitizePalette(n.RainbowPalette)
	// The seed is only ever read modulo the palette length, so it is folded here
	// and stored small enough to read at a glance in settings.json.
	if n.RainbowSeed < 0 {
		n.RainbowSeed = -n.RainbowSeed
	}
	n.RainbowSeed %= RainbowSize
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

// fixedPrefix returns the leading path segments of a folder template that hold
// no placeholder, which is the deepest directory that is the same for every
// task.
func fixedPrefix(dir string) string {
	if !strings.Contains(dir, "<") {
		return dir
	}
	sep := string(filepath.Separator)
	normalised := strings.ReplaceAll(dir, "/", sep)
	parts := strings.Split(normalised, sep)
	keep := parts[:0:0]
	for _, p := range parts {
		if strings.Contains(p, "<") {
			break
		}
		keep = append(keep, p)
	}
	if out := strings.Join(keep, sep); out != "" {
		return out
	}
	// Everything after the root is a placeholder; the root is what is left.
	return sep
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
	// A folder may be a template like /downloads/<jd:date>/<jd:packagename>.
	// Only the part before the first placeholder is a real path: creating the
	// rest would put folders literally named "<jd:date>" on disk, and checking
	// it would test a path that never exists at download time.
	dir = fixedPrefix(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".knightloader-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	return os.Remove(probe)
}
