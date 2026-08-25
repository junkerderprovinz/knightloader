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
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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

	// AutoConfirm, AutoConfirmDelay and AutoStart are the three fields a
	// single AutoStart boolean used to be, and MUST be read together - see
	// migrateAutoStart in settings_confirm.go for the migration this split
	// demands from every existing install.
	//
	// AutoConfirm moves a batch out of the collector on its own, without a
	// click - what the old flag actually gated, under the name that
	// conflated it with AutoStart below.
	AutoConfirm bool `json:"autoConfirm"`
	// AutoConfirmDelay is how long AutoConfirm waits before it fires, in
	// seconds. Zero fires the instant a batch is staged, which is what
	// every install had before this field existed - there was no delay to
	// preserve, only the one it would be wrong to invent on their behalf.
	AutoConfirmDelay int `json:"autoConfirmDelay"`
	// AutoStart is what a confirmed batch does next: start immediately
	// (true, the default) or sit in the queue until something explicitly
	// releases it (false). Before this split there was no way to ask for
	// the second half on its own - confirming a link, however it happened,
	// always started it - so AutoStart defaults to true precisely to keep
	// that the case for every install that never touches this setting.
	// "Confirm without start" (AutoConfirm=true, AutoStart=false) is the
	// state this split makes possible for the first time.
	AutoStart bool `json:"autoStart"`

	// OnDupes and OnOffline are the confirm-time policies for a link that
	// duplicates one already in the list, or one a check has already found
	// gone - internal/confirm.Policy, stored as its string form the same
	// way MirrorPolicy and CollisionPolicy are. These are the INSTANCE's
	// own defaults; a batch may carry its own override of either (see
	// internal/app.ConfirmTasks), read against these two when it does not.
	OnDupes   string `json:"onDupes"`
	OnOffline string `json:"onOffline"`
	// AddAtTop puts a batch leaving the collector at the front of the wait
	// order instead of the back, so it plays next rather than after
	// whatever was already queued.
	AddAtTop bool `json:"addAtTop"`

	// DownloadDir is where finished files land. Empty means the built-in
	// default inside the data directory.
	DownloadDir string `json:"downloadDir"`
	// SubfolderByPackage puts each package in its own folder below DownloadDir.
	SubfolderByPackage bool `json:"subfolderByPackage"`
	// ArchivePasswords are tried in order when extracting an encrypted archive.
	ArchivePasswords []string `json:"archivePasswords"`

	// ExtractTo collects extractions in one folder instead of leaving each one
	// beside its archive. Empty keeps the old behaviour, which is what most
	// people expect and what every install had before the setting existed. It
	// may be a pathvars template, expanded per task like DownloadDir.
	ExtractTo string `json:"extractTo"`
	// ExtractSubfolder puts each package in its own folder below ExtractTo. It
	// does nothing without ExtractTo - see extract.Options.
	ExtractSubfolder bool `json:"extractSubfolder"`
	// ExtractCollision is what an extraction does when its destination folder is
	// already there: rename, skip or overwrite, decided per folder.
	ExtractCollision string `json:"extractCollision"`
	// ArchiveDisposal is what happens to an archive that unpacked cleanly:
	// keep, trash or delete.
	//
	// This key replaced the boolean `deleteArchive`, and the two do not live
	// side by side: a settings file written by an older build is mapped on the
	// way in by migrate() below. A JSON field that changes type is the one
	// change that breaks the round-trip for every existing install, so the old
	// spelling is read exactly once, at load, and never written again.
	ArchiveDisposal string `json:"archiveDisposal"`
	// TrashRetentionDays is how long a trashed archive stays before the sweep
	// takes it. Zero never sweeps.
	TrashRetentionDays int `json:"trashRetentionDays"`
	// DeleteInfoFiles sweeps the .nfo/.sfv/.diz/.url that came with the same
	// package as the archive, using the same disposal.
	DeleteInfoFiles bool `json:"deleteInfoFiles"`
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
	// PreParserEnabled turns on internal/linkscan for POST /api/links: the
	// pasted or dropped blob is scanned for links wherever they sit in it,
	// instead of one line being taken as one link verbatim. Off falls back
	// to that older, literal behaviour. Named and defaulted after
	// JDownloader's own AddLinksPreParserEnabled (verified against
	// JDownloader's own source, CFG_LINKGRABBER and LinkgrabberSettings.java:
	// same key, same true default, same "works on the pasted text as-is"
	// meaning for off), not a spelling picked from the plan's prose.
	PreParserEnabled bool `json:"preParserEnabled"`

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

	// HideAccountsFromSidebar removes the sidebar's own "Konten" nav item,
	// for someone who only ever reaches accounts through the identical
	// settings tab and finds the second entry point redundant rather than
	// convenient. The zero value (false) keeps the current, pre-existing
	// behaviour - both the nav item and the settings tab render the same
	// page either way, so hiding one costs nothing but a click.
	HideAccountsFromSidebar bool `json:"hideAccountsFromSidebar"`

	// AutoUpdateCheck asks the desktop build to call update.Check once at
	// startup (and the Allgemein tab to do the same on load) instead of only
	// on an explicit click of "Check for updates" - desktop only in
	// practice, read nowhere on the container build. Off by default: it is
	// an outbound call to GitHub on every launch, and that is an opt-in, not
	// something a fresh install does before being asked.
	AutoUpdateCheck bool `json:"autoUpdateCheck"`

	// AutoUpdateInstall asks the desktop build to install a newer release
	// (download, verify, swap the running binary, relaunch) the moment
	// AutoUpdateCheck's own check finds one, instead of only offering the
	// release page to fetch by hand. Meaningless without AutoUpdateCheck
	// also being on - nothing reads this unless a check already found an
	// update - and meaningless on the container build, which cannot replace
	// itself from the inside (App.RequestUpdateInstall is nil there; the
	// route refuses before this field is ever read). Off by default, for
	// the same reason AutoUpdateCheck is: silently replacing your own
	// running binary is a bigger step than an outbound version check, and
	// opting into "check" does not imply opting into "also apply".
	AutoUpdateInstall bool `json:"autoUpdateInstall"`

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
	// KeepMirrors keeps the second copy instead of dropping it: the link is
	// staged as a sibling of the download it mirrors, parked, and labelled with
	// the task it is a copy of.
	//
	// Off by default, and the reason is that nothing fails over to a sibling on
	// its own yet. What it buys today is that the alternative link survives - a
	// dropped mirror lives on only in an in-memory trace that the next restart
	// clears - and the price of it being on is a parked row per mirror in a list
	// people already complain is long. On is a choice; off is what the list looks
	// like now.
	KeepMirrors bool `json:"keepMirrors"`

	// CollisionPolicy is what happens when the destination file already exists.
	CollisionPolicy string `json:"collisionPolicy"`
	// CollisionMaxAttempts caps how many counted names a rename tries. Zero means
	// the package's own cap.
	CollisionMaxAttempts int `json:"collisionMaxAttempts,omitempty"`

	// Connections is the user-ordered list of outbound connections downloads are
	// spread across. Empty means everything goes out over the machine's own
	// connection, which is what an install that never opened the page has.
	Connections []proxycfg.Entry `json:"connections,omitempty"`

	// Chunks is how many connections ONE download opens, when neither the task
	// nor a rule has named a number. It is not about the list above: Connections
	// is which way out of the machine the bytes go, this is how many sockets one
	// file is pulled over.
	//
	// Zero is "no opinion", exactly as on the task and for the same reason - the
	// dispatcher owns the fallback, and a copy of that number here is a second
	// one to forget when the first is changed.
	Chunks int `json:"chunks"`

	// Reconnect gets the box a new public address when a hoster's free-user limit
	// is keyed to the one it has. Off by default: it runs a program or talks to
	// the router, and neither should ever happen because a default said so.
	Reconnect reconnect.Config `json:"reconnect"`

	// Schedule is the timetable that pauses or throttles the queue by the clock.
	// An empty timetable changes nothing, which is what a fresh install wants.
	//
	// It stays a field of this struct rather than a file of its own beside
	// settings.json - see the doc comment on PUT /api/schedule in
	// routes_schedule.go for why that is a considered choice and not an
	// oversight, and setFeature's "scheduler" case in routes_features.go for
	// the read-current/write-one-field shape every writer of this field, this
	// route included, is expected to use.
	Schedule []schedule.Entry `json:"schedule,omitempty"`

	// IdleAction is what happens once the wait queue has nothing enabled left
	// to run, start or finish, after a cancellable countdown - see
	// internal/idleaction. Embedded here rather than in a file of its own for
	// the same reason Schedule just above is: one small struct, one settings
	// page, no secret in it anywhere. The zero value is Action=ActionNone, so
	// a fresh install - and an upgrade that has never seen this key - has
	// nothing armed.
	IdleAction idleaction.Config `json:"idleAction"`

	// ResumeOnStart is what happens to the downloads that were in flight when
	// the process last stopped: never, only what was running, or everything
	// unfinished. See the constants for what each one costs.
	ResumeOnStart string `json:"resumeOnStart"`
	// KeepFinishedDays is how long a finished download stays in the LIST. Zero
	// keeps it forever.
	//
	// It never touches the file. Removing a row and deleting what was downloaded
	// are two different actions in this app and always have been - conflating
	// them is the bug that cost somebody their downloads on the ordinary "clear
	// finished" path, and this is the same path running on a timer. What was
	// fetched is kept in the history table, which retention does not read.
	KeepFinishedDays int `json:"keepFinishedDays"`
	// HistoryMax caps the download history. Zero keeps every entry, which is a
	// table that only grows on an instance that is never restarted.
	HistoryMax int `json:"historyMax"`

	// CaptchaSolverOrder is which automatic captcha-solving services
	// (internal/accounts.Catalogue ids "2captcha"/"anticaptcha") to try, and
	// in what order, before a captcha is ever shown to a human. Membership
	// AND order live in the one list - an id absent from it is not tried at
	// all, exactly the same "presence in an ordered list is the switch" rule
	// the accounts page's own resolver-priority order already uses - rather
	// than a separate bool per service that could disagree with where the
	// service sits in the order. Empty means what a fresh install has:
	// nothing configured, straight to the prompt modal, whether or not a key
	// happens to be stored - an id here with no matching credential is
	// simply skipped when tried (see sanitizeCaptcha for why an id here
	// never implies a stored key, and never the reverse). This is the
	// NON-secret half; the API key itself is a credential
	// (internal/accounts), never a settings field - see
	// internal/accounts/catalogue.go's GroupCaptchaSolver.
	//
	// No omitempty, deliberately, matching RainbowPalette just above rather
	// than ArchivePasswords further up: a nil slice with omitempty is
	// DROPPED from the JSON entirely, and web/src/lib/api.ts's Settings
	// type has no way to type a field that is sometimes simply absent. A
	// nil slice with no omitempty encodes as JSON null instead, so the
	// field is always present and the frontend types it `string[] | null`,
	// the same pairing RainbowPalette already uses.
	CaptchaSolverOrder []string `json:"captchaSolverOrder"`

	// Ytdlp is the yt-dlp backend's own configuration - format/quality
	// selection, subtitles, the output filename template, whether a
	// playlist URL fetches one video or the whole list. See
	// internal/resolver/ytdlp's own doc comment on Options for why every
	// field's zero value reproduces this backend's behaviour from before
	// any of them existed - an install that never opens the settings page
	// this backs downloads exactly as it always has.
	Ytdlp ytdlp.Options `json:"ytdlp"`

	// YtdlpPresets is per-host (e.g. "youtube.com") config for the
	// "Variante" rows a yt-dlp link now stages (see ytdlp.HosterPreset's
	// own doc comment): which of video/audio/thumbnail/subtitle/
	// description land in the collector by default for links from that
	// host, and the default quality/audio-format for the two variants that
	// have one. A host with no entry here gets ytdlp.DefaultHosterPreset()
	// - map, not omitempty, matching CaptchaSolverOrder's own reasoning
	// just above for why a field a caller has never touched should not
	// vanish from the JSON rather than round-trip as an empty object.
	YtdlpPresets map[string]ytdlp.HosterPreset `json:"ytdlpPresets"`

	// Torrent is the seed/port/DHT/PEX policy for the BitTorrent backend -
	// see settings_torrent.go for the full shape and, especially, for what
	// of it is and is not actually enforced by the gopeed dependency this
	// build embeds today.
	Torrent Torrent `json:"torrent"`

	// InstanceID, InstanceName and KnownDomains are this instance's own
	// identity - see settings_identity.go for the sanitize hook and all
	// three fields' own doc comments.
	InstanceID   string   `json:"instanceId"`
	InstanceName string   `json:"instanceName"`
	KnownDomains []string `json:"knownDomains"`

	// RelayURL is the self-hosted relay this instance dials out to so that
	// it can be reached by siblings on other networks - see
	// settings_relay.go for the field's own doc comment, and especially for
	// why the relay key that goes with it is a credential in
	// internal/accounts rather than a second field here.
	RelayURL string `json:"relayUrl"`
}

// Defaults returns the settings a fresh install starts with.
func Defaults() Settings {
	return Settings{
		MaxConcurrent:    4,
		MaxPerHost:       2,
		SpeedLimit:       0,
		Extract:          true,
		MaxRetries:       3,
		Crawl:            true,
		VerifyChecksums:  true,
		PreParserEnabled: true,
		// AutoConfirm and AddAtTop are usable at their zero value (false):
		// nothing is auto-confirmed and nothing is reordered, which is what
		// every install had before either existed. AutoStart is the one of
		// the three that is NOT its zero value - see its own doc comment on
		// the struct for why "confirmed implies started" has to stay the
		// default rather than silently becoming a fourth thing every
		// existing install's links now do differently.
		AutoStart: true,
		// Never ExcludeAndRemove - see confirm.DefaultPolicy's own comment.
		OnDupes:   string(confirm.DefaultPolicy),
		OnOffline: string(confirm.DefaultPolicy),
		Shape:     ShapeRound,
		// The three archive defaults all say "change nothing you did not ask
		// for": keep the archive, unpack beside it, and write into the folder
		// that is already there rather than starting a second one. The
		// retention is only consulted once somebody switches disposal to trash.
		ArchiveDisposal:    string(extract.DefaultDisposal),
		ExtractCollision:   string(extract.DefaultCollision),
		TrashRetentionDays: extract.DefaultTrashDays,
		// Only three of the new fields have a default worth writing down. The rest
		// are usable at their zero value: no rules, no connections and no timetable
		// all mean "behave exactly as before", which is what a fresh install wants.
		MirrorPolicy:    string(dedupe.DefaultPolicy),
		CollisionPolicy: string(collide.DefaultPolicy),
		Reconnect:       reconnect.Defaults(),
		IdleAction:      idleaction.Defaults(),
		// The zero value already - see Options's own doc comment - written
		// out anyway so every sub-package's Defaults() is called from
		// exactly one place, matching its three neighbours above.
		Ytdlp: ytdlp.Defaults(),
		// Empty, not nil - see this field's own doc comment on YtdlpPresets
		// for why a host with nothing saved here still gets
		// ytdlp.DefaultHosterPreset() rather than no variants at all; that
		// fallback is applied by whoever looks a host up, not baked into
		// every fresh install's own JSON.
		YtdlpPresets: map[string]ytdlp.HosterPreset{},
		// Unlike Ytdlp's, these are not the zero value - see defaultTorrent's
		// own doc comment for where each number actually comes from.
		Torrent: defaultTorrent(),
		// The list is trimmed after a month and the history is not: the two
		// together are the only combination in which "do not let the list grow
		// forever" costs nobody the record of what they downloaded.
		ResumeOnStart:    ResumeNever,
		KeepFinishedDays: DefaultKeepFinishedDays,
		HistoryMax:       DefaultHistoryMax,
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
		} else {
			s.cur = migrate(b, s.cur)
		}
	}
	s.cur = sanitize(s.cur)
	return s, nil
}

// migrate maps keys an older build wrote onto the ones this build reads,
// running each independent sub-migration against the same raw bytes in
// turn - the same "each hook owns its own fields, and none of them has to
// know about the others" shape sanitize below uses, and for the same
// reason: a change that widens one key must not risk the early return
// inside a DIFFERENT key's migration, which is exactly the bug the first
// draft of this split had (migrateAutoStart called only from inside the
// tail of the archive-disposal branch, so it silently never ran for any
// document that had already dropped the ancient deleteArchive key - which
// is to say, for every real install by now).
func migrate(raw []byte, n Settings) Settings {
	n = migrateArchiveDisposal(raw, n)
	n = migrateAutoStart(raw, n)
	return n
}

// migrateArchiveDisposal maps the boolean deleteArchive onto the
// ArchiveDisposal it became. It runs on the raw bytes, once, at load.
//
// The raw bytes are the point. A key that changed TYPE cannot be migrated
// through the struct: leaving the old field on it to read the old value means
// carrying a field the interface then round-trips, so the first save from a
// client that still knows the old name writes it straight back and the two
// disagree forever. Reading it out of the file instead means the old spelling
// is seen exactly once and the next save writes only the new one.
//
// Silence on a parse failure is deliberate: the document already unmarshalled
// into Settings, so a shape this cannot read is a key that has been given some
// third type by hand, and the defaults are a better answer than a guess.
func migrateArchiveDisposal(raw []byte, n Settings) Settings {
	var old struct {
		// deleteArchive: the boolean that became ArchiveDisposal.
		DeleteArchive *bool `json:"deleteArchive"`
		// Read as well, because the new key present in the file is what says
		// this install has already been migrated. Without it a client that
		// keeps sending the old boolean would undo the user's choice on every
		// load.
		ArchiveDisposal *string `json:"archiveDisposal"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return n
	}
	if old.DeleteArchive == nil {
		return n
	}
	if old.ArchiveDisposal != nil && strings.TrimSpace(*old.ArchiveDisposal) != "" {
		return n
	}
	// Only the true half maps onto a new value. False meant "keep", which is
	// also the default, so mapping it is the same as leaving it alone - but it
	// is written out rather than inferred, because an install that deliberately
	// chose "do not delete" should read that way in the file.
	n.ArchiveDisposal = string(extract.DisposalKeep)
	if *old.DeleteArchive {
		n.ArchiveDisposal = string(extract.DisposalDelete)
	}
	return n
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
	return s.setLocked(n)
}

// SetPartial applies patch on top of whatever is CURRENTLY stored and
// persists the result, exactly like Set, except the fields patch does not
// name are read from that current copy under the same lock that then writes
// the result back, never from a copy the caller fetched earlier. Two partial
// saves racing each other therefore compose (a speedLimit patch and a
// concurrent, unrelated maxConcurrent patch both survive) instead of the
// second one's read predating the first one's write and silently reverting
// it. That is the same class of bug `PATCH /api/settings` exists to close,
// guarded one layer further in than the HTTP handler alone could reach: see
// Set's own comment just above for why taking a snapshot outside this lock
// is exactly the mistake that already had to be avoided once, for
// Reconnect's secret merge.
//
// patch's keys are top-level only, exactly as Settings' own JSON encoding
// has them: a key present replaces that whole field. An object field
// replaces the whole sub-document, not a deep per-field merge, so a partial
// Reconnect edit still carries the whole Reconnect object, the same shape
// the Reconnect settings page already saves today, and a key absent leaves
// the stored field untouched. There is deliberately no dotted-path syntax
// for reaching inside a nested field here: 2I's advanced key table
// (routes_features.go, settings_describe.go) already owns that job and its
// own validation pass, and duplicating a second one here is how the two
// quietly disagree about what a clamp allows.
func (s *Store) SetPartial(patch map[string]json.RawMessage) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merged, err := ApplyPatch(s.cur, patch)
	if err != nil {
		return s.cur, err
	}
	return s.setLocked(merged)
}

// setLocked is Set's body, factored out so SetPartial can build its merged
// document from s.cur and persist it inside the one critical section that
// also read s.cur. See SetPartial's own comment for why that is the whole
// point. Callers hold mu.
func (s *Store) setLocked(n Settings) (Settings, error) {
	// The secrets the client was never shown are put back first, and against the
	// value under this very lock. Reading the previous settings through Get would
	// deadlock (mu is a plain Mutex and Get takes it), and taking a snapshot
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

// ApplyPatch overlays patch's top-level keys onto base's own JSON encoding
// and decodes the result back into a Settings, without validating, sanitizing
// or persisting anything.
//
// Exported, and used two ways: SetPartial calls it inside its own lock to
// build what it is about to write, and the PATCH /api/settings handler calls
// it OUTSIDE any lock, against a freshly Get() copy, purely to validate the
// would-be result the same way PUT validates its whole body before ever
// reaching the store. settings.Validate(preview.DownloadDir) and
// validateRows(preview) need a real Settings to inspect, and this is the one
// path that builds one from a patch. That preview can go stale by the
// microseconds between the read and SetPartial's own later, authoritative
// merge under lock; sanitize (inside setLocked) is the same safety net PUT
// already relies on for anything validateRows does not itself cover, so a
// value that changed out from under a stale preview is clamped, never
// corrupted.
//
// Marshal, merge as raw JSON, unmarshal, rather than a hand-written
// field-by-field copy, because Settings already knows how to become and
// come back from exactly this shape, and a second, hand-maintained copy of
// "every field this struct has" is one waves 1-11 have already shown drifts
// (settingsKinds' own doc comment in routes_features.go makes the identical
// argument for reflecting over the struct instead of listing it by hand, in
// the opposite direction of the same document). An unknown key in patch is
// silently dropped by the final Unmarshal, the same as every other decode in
// this codebase (see decodeJSON's own doc comment), not a new inconsistency
// introduced here.
func ApplyPatch(base Settings, patch map[string]json.RawMessage) (Settings, error) {
	baseBytes, err := json.Marshal(base)
	if err != nil {
		return base, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(baseBytes, &merged); err != nil {
		return base, err
	}
	for k, v := range patch {
		merged[k] = v
	}
	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return base, err
	}
	var out Settings
	if err := json.Unmarshal(mergedBytes, &out); err != nil {
		return base, err
	}
	return out, nil
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
	n = sanitizeArchives(n)
	n = sanitizeIntake(n)
	n = sanitizeNetwork(n)
	n = sanitizeResolvers(n)
	n = sanitizeRules(n)
	n = sanitizeLifecycle(n)
	n = sanitizeIdleAction(n)
	n = sanitizeCaptcha(n)
	n = sanitizeConfirm(n)
	n = sanitizeTorrent(n)
	n = sanitizeIdentity(n)
	n = sanitizeRelay(n)
	return n
}
