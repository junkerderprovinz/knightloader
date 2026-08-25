// Package core holds KnightLoader's shared domain types.
package core

import "time"

// Status is the lifecycle state of a download task.
type Status string

const (
	StatusCollected  Status = "collected" // staged in the link collector, not started
	StatusQueued     Status = "queued"
	StatusRunning    Status = "running"
	StatusPaused     Status = "paused"
	StatusExtracting Status = "extracting" // download finished, archive unpacking
	StatusDone       Status = "done"
	StatusError      Status = "error"
)

// Availability is what we know about the link itself, independent of whether a
// download has been attempted. A link can be staged and known-dead, which is
// exactly what the collector wants to show.
type Availability string

const (
	AvailUnknown Availability = ""        // not checked (or the resolver can't check)
	AvailOnline  Availability = "online"  // the host answered and the file is there
	AvailOffline Availability = "offline" // the host answered and the file is gone
	// AvailUncheckable is the host being asked and refusing to say: a 429, a 503,
	// a transport error, or a resolver with no way to probe at all. Without it
	// every one of those is filed as offline, one flaky minute marks a live link
	// dead, and the user deletes it.
	AvailUncheckable Availability = "uncheckable"
)

// Reason is why a task failed, as a value rather than as prose. The message on
// Error is for the person reading the list; this is what the app itself acts on,
// which is why a reconnect can fire on an address-keyed limit and never on a
// 404. An error nothing recognises stays ReasonUnknown rather than being guessed
// at: a generic failure that reboots the router is worse than no reason at all.
// The taxonomy itself is filled in where failures are classified.
type Reason string

// ReasonUnknown is an error nothing has classified.
const ReasonUnknown Reason = ""

// The taxonomy. There is one value for each failure this app can genuinely tell
// apart, and deliberately none for the ones it can only guess at: the interface
// turns a reason into advice ("the file is gone, delete the link"), and advice
// about the wrong problem is acted on, which a bare error sentence never is.
const (
	// ReasonGone is the host answering that the file is not there: a 404 or a 410.
	ReasonGone Reason = "gone"
	// ReasonAuth is the host wanting credentials it did not get or would not
	// accept: a 401, a 403, or a 407 from a proxy in the way.
	ReasonAuth Reason = "auth"
	// ReasonLimit is an allowance being used up rather than anything being wrong:
	// a 429, a 509, a hoster saying the daily traffic is spent. It is the one
	// failure a new address can fix, which is why reconnect keys on it.
	ReasonLimit Reason = "limit"
	// ReasonUnavailable is the host being up and saying "not now": a 502, 503 or
	// 504, or a hoster in maintenance. Waiting is the whole remedy.
	ReasonUnavailable Reason = "unavailable"
	// ReasonNetwork is the transport failing before an answer arrived: a name that
	// does not resolve, a refused or reset connection, a timeout.
	ReasonNetwork Reason = "network"
	// ReasonDiskFull is the destination running out of space (ENOSPC, or Windows'
	// own disk-full errors). It is the one reason where retrying is pointless AND
	// the user can fix it, so it must never settle as a generic write failure.
	ReasonDiskFull Reason = "diskFull"
	// ReasonUnsupported is no backend claiming the link at all - nothing matched
	// it, or every backend in the chain handed it on.
	ReasonUnsupported Reason = "unsupported"
	// ReasonCaptcha is a backend stopping to ask a human, which nothing in this
	// build can answer for it.
	ReasonCaptcha Reason = "captcha"
	// ReasonCancelled is the run being called off from this side rather than
	// failing: a shutdown, a task taken away underneath the attempt.
	ReasonCancelled Reason = "cancelled"
)

// Origin is the intake path a link arrived by — the paste box, the watch folder,
// Click'n'Load, a container upload. It exists so a rule can be written about it
// and so the list can say where something came from; nothing about a download
// depends on it.
type Origin string

// Update is a change to a task reported by a download backend (the Gopeed
// engine or a delegated backend). Empty fields are left untouched by the app.
type Update struct {
	Status Status
	Name   string
	Size   int64
	Loaded int64
	Speed  int64
	Err    string
	// Retry, when set on an error, asks for another attempt after the delay
	// instead of settling the task (a hoster cool-down, a transient 5xx).
	Retry time.Duration
	// Unsupported says the backend recognised the link as none of its business
	// rather than failing to fetch it. It is the difference between "I cannot
	// do this" and "this did not work", and only the former should hand the
	// task to the next backend in the chain.
	Unsupported bool
	// Torrent carries the swarm numbers when the backend has any, and nil when
	// it does not - which is every update from every non-torrent backend, so
	// nothing else pays for this field.
	//
	// IT IS DELIBERATELY A POINTER AND NOT FIVE MORE FLAT FIELDS. Zero peers on
	// a finished torrent is a true statement; zero peers on an HTTP download is
	// the absence of one, and flat fields cannot tell those apart, so every
	// ordinary update would quietly write "0 peers, 0 seeds, ratio 0" over
	// whatever a torrent task had a moment earlier.
	Torrent *TorrentStats
}

// TorrentStats is one reading of a torrent's swarm, as the engine takes it off
// the download library.
//
// Verified live against gopeed v1.9.3 rather than assumed (cmd/spike-torrent):
// download.Downloader.Stats(taskID) answers a *pkg/protocol/bt.Stats for a
// torrent task, and its TotalPeers/ConnectedSeeders/SeedBytes/SeedRatio are
// where these four numbers come from.
type TorrentStats struct {
	Peers    int
	Seeds    int
	Ratio    float64
	Uploaded int64
	// Seeding is derived, not read: gopeed's own download.Task.Uploading is set
	// at CREATE time for every torrent task and means "this fetcher can upload",
	// not "this one is seeding now" - confirmed live, a task two seconds into its
	// download already reports Uploading true. Seeding is that flag AND a
	// finished download, which is the pair that actually means what the word
	// says. See Engine.readTorrentStats.
	Seeding bool
}

// ApplyTo writes one reading onto a task. It exists so the single place in the
// app that folds an Update into a Task stays one line for this, rather than
// five assignments that a sixth torrent field would have to be remembered in.
func (s TorrentStats) ApplyTo(t *Task) {
	t.Peers = s.Peers
	t.Seeds = s.Seeds
	t.Ratio = s.Ratio
	t.Uploaded = s.Uploaded
	t.Seeding = s.Seeding
}

// TorrentFile is one file inside a multi-file torrent, and the tick beside it.
//
// WHERE THIS LIVES, and why, because docs/torrent-support.md left it open.
// It hangs off core.Task (Task.TorrentFiles) rather than becoming a new
// pre-Task "resolved, awaiting selection" state, for three reasons:
//
//  1. StatusCollected already means exactly "resolved, staged, not started" -
//     the state a file tree is chosen in. A second staging state ahead of it
//     would be a new core.Status in all but name, and section 4 conflict 2 of
//     the build plan has ruled that out for every wave since Wave 1.
//  2. The two intake paths learn the file list at different moments. An
//     uploaded .torrent is parsed with no network at all, so its tree exists
//     before the task does; a magnet's tree arrives only once the swarm hands
//     over metadata, seconds or minutes after the link was pasted. One field
//     on the task lets the magnet stage immediately with an empty list and
//     fill it in later, and lets both paths feed the same tree component.
//  3. Nothing outside the torrent path pays anything: the field is omitempty
//     and every other task carries an empty slice.
//
// Path is the file's path INSIDE the torrent, forward-slashed, relative to the
// torrent's own root folder, exactly as BEP 3 states it - it is not a path on
// this machine and must never be joined onto one without going through the
// containment check in internal/resolver/torrent (Contained). It arrives
// from a stranger's file.
type TorrentFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Selected bool   `json:"selected"`
}

// SelectedTorrentIndices is the file selection in the form the download library
// takes it: positions in the task's own TorrentFiles list.
//
// Nil for a task with no list and nil for a task with every box ticked are the
// same answer on purpose, because they are the same instruction - gopeed reads
// an empty selection as "fetch all of it" (base.Options.InitSelectFiles), which
// is what both mean. Returning an explicit "all" list instead would differ only
// in being longer to serialise and easy to get one short.
func SelectedTorrentIndices(files []TorrentFile) []int {
	if len(files) == 0 {
		return nil
	}
	out := make([]int, 0, len(files))
	for i, f := range files {
		if f.Selected {
			out = append(out, i)
		}
	}
	if len(out) == len(files) {
		return nil
	}
	return out
}

// Task is one download in the queue. It is what the UI renders and the store persists.
type Task struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Package   string    `json:"package"`
	Resolver  string    `json:"resolver"`
	Size      int64     `json:"size"`   // total bytes, 0 = unknown
	Loaded    int64     `json:"loaded"` // bytes downloaded so far
	Speed     int64     `json:"speed"`  // bytes/s (0 when not running)
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`

	// Dir overrides the download destination for this task; empty means the
	// folder derived from settings and package.
	Dir string `json:"dir,omitempty"`
	// Password is tried first when extracting this task's archive.
	Password string `json:"password,omitempty"`
	// Online is what a check said about the link.
	Online Availability `json:"online,omitempty"`
	// Retries counts attempts already made after a failure.
	Retries int `json:"retries,omitempty"`
	// NextTry is when an automatic retry is due (zero = none pending).
	NextTry time.Time `json:"nextTry,omitempty"`
	// Priority lifts a task in the wait queue; higher runs first.
	Priority int `json:"priority"`
	// Position orders tasks of equal priority.
	Position int `json:"position"`
	// Checksum is what verification said about the finished file: empty when
	// nothing was checked, "ok" or "failed".
	Checksum string `json:"checksum,omitempty"`

	// Comment is what a Packagizer rule attached to this task. Nothing in the app
	// reads it; it exists so a rule can leave a note for the person reading the
	// list weeks later.
	Comment string `json:"comment,omitempty"`
	// Chunks is this one download's connection count, set by a Packagizer rule as
	// the link was staged or typed on the row afterwards. Zero means "no opinion":
	// the global setting decides, and the built-in default behind that. It is not
	// "no connections", and it is not "whatever the resolver says" - what a
	// resolver reports is a ceiling on this number, never a replacement for it.
	Chunks int `json:"chunks,omitempty"`
	// AutoExtract overrides the global extraction switch for this one task. Nil
	// is "no rule had an opinion", which is not the same as false: a rule that
	// deliberately switches unpacking off has to survive a global that is on, and
	// with a plain bool the two are the same value.
	AutoExtract *bool `json:"autoExtract,omitempty"`
	// MatchedRules names the Packagizer rules that shaped this task, in the order
	// they fired. It answers "why did this land here" without re-running a rule
	// list that may since have been edited.
	MatchedRules []string `json:"matchedRules,omitempty"`

	// Everything below is widened in one migration for the whole build plan
	// rather than one per package. The store's migration list is append-only and
	// ordered, so fifteen packages each appending an ALTER TABLE is fifteen
	// merge conflicts in one slice and a serialisation point between the agents
	// building them. Several of these are written by a later wave; they are
	// declared and persisted now so that nothing has to reopen this file or
	// store.go to start using one.

	// FinishedAt is when the download settled as done, and zero while it has
	// not. It is not derivable from CreatedAt or from the status, so retention
	// ("remove finished after N days") and a finished-at column both need it
	// recorded at the moment it happens.
	//
	// THE STORE WRITES IT, on the save that first carries a settled task, and
	// clears it again for a task that leaves the done state - a row is never
	// allowed to claim a finish time and still be running. Nothing else may set
	// it: a second writer is how a restarted download keeps the time it finished
	// the first time round. See store.stampFinish.
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Enabled is the user's own switch for one link: a disabled link stays in the
	// list, keeps its progress and is passed over by everything that starts
	// downloads.
	//
	// It defaults to TRUE, in the column as well as here, and that is the single
	// most dangerous line in the schema. A bool's zero value is false, so a
	// column added with the ordinary `DEFAULT 0` would disable every task already
	// in the store on the first boot after the upgrade — a whole queue that
	// silently refuses to run, with nothing on screen to connect it to an update.
	// Every place that builds a Task therefore sets it explicitly.
	Enabled bool `json:"enabled"`
	// Skipped parks a link without failing it: it is not started, and it is not
	// an error either. It is a flag rather than a Status on purpose — a new status
	// value breaks every exhaustive mapping of the seven, the store round trip and
	// a rollback to the previous build.
	Skipped bool `json:"skipped,omitempty"`
	// SkipReason is why, in the app's own words, so the list can say "skipped:
	// the destination is full" instead of just "skipped".
	SkipReason string `json:"skipReason,omitempty"`
	// Hold is a link the user deliberately parked. It is distinct from paused for
	// one reason: "resume everything" must not start the links somebody chose to
	// leave alone.
	Hold bool `json:"hold,omitempty"`
	// Forced starts a task now, past the concurrency and per-host limits — the
	// answer to "everything else can wait, fetch this one".
	Forced bool `json:"forced,omitempty"`
	// DownloadPassword is the password a hoster asks for before it hands over the
	// file. It is NOT Password, which is the archive password tried when
	// unpacking: two different secrets, asked for by two different parties, and
	// conflating them means typing the wrong one into the wrong prompt.
	DownloadPassword string `json:"downloadPassword,omitempty"`
	// ExpectedHash is a checksum supplied with the link rather than found beside
	// the finished file, so verification has something to check against even when
	// no sums file was downloaded.
	ExpectedHash string `json:"expectedHash,omitempty"`
	// Connection is the outbound connection this download is routed over, by the
	// stable id proxycfg assigns. Empty is the machine's own connection.
	Connection string `json:"connection,omitempty"`
	// Host is the file host the link is on, which is not the resolver that
	// fetched it: through a debrid service every download would otherwise claim
	// to come from the same place. It is what a user sorts and filters by.
	Host string `json:"host,omitempty"`
	// Source is the page a crawl found this link on. It was already passed to the
	// rule engine at staging time and then thrown away, so a rule keyed on it
	// could only ever fire once and nothing could show where a link came from.
	Source string `json:"source,omitempty"`
	// MirrorOf names the task this one is a second copy of, when the mirror
	// policy staged it instead of refusing it. That happens only where the user
	// asked for it (settings.KeepMirrors): the default is still to fold a mirror
	// away, so an empty value is the ordinary case and not a gap. A task that
	// carries one is parked - see app.stageSibling - and nothing switches to it
	// on its own.
	MirrorOf string `json:"mirrorOf,omitempty"`
	// Resumable is whether an interrupted transfer can be picked up where it
	// stopped. Nil is a genuine third answer — nobody has asked yet — because
	// warning "you will lose 4.2 GB" about a transfer that resumes fine trains
	// people to click through the dialog.
	Resumable *bool `json:"resumable,omitempty"`
	// Filename is the name the file is to be written under when that is not the
	// name the backend would choose, which is how a rename rule reaches the disk.
	Filename string `json:"filename,omitempty"`
	// Variant is which of several forms of the same resource was picked — a
	// yt-dlp format, a quality. It is kept so a re-run fetches the same one.
	Variant string `json:"variant,omitempty"`
	// Ext is a display-only best-effort file extension, shown next to Name
	// in the collector before a download has actually started (Name itself
	// never carries one for a yt-dlp-routed task — see filename()'s own
	// comment on why Name doubles as the URL-vs-resolved sentinel and must
	// not). Set only where it is genuinely certain ahead of time (a
	// description file, a fixed --audio-format target); left empty rather
	// than guessed everywhere yt-dlp's own eventual format selection could
	// still change it - see app_ytdlp_variants.go's applyProbeFormats for
	// exactly which variant kinds qualify and why. Once a real download
	// starts, the backend's own progress stream supplies the true name
	// (with its own real extension) the ordinary way, superseding this.
	Ext string `json:"ext,omitempty"`
	// AvailableQualities narrows the "Variante" quality picker to what a
	// probed yt-dlp source genuinely offers (jdp, 2026-08-25: "man soll nur
	// die varianten auswählen können die wirklich verfügbar sind" - an old
	// or low-resolution source may not actually have a 1080p/4K stream at
	// all). Read only on a video row; nil/empty means "no opinion yet" (the
	// probe hasn't answered, or this isn't a video row) and the frontend
	// falls back to the full static menu, the same "empty means unset"
	// convention every other optional field on this struct already follows.
	AvailableQualities []string `json:"availableQualities,omitempty"`
	// AvailableAudioFormats narrows the "Variante" audio row's own format
	// picker to what the probed source's own audio-only tracks genuinely
	// carry (jdp, 2026-08-26: "bei der audio spur sollen nur die formate
	// angezeigt werden die wirklich von hoster angeboten werden. Youtube
	// bietet zb keine flac audio") - a source-native codec (its own
	// passthrough extension) rather than a generic ffmpeg transcode target:
	// picking flac from a source that never had lossless audio produces a
	// larger file with no more real fidelity than the lossy source already
	// had, which is worth not offering rather than technically permitting.
	// "best" is always kept even when this is populated (it has no codec of
	// its own to compare against); empty (nothing probed yet, or this isn't
	// an audio row) falls back to AudioFormats()'s full static menu, same
	// convention as AvailableQualities above.
	AvailableAudioFormats []string `json:"availableAudioFormats,omitempty"`
	// AudioBitrate is the "Variante" audio row's own bitrate pick (yt-dlp's
	// own --audio-quality, e.g. "192" for 192 kbit/s) - only meaningful once
	// AudioFormat asks for an actual transcode (a "best" extract has no
	// bitrate to target, it copies the source's own). Empty leaves the
	// bitrate to ffmpeg's own default.
	AudioBitrate string `json:"audioBitrate,omitempty"`
	// ManualPackage marks a package the user chose by hand. Everything that
	// re-packages links automatically has to leave those alone, or a catch-all
	// rule quietly undoes the grouping somebody just did.
	ManualPackage bool `json:"manualPackage,omitempty"`
	// Reason is the typed cause of the current failure; Error is the sentence
	// beside it.
	Reason Reason `json:"reason,omitempty"`
	// Origin is the intake path this link arrived by.
	Origin Origin `json:"origin,omitempty"`
	// ChangedAt is when this task last changed, which is what JD's "Geändert am"
	// column shows and what a list sorted by recent activity needs.
	ChangedAt time.Time `json:"changedAt,omitempty"`
	// ArchivePart is the volume number inside a multi-volume set, or 0 for a file
	// that is not part of one. Extraction already works this out from the name
	// and throws it away, so the list cannot show the parts in order.
	ArchivePart int `json:"archivePart,omitempty"`

	// The torrent fields. Every one is omitempty and every non-torrent task
	// leaves all of them at zero, which is why these are fields here rather than
	// a nested struct nothing else would ever allocate.
	//
	// PEERS, SEEDS, RATIO, UPLOADED AND SEEDING ARE NOT PERSISTED, and that is
	// deliberate rather than an omission. A peer count is true for the second it
	// was read; written to the database and shown again after a restart it is a
	// number about a swarm nobody has looked at since. The two that would survive
	// being stale - Uploaded and Ratio - do not need this table either, because
	// gopeed keeps its own SeedBytes/SeedTime across a restart and reports them
	// back out of the restored task, so a second copy here could only ever
	// disagree with the one the download library is still keeping.

	// Peers is how many peers the swarm has shown us, seeding or not.
	Peers int `json:"peers,omitempty"`
	// Seeds is how many of those are connected and complete - the number that
	// actually says whether this torrent can finish.
	Seeds int `json:"seeds,omitempty"`
	// Ratio is uploaded over downloaded, which is what a seed target is measured
	// against.
	Ratio float64 `json:"ratio,omitempty"`
	// Uploaded is bytes sent to the swarm.
	Uploaded int64 `json:"uploaded,omitempty"`
	// Seeding is a finished torrent still giving bytes back. It is a FLAG beside
	// Status == StatusDone and never a Status of its own, for the reason Skipped
	// and Hold are flags: a new status value breaks every exhaustive mapping of
	// the seven, the store round trip, and a rollback. A seeding task is done -
	// the bytes the user asked for are on disk - and everything that reads "done"
	// must keep reading it as done.
	Seeding bool `json:"seeding,omitempty"`
	// TorrentFiles is the multi-file selection tree. See TorrentFile for where
	// this lives and why.
	//
	// UNLIKE THE FIVE ABOVE THIS ONE DOES WANT PERSISTING: it is a decision the
	// user made, not a reading of the world, and a restart that quietly forgets
	// which files were unticked starts fetching the ones they excluded. See
	// internal/store/store.go's torrent_files column (migration 12).
	TorrentFiles []TorrentFile `json:"torrentFiles,omitempty"`
	// InfoHash and Trackers ALSO want persisting, unlike Peers/Seeds/Ratio/
	// Uploaded/Seeding above: they are a fact about which torrent this is,
	// fixed the moment it was staged, not a reading of a swarm that keeps
	// changing. Set once, at stage time, from the same Describe call that
	// already runs there (app_torrents.go's AddTorrent for an uploaded
	// .torrent, app_links.go's stage for a pasted magnet) - never
	// re-derived later, so there is exactly one place either can go stale.
	// See internal/store/store.go's info_hash/trackers columns (migration 13).
	InfoHash string   `json:"infoHash,omitempty"`
	Trackers []string `json:"trackers,omitempty"`
}
