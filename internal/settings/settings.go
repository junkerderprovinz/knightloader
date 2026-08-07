// Package settings persists user-tunable behaviour (concurrency, speed limit,
// extraction) as JSON in the data dir and hands out consistent snapshots.
//
// This file holds the shape and the store: the Settings struct, the defaults, and
// Load/Get/Set. What each group of fields is allowed to contain lives with that
// group, in settings_queue.go, settings_paths.go, settings_appearance.go,
// settings_intake.go and settings_network.go, each with its own sanitize hook
// that sanitize below calls in turn. The struct is one declaration because Go
// gives it no choice and because embedding sub-structs would flatten differently
// in JSON and break every composite literal in the tree; the rules about it are
// what is split, and those were the four hundred lines nobody could edit at once.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
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

	// Packagizer names packages, picks folders and sets download options as
	// links are staged. It is stored exactly as the user wrote it: rules.Compile
	// is the validator, and a rule with a broken regular expression has to
	// round-trip to disk so the user can find and fix it in the form instead of
	// watching it disappear on save.
	Packagizer rules.Set `json:"packagizer"`
	// LinkFilter decides which links are taken into the collector at all.
	// StopAfterMatch usually wants to be on here, so a narrow accept placed above
	// a broad reject actually protects the link; it is the user's flag and
	// nothing here forces it.
	LinkFilter rules.Set `json:"linkFilter"`

	// MirrorPolicy is when two different URLs count as the same file.
	MirrorPolicy string `json:"mirrorPolicy"`

	// CollisionPolicy is what happens when the destination file already exists.
	CollisionPolicy string `json:"collisionPolicy"`
	// CollisionMaxAttempts caps how many counted names a rename tries. Zero means
	// the package's own cap.
	CollisionMaxAttempts int `json:"collisionMaxAttempts,omitempty"`

	// Connections is the user-ordered list of outbound connections downloads are
	// spread across. Empty means everything goes out over the machine's own
	// connection, which is what an install that never opened the page has.
	Connections []proxycfg.Entry `json:"connections,omitempty"`

	// Reconnect gets the box a new public address when a hoster's free-user limit
	// is keyed to the one it has. Off by default: it runs a program or talks to
	// the router, and neither should ever happen because a default said so.
	Reconnect reconnect.Config `json:"reconnect"`

	// Schedule is the timetable that pauses or throttles the queue by the clock.
	// An empty timetable changes nothing, which is what a fresh install wants.
	Schedule []schedule.Entry `json:"schedule,omitempty"`
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
		// Only three of the new fields have a default worth writing down. The rest
		// are usable at their zero value: no rules, no connections and no timetable
		// all mean "behave exactly as before", which is what a fresh install wants.
		MirrorPolicy:    string(dedupe.DefaultPolicy),
		CollisionPolicy: string(collide.DefaultPolicy),
		Reconnect:       reconnect.Defaults(),
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
	s.mu.Lock()
	defer s.mu.Unlock()
	// The secrets the client was never shown are put back first, and against the
	// value under this very lock. Reading the previous settings through Get would
	// deadlock — mu is a plain Mutex and Get takes it — and taking a snapshot
	// before the lock would let two concurrent saves merge against the same stale
	// value, so the second one writes back a router password the first had
	// already changed.
	n.Reconnect = n.Reconnect.WithSecretsFrom(s.cur.Reconnect)
	n.Connections = proxycfg.Merge(n.Connections, s.cur.Connections)
	n = sanitize(n)
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

// sanitize is the one path everything written to disk goes down, and it does
// nothing itself: each group of fields is cleaned by the file that owns it. A
// new setting therefore lands in one domain file and one line of this list,
// which is what lets several people add settings in the same wave without
// meeting in the middle of a four-hundred-line function.
//
// The hooks are independent — no hook reads a field another one rewrites — so
// the order below is for reading, not for correctness.
func sanitize(n Settings) Settings {
	n = sanitizeAppearance(n)
	n = sanitizeQueue(n)
	n = sanitizePaths(n)
	n = sanitizeIntake(n)
	n = sanitizeNetwork(n)
	n = sanitizeRules(n)
	return n
}
