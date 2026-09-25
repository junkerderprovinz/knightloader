// Package settings persists user-tunable behaviour (concurrency, speed limit,
// extraction) as JSON in the data dir and hands out consistent snapshots.
//
// This file holds the shape and the store: the Settings struct, the defaults and
// Load/Get/Set. What each group of fields may contain lives with that group, in
// settings_queue.go, settings_paths.go, settings_appearance.go and the rest,
// each with its own sanitize hook that sanitize below calls in turn.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
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

	// Quiet is the second set of the two numbers above: what the queue may do
	// while quiet mode is on, switched in with one press or by a timetable
	// window. A zero in there means "leave that one alone" rather than
	// "unlimited", the opposite of what zero means in SpeedLimit one line up.
	// See settings_quiet.go.
	Quiet QuietLimits `json:"quiet"`

	// AutoConfirm moves a batch out of the collector on its own, without a
	// click. AutoConfirmDelay is how long it waits first, in seconds; zero
	// fires the instant a batch is staged. AutoStart is what a confirmed batch
	// does next: start immediately (the default) or sit in the queue until
	// something releases it.
	//
	// The three replace a single AutoStart boolean that conflated them, so they
	// are read together and migrateAutoStart in settings_confirm.go maps every
	// existing install onto them. AutoStart defaults to true because confirming
	// a link always started it before the split.
	AutoConfirm      bool `json:"autoConfirm"`
	AutoConfirmDelay int  `json:"autoConfirmDelay"`
	AutoStart        bool `json:"autoStart"`

	// OnDupes and OnOffline are the confirm-time policies for a link that
	// duplicates one already in the list, or one a check has already found
	// gone (internal/confirm.Policy, stored as its string form like
	// MirrorPolicy and CollisionPolicy). A batch may carry its own override of
	// either, see internal/app.ConfirmTasks; these are the fallback.
	OnDupes   string `json:"onDupes"`
	OnOffline string `json:"onOffline"`
	// AddAtTop puts a batch leaving the collector at the front of the wait
	// order instead of the back.
	AddAtTop bool `json:"addAtTop"`

	// DownloadDir is where finished files land. Empty means the built-in
	// default inside the data directory.
	DownloadDir string `json:"downloadDir"`
	// SubfolderByPackage puts each package in its own folder below DownloadDir.
	SubfolderByPackage bool `json:"subfolderByPackage"`

	// WorkDir is where a download's bytes are written while they are still
	// arriving. The result moves to DownloadDir once nothing is owed on it any
	// more, after the checksum and after the extraction when one is due. Empty
	// writes straight to the destination, which is what an install without this
	// field does; the alternative would copy every download across a filesystem
	// boundary for a problem the owner may not have.
	//
	// It keeps half files out of folders other programs watch: Unraid's mover
	// takes a .part off the cache and copies half a film onto the array, and a
	// library scanner indexes an unfinished mkv once and never looks again. See
	// internal/workdir for the folder and the trip out of it.
	//
	// An absolute path, never a template. See sanitizeStaging.
	WorkDir string `json:"workDir"`
	// ArchivePasswords are tried in order when extracting an encrypted archive.
	ArchivePasswords []string `json:"archivePasswords"`

	// ExtractTo collects extractions in one folder instead of leaving each one
	// beside its archive. Empty leaves them beside the archive. It may be a
	// pathvars template, expanded per task like DownloadDir.
	ExtractTo string `json:"extractTo"`
	// ExtractSubfolder puts each package in its own folder below ExtractTo. It
	// does nothing without ExtractTo, see extract.Options.
	ExtractSubfolder bool `json:"extractSubfolder"`
	// ExtractMoveTo is where the content of a finished extraction is moved once
	// it has unpacked. Empty leaves it where it unpacked. It may be a pathvars
	// template, expanded per task exactly as DownloadDir and ExtractTo are.
	//
	// It is not a second spelling of ExtractTo. That one is where the unpacking
	// writes, so the destination holds a growing, half-finished folder for as
	// long as the extraction runs. This moves the finished files afterwards,
	// and it moves the content rather than the folder: a release that unpacked
	// as "Show.S01.COMPLETE.WEB/ep01.mkv" lands as "ep01.mkv".
	//
	// The per-link answer is a Packagizer rule (rules.Action.ExtractDir), read
	// in front of this the way Task.AutoExtract is read in front of Extract.
	// See app.extractWanted.
	ExtractMoveTo string `json:"extractMoveTo"`
	// ExtractCollision is what an extraction does when its destination folder is
	// already there: rename, skip or overwrite, decided per folder.
	ExtractCollision string `json:"extractCollision"`
	// ArchiveDisposal is what happens to an archive that unpacked cleanly:
	// keep, trash or delete. It replaced the boolean deleteArchive, which
	// migrate() below reads once at load and never writes again.
	ArchiveDisposal string `json:"archiveDisposal"`
	// TrashRetentionDays is how long a trashed archive stays before the sweep
	// takes it. Zero never sweeps.
	TrashRetentionDays int `json:"trashRetentionDays"`
	// DeleteInfoFiles sweeps the .nfo/.sfv/.diz/.url that came with the same
	// package as the archive, using the same disposal.
	DeleteInfoFiles bool `json:"deleteInfoFiles"`
	// MaxRetries is how often a failed download is retried automatically. A
	// RetryRule with no Tries of its own falls back to this count.
	MaxRetries int `json:"maxRetries"`
	// Retry is the backoff in front of those attempts, per failure reason and
	// per host, instead of one doubling sequence for every hoster on the
	// internet. Empty is the built-in fifteen-seconds-to-ten-minutes backoff.
	// See settings_hostrules.go.
	Retry RetryPolicy `json:"retry"`

	// StallTimeout is how long a running download may move no bytes before it
	// is marked as standing still, in seconds. Zero never marks anything.
	//
	// A dead connection is indistinguishable from a slow one on a list: the row
	// says "running", the speed says 0 B/s, and the slot it holds is gone until
	// somebody notices. The mark stops nothing on its own; StallRestart is the
	// half that acts on it.
	//
	// Clamped up to MinStallTimeout when set at all, see settings_stall.go.
	StallTimeout int `json:"stallTimeout"`
	// StallRestart hands a marked download back to the wait queue and starts it
	// again from the top. Off by default, and a switch of its own rather than
	// part of StallTimeout, because a restart throws away the bytes the stalled
	// attempt did fetch: app.restartStalled goes down the same path
	// RestartTasks does, which clears the backend's partial file.
	StallRestart bool `json:"stallRestart"`
	// StallMaxRestarts caps how many of those one task gets. Zero means
	// DefaultStallRestarts rather than unlimited.
	StallMaxRestarts int `json:"stallMaxRestarts"`

	// DiskReserve, DiskLowSpace and DiskCriticalSpace are the destination
	// volume's three numbers, all in bytes. See settings_diskspace.go for the
	// defaults, the clamps and the invariant between them.
	//
	// Without them free space is only ever read from a write that has already
	// failed (core.ReasonDiskFull), by which time the bytes are spent and every
	// other transfer aimed at the same volume is still running.
	//
	// DiskReserve is headroom kept free beyond what a download still needs,
	// checked against that download's own remaining bytes. The only one of the
	// three that is on by default, because it refuses exactly the downloads
	// that provably would not have fitted.
	DiskReserve int64 `json:"diskReserve"`
	// DiskLowSpace is the floor under which no new download starts, whatever
	// its size and whether its size is known at all. Zero is off, because a
	// number of bytes means nothing without knowing the volume: a gigabyte is
	// nothing on a sixteen-terabyte array and a third of a memory card.
	DiskLowSpace int64 `json:"diskLowSpace"`
	// DiskCriticalSpace is the floor under which everything already running is
	// stopped and put back in the wait queue. Zero is off.
	//
	// A second threshold rather than a second reading of the first, because
	// declining to start costs a download its place in the queue for a while
	// while stopping one throws away whatever a non-resumable transfer had
	// fetched. The queue may keep filling a disk down to the low mark; only a
	// volume about to run out gets the transfers taken off it.
	DiskCriticalSpace int64 `json:"diskCriticalSpace"`

	// VolumeCap, VolumeCapResetDay, VolumeCapAction and VolumeCapThrottle are
	// the allowance: how much may finish downloading in one period, and what
	// happens once that much has. See settings_volume.go for the defaults and
	// the clamps, internal/app/app_volumecap.go for the arithmetic.
	//
	// They are the disk guard's opposite number. Free space is a fact about
	// this machine that anybody can measure; a volume allowance is a number in
	// somebody's contract that nothing on this box can see, so the whole of it
	// is typed in here.
	//
	// VolumeCap is bytes, and 0 is no cap rather than an unset field: the
	// counter keeps running, the chart keeps drawing, nothing is held back.
	VolumeCap int64 `json:"volumeCap"`
	// VolumeCapResetDay is the day of the month the counter goes back to zero,
	// 1..31, usually the day an allowance renews. In a month shorter than the
	// chosen day the period restarts on that month's last day and never slips
	// into the next one.
	VolumeCapResetDay int `json:"volumeCapResetDay"`
	// VolumeCapAction is what reaching the cap does: "report" (the default,
	// which only counts), "pause" (nothing new starts until the counter
	// restarts) or "throttle" (everything keeps going at VolumeCapThrottle).
	// None of the three is read while VolumeCap is 0.
	VolumeCapAction string `json:"volumeCapAction"`
	// VolumeCapThrottle is bytes per second while capped, and means nothing
	// unless the action is "throttle". It is a ceiling beside the other limits
	// and never instead of them: a schedule window or quiet mode asking for
	// less still wins, because all of them meet in app_budget.go.
	VolumeCapThrottle int64 `json:"volumeCapThrottle"`

	// Crawl lets a pasted page URL be opened and the files it links to be
	// staged, instead of the page itself becoming one task.
	Crawl bool `json:"crawl"`
	// CrawlDepth is how many pages deep that crawl goes: 1 is the pasted page
	// alone, 2 also follows the pages it links to, 3 follows theirs. It
	// defaults to 1 because a deep crawl is dozens of requests to a stranger's
	// server, which only the person who typed the number may ask for.
	//
	// Clamped to internal/crawler.MaxDepth, see sanitizeIntake.
	CrawlDepth int `json:"crawlDepth"`
	// CrawlMaxPages caps how many pages one crawl fetches. It counts requests
	// rather than links found, see crawler.Options.MaxPages. It does nothing at
	// depth 1, where there is exactly one page.
	CrawlMaxPages int `json:"crawlMaxPages"`
	// CrawlSameHost keeps a deep crawl on the pasted page's own host. Host
	// exactly, subdomains excluded, and it never restricts the files that come
	// back; crawler.Options.SameHost carries the reasoning for both halves.
	// True by default, since at depth 1 it does nothing at all and the first
	// person to raise the depth then gets the safe answer already.
	CrawlSameHost bool `json:"crawlSameHost"`
	// CrawlInclude and CrawlExclude are regular expressions matched against the
	// URLs a crawl meets. Empty lists, the default, mean no filtering. Exclude
	// keeps the crawl away from pages as well as files; include only narrows
	// what is staged, see crawler.Options for why they are not symmetric.
	//
	// No omitempty: a nil slice with omitempty is dropped from the JSON
	// entirely, and the frontend has no way to type a field that is sometimes
	// absent. Without it a nil slice encodes as null, so the key is always
	// there. The same holds for Feeds, Categories, MediaHooks, RainbowPalette,
	// CaptchaSolverOrder, ResolverOrder and YtdlpPresets below.
	CrawlInclude []string `json:"crawlInclude"`
	CrawlExclude []string `json:"crawlExclude"`
	// WatchDir is a folder whose dropped .txt/.crawljob files are picked up.
	// Empty disables the watcher.
	WatchDir string `json:"watchDir"`
	// Feeds are the RSS and Atom subscriptions this instance follows: an
	// address, how often to look at it, and optionally a title pattern, a
	// destination folder and a priority for what it finds. An empty list is the
	// off state. A feed is an intake, the sibling of WatchDir above; what one
	// may contain is settings_feeds.go's business.
	//
	// No omitempty, see CrawlInclude.
	Feeds []feed.Subscription `json:"feeds"`
	// EventTargets are the addresses this instance reports to when one of the
	// events internal/script publishes happens: a URL, a method, headers and a
	// body template the operator wrote, per row. See settings_notify.go.
	//
	// omitempty here, unlike Feeds: an absent key has to keep decoding to nil
	// so that a settings.json written before this field existed reads back as
	// "nothing sends". The frontend types it `EventTargetRow[] | null |
	// undefined` for the same reason.
	//
	// Every header value in here is a secret. See Redacted in
	// settings_network.go, and notify.Merge for why the carry-back on save is
	// bound to the address and not only to the row id.
	EventTargets []notify.Target `json:"eventTargets,omitempty"`
	// VerifyChecksums checks a finished download against a checksum file that
	// came with it, when one did.
	VerifyChecksums bool `json:"verifyChecksums"`
	// PreParserEnabled turns on internal/linkscan for POST /api/links: the
	// pasted or dropped blob is scanned for links wherever they sit in it,
	// instead of one line being taken as one link verbatim. Named and
	// defaulted after JDownloader's AddLinksPreParserEnabled
	// (LinkgrabberSettings.java): same key, same true default, same meaning
	// for off.
	PreParserEnabled bool `json:"preParserEnabled"`

	// DownloadClientAPI opens the SABnzbd-shaped door Sonarr and Radarr can be
	// pointed at. See internal/api/routes_downloadclient.go for what it speaks.
	//
	// Off by default because it lets a program on the network create downloads
	// and delete finished files, the way Reconnect below is off because it runs
	// a program on the router. Switching it on is not enough on its own: the
	// route refuses every request without a valid API token
	// (internal/apitoken), even on an instance with no password set.
	DownloadClientAPI bool `json:"downloadClientApi"`

	// Metrics opens GET /api/metrics, which answers the health readout as
	// Prometheus exposition text for a monitoring system to fetch. The address
	// is only ever read from.
	//
	// Off by default. The route is session-guarded like everything under /api/,
	// but it carries the target folders' paths as label values. While this is
	// false the route answers 404, so the address does not exist until somebody
	// opens it. No entry in Defaults() and nothing to sanitise: false is the
	// default, and a bool has no wrong value.
	Metrics bool `json:"metrics"`

	// Shape is what shape the interface's corners take: "round", "soft",
	// "square", or "leaf", which no picker offers until it is found. One knob
	// drives every corner, so the app never looks half-converted.
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

	// HideAccountsFromSidebar removes the sidebar's "Konten" nav item, for
	// someone who only ever reaches accounts through the identical settings
	// tab. Both render the same page, so hiding one costs nothing but a click.
	HideAccountsFromSidebar bool `json:"hideAccountsFromSidebar"`

	// HideInstancesFromSidebar does the same for the "Instanzen" item:
	// somebody running a single instance has a nav item that lists exactly
	// itself. A separate field rather than a shared list of hidden nav items,
	// because a set of strings in settings.json is a set somebody can put a
	// typo in, and two entries are not a family worth that.
	HideInstancesFromSidebar bool `json:"hideInstancesFromSidebar"`

	// NavLabels is how much of a navigation entry is drawn: "both", "glyph",
	// "text" or "hover". It governs the sidebar and the settings rail together,
	// from one control. Stored with the instance rather than in the browser,
	// alongside Shape and Accent, so the look follows the instance from one
	// machine to the next. See settings_appearance.go for what "hover" does,
	// which is not what the word suggests.
	NavLabels string `json:"navLabels"`

	// BottomBarLabels is how much of an entry the bar along the bottom of the
	// phone layout draws: "follow", the default, draws it the way NavLabels
	// says, and NavLabels' own four values set it apart from the sidebar. It
	// changes nothing on a wider screen, where there is no bar.
	BottomBarLabels string `json:"bottomBarLabels"`

	// AutoUpdateCheck asks the desktop build to call update.Check once at
	// startup, and the Allgemein tab to do the same on load, instead of only on
	// a click of "Check for updates". The container build reads it nowhere. Off
	// by default: it is an outbound call to GitHub on every launch.
	AutoUpdateCheck bool `json:"autoUpdateCheck"`

	// AutoUpdateInstall asks the desktop build to install a newer release
	// (download, verify, swap the running binary, relaunch) the moment
	// AutoUpdateCheck finds one. It means nothing without AutoUpdateCheck, and
	// nothing on the container build, which cannot replace itself from the
	// inside: App.RequestUpdateInstall is nil there and the route refuses
	// before this field is read. Off by default, since opting into a version
	// check does not imply opting into replacing the running binary.
	AutoUpdateInstall bool `json:"autoUpdateInstall"`

	// YtdlpVersionCheck asks the Resolvers page to call GET
	// /api/mediatools/ytdlp/latest once when it loads, instead of only when
	// somebody presses "Ask GitHub". Off by default, because it is an outbound
	// call to api.github.com. It never downloads and never replaces anything,
	// and there is no companion switch that installs what it finds: yt-dlp does
	// ship regressions, and a new one silently changes what every download
	// produces.
	YtdlpVersionCheck bool `json:"ytdlpVersionCheck"`

	// Packagizer names packages, picks folders and sets download options as
	// links are staged. Stored exactly as the user wrote it: rules.Compile is
	// the validator, and a rule with a broken regular expression has to
	// round-trip to disk so it can be fixed in the form instead of disappearing
	// on save.
	Packagizer rules.Set `json:"packagizer"`
	// LinkFilter decides which links are taken into the collector at all.
	// StopAfterMatch usually wants to be on here, so a narrow accept placed
	// above a broad reject protects the link; nothing forces it.
	LinkFilter rules.Set `json:"linkFilter"`

	// MirrorPolicy is when two different URLs count as the same file.
	MirrorPolicy string `json:"mirrorPolicy"`
	// KeepMirrors keeps the second copy instead of dropping it: the link is
	// staged as a sibling of the download it mirrors, parked, and labelled with
	// the task it is a copy of. Off by default, because it costs a parked row
	// per mirror in a list people already find long; what it buys is that the
	// alternative link survives a restart, which an in-memory trace does not.
	// On its own it starts nothing.
	KeepMirrors bool `json:"keepMirrors"`
	// MirrorFailover releases that parked sibling once the download it is a
	// copy of has finished failing, and hands it the dead task's folder,
	// package and priority. See app.handOverToMirrorLocked for when "finished
	// failing" is and where the chain of copies ends.
	//
	// Off by default, and a switch of its own rather than part of KeepMirrors:
	// keeping a mirror costs a row in a list, while switching to one starts a
	// transfer from a hoster the user did not pick, possibly a re-encode rather
	// than the release they were after, with nothing here able to ask first.
	//
	// It does nothing without KeepMirrors: with mirrors dropped there is never
	// a parked sibling to release.
	MirrorFailover bool `json:"mirrorFailover"`

	// CollisionPolicy is what happens when the destination file already exists.
	CollisionPolicy string `json:"collisionPolicy"`
	// CollisionMaxAttempts caps how many counted names a rename tries. Zero means
	// the package's own cap.
	CollisionMaxAttempts int `json:"collisionMaxAttempts,omitempty"`

	// Connections is the user-ordered list of outbound connections downloads are
	// spread across. Empty means everything goes out over the machine's own
	// connection, which is what an install that never opened the page has.
	Connections []proxycfg.Entry `json:"connections,omitempty"`

	// Chunks is how many connections one download opens when neither the task
	// nor a rule has named a number. It is not about the list above:
	// Connections is which way out of the machine the bytes go, this is how
	// many sockets one file is pulled over. Zero is "no opinion", as on the
	// task, and the dispatcher owns the fallback.
	Chunks int `json:"chunks"`

	// HostRules is what one host may differ in: its own simultaneous-download
	// ceiling, its own chunk count, its own retry backoff. Keyed by host
	// pattern, see HostRuleFor for what matches.
	//
	// MaxPerHost and Chunks are each one number for every hoster on the
	// internet, and hosters do not agree: one tolerates eight connections, the
	// next blocks from two, so tuning the global pair for the strictest host
	// throttles every other download on the box. A host with no entry gets the
	// global values, which makes an empty table a no-op.
	HostRules map[string]HostRule `json:"hostRules"`

	// Categories are the named drawers a link can be filed in, each carrying a
	// folder, a priority, an unpacking switch, a speed limit and a collision
	// rule. Referred to by Task.Category, offered when links are thrown in,
	// filterable as a facet, settable by a Packagizer rule. See
	// settings_categories.go for which of three folders wins, what a change or
	// a delete does to downloads already filed, and why the field hangs off the
	// task rather than off the package.
	//
	// A slice and not a map keyed by id, which is where this differs from
	// HostRules above: a host rule is looked up and never listed, a category is
	// a menu, and Go's map iteration would reshuffle that menu on every process
	// start. The order here is the order it is offered in.
	//
	// Empty, the default, is the feature switched off: nothing is filed
	// anywhere and every task takes the global answers. No omitempty, see
	// CrawlInclude.
	Categories []Category `json:"categories"`

	// MediaHooks are the stored addresses a category drawer calls once a
	// package filed in it has finished and its files have been moved into
	// place, a media library told to rescan in practice. See
	// settings_mediahooks.go for the shape and Category.Notify for the
	// reference into this table.
	//
	// The one header value such an address may carry is not a field on the row:
	// this struct is what routes_diagnostics.go serialises into the bundle
	// people attach to public bug reports, and what routes_features.go reflects
	// over for the Advanced key table. The value is sealed in accounts.Store
	// instead, see internal/mediahook's package comment.
	//
	// Empty, the default, is the feature switched off. No omitempty, see
	// CrawlInclude.
	MediaHooks []mediahook.Hook `json:"mediaHooks"`

	// Reconnect gets the box a new public address when a hoster's free-user limit
	// is keyed to the one it has. Off by default: it runs a program or talks to
	// the router, and neither should ever happen because a default said so.
	Reconnect reconnect.Config `json:"reconnect"`

	// Schedule is the timetable that pauses or throttles the queue by the clock.
	// An empty timetable changes nothing. It stays a field of this struct
	// rather than a file of its own beside settings.json; see the doc comment
	// on PUT /api/schedule in routes_schedule.go, and setFeature's "scheduler"
	// case in routes_features.go for the read-current/write-one-field shape
	// every writer of this field is expected to use.
	Schedule []schedule.Entry `json:"schedule,omitempty"`

	// IdleAction is what happens once the wait queue has nothing enabled left
	// to run, start or finish, after a cancellable countdown. See
	// internal/idleaction. The zero value is Action=ActionNone, so a fresh
	// install and an upgrade that has never seen this key have nothing armed.
	IdleAction idleaction.Config `json:"idleAction"`

	// ResumeOnStart is what happens to the downloads that were in flight when
	// the process last stopped: never, only what was running, or everything
	// unfinished. See the constants for what each one costs.
	ResumeOnStart string `json:"resumeOnStart"`
	// ReclaimTrust is how much the "already on the disk" pass may believe about
	// a file it did not watch arrive: only a verified checksum, or also this
	// instance's own record of what it finished, or also bare name plus length.
	// See internal/reclaim's Trust doc comment for what each tier costs, and
	// for why size on its own is not the default on a build whose download
	// library creates the destination file at its full final length before the
	// first byte arrives.
	//
	// A policy and not a switch: there is no "run this at boot" field beside
	// it. The pass stats every unfinished task's folder and, wherever a
	// checksum exists, reads a file that may be tens of gigabytes. Moving a box
	// or restoring a backup is a one-off event and gets a one-off press, see
	// App.Reclaim.
	ReclaimTrust string `json:"reclaimTrust"`

	// KeepFinishedDays is how long a finished download stays in the list. Zero
	// keeps it forever. It never touches the file: removing a row and deleting
	// what was downloaded are two different actions here, and what was fetched
	// stays in the history table, which retention does not read.
	KeepFinishedDays int `json:"keepFinishedDays"`
	// HistoryMax caps the download history. Zero keeps every entry, which is a
	// table that only grows on an instance that is never restarted.
	HistoryMax int `json:"historyMax"`

	// MaintenanceIntervalDays is how often the database looks after itself
	// without being asked. Zero, the shipped value, means only when somebody
	// presses the button on the diagnostics page.
	//
	// Zero is the default because the work behind this holds every write in the
	// process for as long as it runs (internal/store/maintenance.go), and an
	// update should not start doing that at four in the morning. Switching it
	// on runs nothing immediately either: the first run is a whole interval
	// away, and the button is there for now.
	MaintenanceIntervalDays int `json:"maintenanceIntervalDays"`
	// MaintenanceCompactOnSchedule decides whether the scheduled run also
	// rewrites the file, or only reads it and reports. False, because
	// compacting needs room for a full second copy of the database on the
	// temporary volume, which on a container is not the data volume and is
	// often not large. The manual Compact button ignores this field.
	MaintenanceCompactOnSchedule bool `json:"maintenanceCompactOnSchedule"`

	// LogFile is the optional copy of this process's own log output on disk:
	// whether it is written at all, how big one file may get, and how many
	// renamed ones stay beside it. Off on every install that upgrades into this
	// key, since Load unmarshals over Defaults. See settings_logfile.go for why
	// there is no path field.
	LogFile LogFile `json:"logFile"`

	// CaptchaSolverOrder is which automatic captcha-solving services
	// (internal/accounts.Catalogue ids "2captcha"/"anticaptcha") to try, and in
	// what order, before a captcha is shown to a human. Membership and order
	// live in the one list, the way the accounts page's resolver priority does:
	// an id absent from it is not tried, rather than a separate bool per
	// service that could disagree with where the service sits in the order.
	// Empty is what a fresh install has, straight to the prompt modal.
	//
	// An id here with no matching credential is skipped when tried, and neither
	// half implies the other, see sanitizeCaptcha. This is the non-secret half:
	// the API key is a credential (internal/accounts), never a settings field.
	//
	// No omitempty, see CrawlInclude. The frontend types it `string[] | null`.
	CaptchaSolverOrder []string `json:"captchaSolverOrder"`

	// ResolverOrder is the hand-arranged order the download services are asked
	// in, most-preferred first, by resolver id ("torbox", "alldebrid", "jd",
	// "ytdlp", "direct", ...). Empty, the default, means the automatic order.
	//
	// An entry names a service and moves every account configured for it: a
	// service with two stored keys is two entries in the routing table
	// (resolver.SlotID) and one row on the card, and dispatch matches this list
	// against the service half of a resolver id, see app.dynamicPrio. A full
	// slot id ("alldebrid#work") written in here is honoured as written.
	//
	// Unlike CaptchaSolverOrder there is no id whitelist. The set of resolvers
	// is not fixed at compile time: it grows with every debrid service in the
	// catalogue, and a service is registered only once a key for it is stored.
	// A whitelist here would be a second copy of the catalogue or a dependency
	// on the resolver registry, and would turn "you removed the key for a
	// service you had ordered" into a silent rewrite of that order. An id
	// naming nothing is inert, since dispatch only ranks resolvers that exist.
	//
	// No omitempty, see CrawlInclude.
	ResolverOrder []string `json:"resolverOrder"`

	// Ytdlp is the yt-dlp backend's own configuration: format and quality
	// selection, subtitles and their language, the output filename template,
	// whether a playlist URL fetches one video or the whole list, and the
	// library-facing extras, all off by default (metadata, cover art, chapters
	// and subtitles embedded into the container, a Kodi NFO beside it, music
	// tagging for the audio row, an ffprobe pass over the finished file, the
	// caps on a livestream recording). See the doc comment on
	// internal/resolver/ytdlp.Options for why every field's zero value
	// reproduces the backend's behaviour from before any of them existed.
	//
	// The per-site cookies.txt that answers "Sign in to confirm you are not a
	// bot" is not here. A jar is a live session, and this struct is marshalled
	// into settings.json, returned by GET /api/settings and serialised whole
	// into the diagnostics bundle people attach to public bug reports, so the
	// jar lives in the encrypted credential store under ytdlp.CookieService the
	// way native hoster logins do (internal/hosterauth). The only field here is
	// the switch that says whether a stored jar may be used.
	Ytdlp ytdlp.Options `json:"ytdlp"`

	// YtdlpPresets is per-host (say "youtube.com") config for the "Variante"
	// rows a yt-dlp link stages, see ytdlp.HosterPreset: which of video, audio,
	// thumbnail, subtitle and description land in the collector by default for
	// links from that host, and the default quality and audio format for the
	// two variants that have one. A host with no entry gets
	// ytdlp.DefaultHosterPreset(). Map and no omitempty, see CrawlInclude.
	YtdlpPresets map[string]ytdlp.HosterPreset `json:"ytdlpPresets"`

	// Torrent is the seed, port, DHT and PEX policy for the BitTorrent backend.
	// See settings_torrent.go for the full shape and for what of it the gopeed
	// dependency this build embeds actually enforces.
	Torrent Torrent `json:"torrent"`

	// InstanceID, InstanceName and KnownDomains are this instance's own
	// identity. See settings_identity.go for the sanitize hook and the three
	// fields' own doc comments.
	InstanceID   string   `json:"instanceId"`
	InstanceName string   `json:"instanceName"`
	KnownDomains []string `json:"knownDomains"`

	// RelayURL is the self-hosted relay this instance dials out to so that it
	// can be reached by siblings on other networks. See settings_relay.go, and
	// for why the relay key that goes with it is a credential in
	// internal/accounts rather than a second field here.
	RelayURL string `json:"relayUrl"`

	// RelayServe makes this instance run the relay itself, on its own address
	// under /relay/connect, for instances carrying the same relay key. See
	// settings_relay.go for what that does and does not buy.
	RelayServe bool `json:"relayServe"`

	// RelayMode is which relay this instance uses: RelayModeProject,
	// RelayModeOwn or RelayModeOff.
	//
	// The answer used to be inferred from RelayURL, empty meaning the project's
	// relay and set meaning your own, which left no room for the third one:
	// instances that all sit on the same network need no relay and should not
	// be dialling one. An empty value is not a fourth state but an install from
	// before the field existed, and RelayModeOf below reads it the way that
	// install behaved.
	RelayMode string `json:"relayMode"`

	// ModulesOff names the modules switched off on the modules page, by the ids
	// internal/api/routes_features.go gives them. It lists what is off rather
	// than what is on, so an upgrade that adds a module leaves it running.
	//
	// No omitempty, see CrawlInclude.
	ModulesOff []string `json:"modulesOff"`
}

// ModuleOff reports whether the module with this id is switched off.
func (s Settings) ModuleOff(id string) bool {
	return slices.Contains(s.ModulesOff, id)
}

// The three answers to "which relay does this instance use".
const (
	// RelayModeProject: the address compiled into the binary
	// (relay.DefaultRelayURL), run by the project.
	RelayModeProject = "project"
	// RelayModeOwn: an address the user gave, or this instance itself when
	// RelayServe is on.
	RelayModeOwn = "own"
	// RelayModeOff: no relay at all. Instances find each other only on the
	// local network or through an address entered by hand.
	RelayModeOff = "off"
)

// RelayModeOf answers which relay these settings mean, including for installs
// that predate the field.
//
// The migration is a read rather than a write: nothing rewrites settings.json
// on upgrade, so an install that never touches the page keeps behaving as it
// did and downgrading to an older build still works. The old inference was
// "RelayURL set means your own", which is what an empty RelayMode means here.
func (s Settings) RelayModeOf() string {
	switch s.RelayMode {
	case RelayModeProject, RelayModeOwn, RelayModeOff:
		return s.RelayMode
	}
	if strings.TrimSpace(s.RelayURL) != "" {
		return RelayModeOwn
	}
	return RelayModeProject
}

// Defaults returns the settings a fresh install starts with.
func Defaults() Settings {
	return Settings{
		MaxConcurrent: 4,
		MaxPerHost:    2,
		SpeedLimit:    0,
		Extract:       true,
		MaxRetries:    3,
		Crawl:         true,
		// One page deep and staying on the host it was pasted from.
		CrawlDepth:       1,
		CrawlMaxPages:    crawler.DefaultMaxPages,
		CrawlSameHost:    true,
		VerifyChecksums:  true,
		PreParserEnabled: true,
		// AutoConfirm and AddAtTop are usable at their zero value: nothing is
		// auto-confirmed and nothing is reordered. AutoStart is the one of the
		// three that is not, so that confirming a link still starts it.
		AutoStart: true,
		// Never ExcludeAndRemove, see confirm.DefaultPolicy.
		OnDupes:         string(confirm.DefaultPolicy),
		OnOffline:       string(confirm.DefaultPolicy),
		Shape:           DefaultShape,
		NavLabels:       NavLabelsBoth,
		BottomBarLabels: BottomBarFollowsNav,
		// Keep the archive, unpack beside it, and write into the folder that is
		// already there. The retention is only consulted once somebody switches
		// disposal to trash.
		ArchiveDisposal:    string(extract.DefaultDisposal),
		ExtractCollision:   string(extract.DefaultCollision),
		TrashRetentionDays: extract.DefaultTrashDays,
		MirrorPolicy:       string(dedupe.DefaultPolicy),
		CollisionPolicy:    string(collide.DefaultPolicy),
		Reconnect:          reconnect.Defaults(),
		IdleAction:         idleaction.Defaults(),
		// The zero value already, written out so every sub-package's Defaults()
		// is called from one place.
		Ytdlp: ytdlp.Defaults(),
		// Empty rather than nil: a host with nothing saved gets
		// ytdlp.DefaultHosterPreset() from whoever looks it up, not from a copy
		// baked into every fresh install's JSON.
		YtdlpPresets: map[string]ytdlp.HosterPreset{},
		// Both empty tables are load-bearing: an empty host table means every
		// host keeps the global numbers, an empty reason table means every
		// failure keeps the one backoff.
		HostRules: map[string]HostRule{},
		Retry:     RetryPolicy{ByReason: map[string]RetryRule{}},
		Torrent:   defaultTorrent(),
		// The list is trimmed after a month and the history is not, so keeping
		// the list from growing forever costs nobody the record of what they
		// downloaded.
		ResumeOnStart:    ResumeNever,
		KeepFinishedDays: DefaultKeepFinishedDays,
		HistoryMax:       DefaultHistoryMax,
		// The next five are written out at values that are partly their zero
		// value because the Advanced key table serves Defaults() unsanitised: a
		// factory reading of "0 / off" that appears only because nobody typed
		// anything is indistinguishable from a field somebody forgot.
		MaintenanceIntervalDays:      DefaultMaintenanceIntervalDays,
		MaintenanceCompactOnSchedule: false,
		LogFile:                      DefaultLogFile(),
		ReclaimTrust:                 string(reclaim.DefaultTrust),
		// Quiet mode ships with a slot count and no speed. The box has no idea
		// how fast the line is, so an invented number would be no limit at all
		// on a gigabit connection or a stall on a slow one. The slot count has
		// to be there, or the first press of the button does nothing.
		Quiet: QuietLimits{MaxConcurrent: 1},
		// One of the volume's three numbers is on by default, see
		// DefaultDiskReserve.
		DiskReserve: DefaultDiskReserve,
		// The allowance ships switched off: a cap of 0 is what makes it off, so
		// neither of these does anything until somebody types a number.
		VolumeCapResetDay: DefaultVolumeCapResetDay,
		VolumeCapAction:   VolumeCapReport,
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

// Path is the settings file this store reads and writes, verbatim.
//
// The file may well not exist: Load reads it and never writes it, so a fresh
// install runs on the built-in defaults until somebody saves a settings page. A
// caller measuring it has to treat "not there" as an answer rather than as an
// error, see StorageInfo.SettingsPresent.
func (s *Store) Path() string { return s.path }

// migrate maps keys an older build wrote onto the ones this build reads,
// running each sub-migration against the same raw bytes in turn. They are
// independent for the reason sanitize's hooks are: a change that widens one key
// must not depend on an early return inside another key's migration.
func migrate(raw []byte, n Settings) Settings {
	n = migrateArchiveDisposal(raw, n)
	n = migrateAutoStart(raw, n)
	return n
}

// migrateArchiveDisposal maps the boolean deleteArchive onto the
// ArchiveDisposal it became. It runs on the raw bytes, once, at load.
//
// A key that changed type cannot be migrated through the struct: leaving the
// old field on it means carrying a field the interface then round-trips, so the
// first save from a client that still knows the old name writes it straight
// back and the two disagree forever. Read from the file instead, the old
// spelling is seen once and the next save writes only the new one.
//
// A parse failure is silent: the document already unmarshalled into Settings,
// so a shape this cannot read is a key given some third type by hand, and the
// defaults beat a guess.
func migrateArchiveDisposal(raw []byte, n Settings) Settings {
	var old struct {
		DeleteArchive *bool `json:"deleteArchive"`
		// The new key being present is what says this install has already been
		// migrated. Without reading it, a client that keeps sending the old
		// boolean would undo the user's choice on every load.
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
	// False meant "keep", which is also the default, so it is written out
	// rather than inferred: an install that chose "do not delete" should read
	// that way in the file.
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

// SetPartial applies patch on top of what is currently stored and persists the
// result, exactly like Set, except that the fields patch does not name are read
// from the stored copy under the same lock that then writes the result back,
// never from a copy the caller fetched earlier. Two partial saves racing each
// other therefore compose: a speedLimit patch and an unrelated maxConcurrent
// patch both survive, instead of the second one's read predating the first
// one's write and reverting it.
//
// patch's keys are top-level only, as Settings' own JSON encoding has them: a
// key present replaces that whole field, an object field replaces the whole
// sub-document rather than merging per field, and a key absent leaves the
// stored field untouched. There is no dotted-path syntax for reaching inside a
// nested field: the advanced key table (routes_features.go,
// settings_describe.go) owns that job and its own validation pass, and a second
// one here is how the two come to disagree about what a clamp allows.
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
// document from s.cur and persist it inside the one critical section that also
// read s.cur. Callers hold mu.
func (s *Store) setLocked(n Settings) (Settings, error) {
	// The secrets the client was never shown are put back first, against the
	// value under this very lock. Reading the previous settings through Get
	// would deadlock, and a snapshot taken before the lock would let two
	// concurrent saves merge against the same stale value, so the second writes
	// back a router password the first had already changed.
	n.Reconnect = n.Reconnect.WithSecretsFrom(s.cur.Reconnect)
	// Bound to the address and not only to the row id: a header value the
	// client was shown as eight stars comes back only while the row still
	// points at the host it was stored for, so a client that was never allowed
	// to read the token cannot have this server post it somewhere else. It runs
	// before sanitize(n) below, because Merge matches on the ids the previous
	// Sanitize handed out.
	n.EventTargets = notify.Merge(n.EventTargets, s.cur.EventTargets)
	n.Connections = proxycfg.Merge(n.Connections, s.cur.Connections)
	// The end-of-queue command line is the third thing a client is never shown
	// (see Settings.Redacted and idleaction.CommandSpec.Redacted), so it needs
	// the same merge back or every save from the Downloads settings page would
	// wipe the stored command with the placeholder it was sent.
	n.IdleAction = n.IdleAction.WithSecretsFrom(s.cur.IdleAction)
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
// It is used two ways: SetPartial calls it inside its own lock to build what it
// is about to write, and the PATCH /api/settings handler calls it outside any
// lock, against a freshly Get() copy, to validate the would-be result the way
// PUT validates its whole body. settings.Validate(preview.DownloadDir) and
// validateRows(preview) need a real Settings to inspect, and this is the one
// path that builds one from a patch. That preview can go stale between the read
// and SetPartial's own merge under lock, which sanitize inside setLocked
// catches, so a value that changed underneath is clamped rather than corrupted.
//
// Marshal, merge as raw JSON, unmarshal, rather than a hand-written
// field-by-field copy: Settings already knows how to become and come back from
// this shape, and a second hand-maintained list of every field it has drifts.
// An unknown key in patch is dropped by the final Unmarshal, as in every other
// decode here.
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

// sanitize is the one path everything written to disk goes down. It does
// nothing itself: each group of fields is cleaned by the file that owns it, so
// a new setting lands in one domain file and one line of this list.
//
// The hooks are independent, no hook reading a field another one rewrites, so
// the order below is for reading rather than for correctness.
func sanitize(n Settings) Settings {
	n = sanitizeAppearance(n)
	n = sanitizeQueue(n)
	n = sanitizeQuiet(n)
	n = sanitizeStall(n)
	n = sanitizeDiskSpace(n)
	n = sanitizeVolume(n)
	n = sanitizeHostRules(n)
	n = sanitizeCategories(n)
	n = sanitizeMediaHooks(n)
	n = sanitizePaths(n)
	n = sanitizeStaging(n)
	n = sanitizeArchives(n)
	n = sanitizeIntake(n)
	n = sanitizeFeeds(n)
	n = sanitizeEventTargets(n)
	n = sanitizeNetwork(n)
	n = sanitizeResolvers(n)
	n = sanitizeRules(n)
	n = sanitizeLifecycle(n)
	n = sanitizeMaintenance(n)
	// A method on the LogFile type rather than a sanitizeLogFile(Settings) hook
	// like its neighbours, because the type compiles on its own.
	n.LogFile = n.LogFile.Sanitized()
	n = sanitizeReclaim(n)
	n = sanitizeIdleAction(n)
	n = sanitizeCaptcha(n)
	n = sanitizeConfirm(n)
	n = sanitizeTorrent(n)
	n = sanitizeIdentity(n)
	n = sanitizeRelay(n)
	n.ModulesOff = cleanIDList(n.ModulesOff)
	return n
}
