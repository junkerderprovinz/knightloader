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
// DownloadMode is whether a transfer goes out on a paid account or anonymously.
//
// Only meaningful for a link a hoster is on the other end of: an ordinary file
// on an ordinary web server is neither free nor premium, it is just a file, and
// labelling it either way would put a word on screen that answers a question
// nobody asked. That case is ModeUnknown, and the interface shows nothing.
type DownloadMode string

const (
	// ModeUnknown is "the question does not apply here", which is the common
	// case: a plain HTTP file, a torrent, a media page.
	ModeUnknown DownloadMode = ""
	// ModeFree is a hoster this app knows, with no account behind it. The
	// download still happens - JD's own plugin does the waiting and the captcha,
	// see jd.PriorityFor - but it happens under the hoster's free limits.
	ModeFree DownloadMode = "free"
	// ModePremium is a hoster reached through an account: a debrid service, or a
	// native login JD has confirmed active.
	ModePremium DownloadMode = "premium"
)

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

// Waiting is why a queued task is not running, as a value rather than as
// silence.
//
// The dispatcher already knows the answer every time it puts a task back: the
// slot count is full, this host is at its own ceiling, the account behind the
// only backend that claims the link is benched, the queue is halted. It knew and
// threw the answer away, so a list of ten queued rows all said "Wartet" and the
// only way to find out which of four completely different situations each one
// was in was to reason about the settings page.
//
// It is not a Reason: a Reason says why something FAILED and belongs to a task
// that has stopped. This says why one that is perfectly healthy has not started,
// which is a different sentence and a different colour.
//
// Empty means "nothing is holding it back" - it is next, or it is not queued at
// all. Every value here is set by dispatchLocked and by nothing else, which is
// what keeps it from drifting: it is recomputed from scratch on every pass, so a
// reason that stops applying disappears on its own rather than needing to be
// cleared by whoever fixed it.
type Waiting string

const (
	// WaitingNone is a task nothing is holding back.
	WaitingNone Waiting = ""
	// WaitingSlot is the global concurrency limit being full.
	WaitingSlot Waiting = "slot"
	// WaitingHost is this host's own limit being full while others are free.
	WaitingHost Waiting = "host"
	// WaitingForced is the separate, smaller pool for forced downloads being
	// full - a different ceiling from WaitingSlot and worth saying apart,
	// because raising MaxConcurrent does not move it.
	WaitingForced Waiting = "forced"
	// WaitingDisabled is the task's own switch being off.
	WaitingDisabled Waiting = "disabled"
	// WaitingHold is the task being parked by hand.
	WaitingHold Waiting = "hold"
	// WaitingCaptcha is a challenge waiting for a human.
	WaitingCaptcha Waiting = "captcha"
	// WaitingAccount is every backend that claims this link having a benched,
	// invalid or expired account behind it. The link is fine and so is the app;
	// the credential is not.
	WaitingAccount Waiting = "account"
	// WaitingHalted is the queue being stopped, by the button or by a timetable
	// window. Every queued task carries it at once, which is the point: a list
	// where nothing moves should say so on the rows, not only in the head card.
	WaitingHalted Waiting = "halted"
	// WaitingDisk is the destination volume not having the room: either it is
	// under the configured floor, or this particular file's remaining bytes
	// plus the reserve would not fit.
	//
	// ONE VALUE FOR BOTH, because the fix is the same sentence either way -
	// free some space, or lower the threshold - and a row that said "this file
	// does not fit" as opposed to "the disk is low" would be inviting a
	// distinction nobody can act on differently. It is a Waiting and not a
	// core.ReasonDiskFull: nothing has failed, no bytes were written, and the
	// download starts on its own the moment the room is there. That difference
	// is the whole point of checking before the transfer rather than after the
	// write that ran out.
	WaitingDisk Waiting = "disk"
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
	// Note is what the backend is doing RIGHT NOW, in its own words, for a task
	// that is running but not moving bytes.
	//
	// It exists because "running at 0 bytes" was the only thing the interface
	// could say while JD sat on "Captcha recognition (rapidgator.net)" and then
	// "Skipped - Captcha is required" (measured on the live instance,
	// 2026-09-03). jdp: "es zeigt wieder nur 'lädt' an ... bei free downloads
	// müsste doch eine captcha abfrage kommen". He was right that a captcha was
	// happening; nothing carried the fact out of the backend.
	//
	// Deliberately NOT Err: this is not a failure, the download is still alive,
	// and putting it in the error field would make every row that is merely
	// waiting look broken. An empty Note means "nothing to add", which is the
	// normal case for a transfer that is simply moving.
	Note string
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
	// GaveUp is a failure the app will not try again ON PURPOSE, as opposed to
	// one that has merely run out of attempts.
	//
	// The two look identical on a list today - both are red, both have no
	// pending retry - and they are not the same thing to the person reading
	// it. "Failed after three attempts" is mended by raising the retry count;
	// "will not be tried again" is not, because nothing about the next ten
	// minutes answers a captcha, frees a byte on a full disk, or overrules a
	// retry rule somebody wrote to say "never for this host". Without the
	// distinction the only way to tell them apart is to remember which reasons
	// the dispatcher exempts, which is a thing nobody remembers.
	//
	// A FLAG BESIDE StatusError, never a Status of its own, for the reason
	// Skipped, Hold and Seeding are flags: a new core.Status value breaks every
	// exhaustive mapping of the seven, the store round trip and a rollback to
	// the previous build.
	//
	// It is raised only where a failure settles and cleared by any dispatch
	// pass that meets the task in the wait queue again (app.dispatchLocked),
	// which is the one point every path back to "we are trying this" goes
	// through - a hand restart included. Not persisted: a task loaded from the
	// store has not failed yet in this process, and a restored flag would be a
	// verdict about an attempt this build never made.
	GaveUp bool `json:"gaveUp,omitempty"`
	// StalledSince is when a running download last moved a byte, set once it
	// has been standing still long enough to be marked (settings.StallTimeout)
	// and zero at every other moment.
	//
	// THE MOMENT AND NOT A DURATION, deliberately. The interface has to show
	// how long this has been going on - a connection that died four hours ago
	// and one that paused a minute ago are not the same news - and a duration
	// written into a field is stale the instant it is sent, so it would have to
	// be rewritten and rebroadcast every second for every stalled row. A single
	// instant is true until it changes and lets whoever renders it count up on
	// its own.
	//
	// It says nothing about the transfer having failed: a stalled task is still
	// running, still holds its slot, and may yet come back on its own. It is
	// also not persisted - it is a reading of a live transfer, like Speed and
	// Note, and a mark restored from the database would describe a connection
	// that no longer exists.
	StalledSince time.Time `json:"stalledSince,omitempty"`
	// StallRestarts counts the automatic restarts the stall watcher has already
	// spent on this task, against settings.StallMaxRestarts. It is what stops
	// "restart when stalled" from being a loop against a host that is refusing.
	StallRestarts int `json:"stallRestarts,omitempty"`
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
	// AvailableAudioBitrates narrows the "Variante" audio row's own bitrate
	// picker to what the probed source's own best audio track can honestly
	// support (jdp, 2026-08-26: "auch die audioqualitäten! bei allen
	// hostern!") - same "empty means no opinion yet, fall back to the full
	// static menu" convention as AvailableQualities/AvailableAudioFormats.
	AvailableAudioBitrates []string `json:"availableAudioBitrates,omitempty"`
	// AudioBitrate is the "Variante" audio row's own bitrate pick (yt-dlp's
	// own --audio-quality, e.g. "192" for 192 kbit/s) - only meaningful once
	// AudioFormat asks for an actual transcode (a "best" extract has no
	// bitrate to target, it copies the source's own). Empty leaves the
	// bitrate to ffmpeg's own default.
	AudioBitrate string `json:"audioBitrate,omitempty"`
	// Category is the named drawer this link is filed in - a folder, a
	// priority, an unpacking switch, a speed limit and a collision rule under
	// one word - by the stable id settings.Category carries. Empty is the
	// ordinary case and means the instance's own answers apply.
	//
	// A REFERENCE AND NOT A COPY. It holds the id and never the settings, so a
	// category renamed, retagged or pointed at a different folder reaches every
	// task that has not started yet, and one that is deleted leaves its tasks
	// behaving exactly like untagged ones (settings.CategoryFor answers the
	// zero category for an id it does not know). The dead id is deliberately
	// left standing rather than cleared: it is the record that somebody filed
	// this download under "Serien", it still reads on the list, and a category
	// re-created under the same id picks its tasks back up. The one value that
	// IS copied is the folder, written into Dir when the download starts,
	// because from the first byte onwards that is not a preference any more but
	// where the file actually is.
	//
	// ON THE TASK AND NOT ON THE PACKAGE, which was the question this field
	// hung on. There is no package entity here - Package is a string, and a
	// package is whatever set of tasks currently share it - so a field there
	// would need a table and a lifecycle nothing else has, to hold a value that
	// has to be resolved per task anyway. It would also make the normal case
	// inexpressible: a package holding an episode, its subtitle and a sample
	// spans drawers, and with the category on the package, moving one link into
	// a package would silently retag it.
	Category string `json:"category,omitempty"`
	// ManualPackage marks a package the user chose by hand. Everything that
	// re-packages links automatically has to leave those alone, or a catch-all
	// rule quietly undoes the grouping somebody just did.
	ManualPackage bool `json:"manualPackage,omitempty"`
	// Reason is the typed cause of the current failure; Error is the sentence
	// beside it.
	Reason Reason `json:"reason,omitempty"`
	// Waiting is why a queued task has not started - see the type. Recomputed
	// by every dispatch pass, never persisted as a decision.
	Waiting Waiting `json:"waiting,omitempty"`
	// Note is the backend's own word for what is happening right now: "Captcha
	// recognition", "Waiting for reconnect", "Skipped - Captcha is required".
	// A live detail, not a failure - see core.Update.Note for why it is separate
	// from Error.
	Note string `json:"note,omitempty"`
	// Mode is whether this download goes out on a paid account or anonymously,
	// and it is a DISPLAY fact rather than a routing one - the routing itself is
	// jd.PriorityFor's job.
	//
	// It exists because the answer was previously invisible (jdp, 2026-09-02:
	// "Wenn man links runterladen möchte für die kein premium account hinterlegt
	// ist muss das angezeigt werden"). A hoster link with no account behind it
	// looks exactly like one with an account behind it, right up until it is
	// slow, queued behind a countdown, or asks for a captcha - and there was
	// nothing on screen to say which of the two you were looking at.
	//
	// Deliberately NOT folded into Reason: Reason is failure-only and is cleared
	// on every requeue, and "this is a free-mode download" is neither a failure
	// nor something that stops being true when the task is retried.
	Mode DownloadMode `json:"mode,omitempty"`
	// ResolverPin nails this one task to a backend. Empty - the ordinary case -
	// leaves the choice to the dispatcher's own ranking.
	//
	// IT IS NOT Resolver ABOVE, and the two must never be merged. Resolver is
	// where the task is right NOW, written by the dispatcher on every pass and
	// rewritten by every fallback; this is what the PERSON said, and nothing in
	// the app is allowed to change it. Kept in one field, a fallback would
	// silently rewrite the instruction it was supposed to be obeying.
	//
	// It exists because a link stuck on one backend had exactly three ways out:
	// delete it, switch the whole instance over and paste it again, or leave it
	// failing. A per-task answer to "no, fetch this one through TorBox" is what
	// that situation was missing.
	//
	// It may name a SERVICE ("alldebrid") or one account slot of it
	// ("alldebrid#work"), matching what settings.ResolverOrder accepts and for
	// the same reason - a person naming a service means all of its keys, and a
	// person naming a slot is being deliberately more specific.
	//
	// A PIN DOES NOT OUTRANK ACCOUNT HEALTH. A backend whose account is benched
	// stays benched, and a task pinned to it fails where it can be seen instead
	// of quietly going out through a different one. See app.pinFailureLocked
	// for that sentence and for why a silent diversion would make the pin mean
	// nothing at all.
	//
	// NOT PERSISTED YET: internal/store has no column for it, so a pin is lost
	// on restart and the task goes back to the ranked chain. The four lines
	// that close that gap are named in this change's own handover note; they
	// live in a file this change does not own.
	ResolverPin string `json:"resolverPin,omitempty"`
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
