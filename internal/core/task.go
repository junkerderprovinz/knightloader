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

// Availability is what is known about the link itself, independent of any
// download attempt. A staged link can be known to be dead.
type Availability string

const (
	AvailUnknown Availability = ""        // not checked (or the resolver can't check)
	AvailOnline  Availability = "online"  // the host answered and the file is there
	AvailOffline Availability = "offline" // the host answered and the file is gone
	// AvailUncheckable is a host that was asked and would not say: a 429, a
	// 503, a transport error, or a resolver that cannot probe. Filing these as
	// offline would mark live links dead after one flaky minute.
	AvailUncheckable Availability = "uncheckable"
)

// DownloadMode is whether a transfer uses a paid account or goes out
// anonymously. It only applies to hoster links; for a plain file it is
// ModeUnknown and nothing is shown.
type DownloadMode string

const (
	// ModeUnknown means the question does not apply, as for a plain HTTP
	// file, a torrent or a media page.
	ModeUnknown DownloadMode = ""
	// ModeFree is a known hoster with no account behind it. JD's plugin still
	// handles the waiting and the captcha (see jd.PriorityFor), under the
	// hoster's free limits.
	ModeFree DownloadMode = "free"
	// ModePremium is a hoster reached through an account: a debrid service, or
	// a native login JD has confirmed active.
	ModePremium DownloadMode = "premium"
)

// Reason is why a task failed, as a value the app acts on; Error holds the
// sentence for people. A reconnect fires on an address-keyed limit and never
// on a 404. An unrecognised error stays ReasonUnknown rather than being
// guessed at.
type Reason string

// ReasonUnknown is an error nothing has classified.
const ReasonUnknown Reason = ""

// There is one value for each failure the app can really tell apart. The
// interface turns a reason into advice, and advice about the wrong problem
// gets acted on.
const (
	// ReasonGone is a 404 or 410.
	ReasonGone Reason = "gone"
	// ReasonAuth is missing or rejected credentials: a 401, a 403, or a 407
	// from a proxy.
	ReasonAuth Reason = "auth"
	// ReasonLimit is a used-up allowance: a 429, a 509, a spent daily
	// traffic quota. It is the one failure a new address can fix, which is
	// why reconnect keys on it.
	ReasonLimit Reason = "limit"
	// ReasonUnavailable is a host that is up but says "not now": a 502, 503
	// or 504, or maintenance.
	ReasonUnavailable Reason = "unavailable"
	// ReasonNetwork is the transport failing before an answer: DNS, a refused
	// or reset connection, a timeout.
	ReasonNetwork Reason = "network"
	// ReasonDiskFull is the destination out of space. Retrying is pointless
	// and the user can fix it, so it must not settle as a generic write
	// failure.
	ReasonDiskFull Reason = "diskFull"
	// ReasonUnsupported is no backend claiming the link.
	ReasonUnsupported Reason = "unsupported"
	// ReasonCaptcha is a backend stopping to ask a person.
	ReasonCaptcha Reason = "captcha"
	// ReasonCancelled is the run being called off from this side, by a
	// shutdown or a task removed mid-attempt.
	ReasonCancelled Reason = "cancelled"

	// The reasons below are set by the backend that hit them rather than by
	// the shared classifier (see Update.Reason and
	// internal/resolver/ytdlp/diagnose.go). Each has its own remedy.

	// ReasonBotCheck is a site refusing an address it thinks is automated.
	// Only a signed-in session clears it.
	ReasonBotCheck Reason = "botCheck"
	// ReasonMembersOnly is a video behind a membership the session lacks.
	ReasonMembersOnly Reason = "membersOnly"
	// ReasonGeoBlocked is a site refusing this region. It is keyed to the
	// address, so no credential fixes it.
	ReasonGeoBlocked Reason = "geoBlocked"
	// ReasonDRM is encrypted media whose key only goes to a trusted player.
	ReasonDRM Reason = "drm"
	// ReasonExtractorBroken is the tool failing to read the page, almost
	// always because the site changed. A newer yt-dlp, which in the container
	// means a newer image, usually fixes it.
	ReasonExtractorBroken Reason = "extractorBroken"
)

// Waiting is why a healthy queued task has not started. It is not a Reason,
// which belongs to a task that failed.
//
// Empty means nothing is holding it back. Only dispatchLocked sets it, and it
// is recomputed on every pass, so a cause that stops applying disappears on
// its own.
type Waiting string

const (
	WaitingNone Waiting = ""
	// WaitingSlot is the global concurrency limit being full.
	WaitingSlot Waiting = "slot"
	// WaitingHost is this host's own limit being full while others are free.
	WaitingHost Waiting = "host"
	// WaitingForced is the smaller pool for forced downloads being full.
	// Raising MaxConcurrent does not move it.
	WaitingForced Waiting = "forced"
	// WaitingDisabled is the task's own switch being off.
	WaitingDisabled Waiting = "disabled"
	// WaitingHold is the task being parked by hand.
	WaitingHold Waiting = "hold"
	// WaitingCaptcha is a challenge waiting for a person.
	WaitingCaptcha Waiting = "captcha"
	// WaitingAccount is every backend that claims the link having a benched,
	// invalid or expired account.
	WaitingAccount Waiting = "account"
	// WaitingHalted is the queue being stopped, by the button or a timetable
	// window. Every queued task carries it.
	WaitingHalted Waiting = "halted"
	// WaitingDisk is the destination volume lacking room, either below the
	// configured floor or too small for this file plus the reserve. The fix
	// is the same either way, so there is one value. Nothing has failed; the
	// download starts once there is room.
	WaitingDisk Waiting = "disk"
	// WaitingVolume is the volume allowance for the period being used up with
	// the cap set to hold the queue. It differs from WaitingHalted because it
	// ends when the period restarts or the cap is raised, not on play.
	WaitingVolume Waiting = "volumeCap"
	// WaitingModule is the backend the link needs (JDownloader, yt-dlp, the
	// torrent engine) being switched off on the modules page. Falling through
	// to the direct download instead would save the hoster's web page.
	WaitingModule Waiting = "module"
)

// Origin is the intake path a link arrived by: the paste box, the watch
// folder, Click'n'Load, a container upload. Rules and the list use it; no
// download depends on it.
type Origin string

// Update is a change to a task reported by a download backend. Empty fields
// are left untouched.
type Update struct {
	Status Status
	Name   string
	Size   int64
	Loaded int64
	Speed  int64
	Err    string
	// Retry, when set on an error, asks for another attempt after the delay
	// instead of settling the task.
	Retry time.Duration
	// Unsupported says the backend does not handle this link at all, as
	// opposed to failing to fetch it. Only this hands the task to the next
	// backend.
	Unsupported bool
	// Reason is a cause the backend itself recognised, and it wins over the
	// shared classifier in internal/app. Only the backend has seen its tool's
	// full output; Err is cut short. Empty means no opinion.
	Reason Reason
	// Note is what the backend is doing now for a running task that is not
	// moving bytes, such as "Captcha recognition (rapidgator.net)". It is not
	// an error, so it is kept out of Err.
	Note string
	// Torrent carries swarm numbers, or nil from any other backend. It is a
	// pointer so an ordinary update does not overwrite a torrent's readings
	// with zeros.
	Torrent *TorrentStats
}

// TorrentStats is one reading of a torrent's swarm, taken from gopeed's
// bt.Stats for the task.
type TorrentStats struct {
	Peers    int
	Seeds    int
	Ratio    float64
	Uploaded int64
	// Seeding is derived: gopeed's Task.Uploading is set at creation for
	// every torrent, so seeding means that flag and a finished download.
	Seeding bool
}

// ApplyTo writes one reading onto a task, zeros included.
func (s TorrentStats) ApplyTo(t *Task) {
	t.Peers = s.Peers
	t.Seeds = s.Seeds
	t.Ratio = s.Ratio
	t.Uploaded = s.Uploaded
	t.Seeding = s.Seeding
}

// TorrentFile is one file inside a multi-file torrent and whether it is
// selected. The list hangs off Task because StatusCollected already means
// "staged, not started", and because a magnet's file list only arrives after
// the task exists.
//
// Path is the path inside the torrent, as the torrent states it. It comes
// from a stranger's file and must not be joined onto a local path without
// the containment check in internal/resolver/torrent.
type TorrentFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Selected bool   `json:"selected"`
}

// SelectedTorrentIndices returns the selected positions in files, the form
// gopeed takes. It returns nil both for no list and for everything selected,
// since gopeed reads an empty selection as "fetch all".
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
	// MaxTries is how many automatic retries this failure gets, as
	// settings.RetryFor answered when it settled; Retries counts against it.
	// Zero means nobody has said, and the denominator is not shown. It is a
	// snapshot: raising the setting does not change rows that already failed.
	// Not persisted, since the policy may change while the process is gone.
	MaxTries int `json:"maxTries,omitempty"`
	// GaveUp marks a failure the app will not retry on purpose (a captcha, a
	// full disk, a "never" rule), as opposed to one that ran out of attempts.
	// It is a flag beside StatusError because a new Status would break every
	// exhaustive mapping, the store round trip and a rollback. The next
	// dispatch pass that meets the task clears it. Not persisted.
	GaveUp bool `json:"gaveUp,omitempty"`
	// StalledSince is when a running download last moved a byte, set once it
	// has stood still for settings.StallTimeout and zero otherwise. It is an
	// instant rather than a duration so it does not need rebroadcasting every
	// second. A stalled task is still running. Not persisted.
	StalledSince time.Time `json:"stalledSince,omitempty"`
	// StallRestarts counts the automatic stall restarts spent on this task,
	// against settings.StallMaxRestarts.
	StallRestarts int `json:"stallRestarts,omitempty"`
	// Priority lifts a task in the wait queue; higher runs first.
	Priority int `json:"priority"`
	// Position orders tasks of equal priority.
	Position int `json:"position"`
	// Checksum is what verification said about the finished file: empty when
	// nothing was checked, "ok" or "failed".
	Checksum string `json:"checksum,omitempty"`

	// Comment is a note a Packagizer rule attached for the person reading the
	// list. Nothing in the app reads it.
	Comment string `json:"comment,omitempty"`
	// Chunks is this download's connection count, from a rule or the row.
	// Zero means the global setting decides; a resolver's figure is a ceiling
	// on it, not a replacement.
	Chunks int `json:"chunks,omitempty"`
	// AutoExtract overrides the global extraction switch for this task. Nil
	// means no rule had an opinion, which differs from false.
	AutoExtract *bool `json:"autoExtract,omitempty"`
	// MatchedRules names the Packagizer rules that shaped this task, in the
	// order they fired.
	MatchedRules []string `json:"matchedRules,omitempty"`

	// FinishedAt is when the download settled as done, zero until then. Only
	// the store sets it, and clears it when a task leaves the done state, so a
	// restarted download does not keep its first finish time. See
	// store.stampFinish.
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Enabled is the user's switch for one link; a disabled link keeps its
	// place and progress but is not started. It defaults to true in the column
	// too, because a false default would disable the whole queue on upgrade,
	// and every place that builds a Task sets it explicitly.
	Enabled bool `json:"enabled"`
	// Skipped parks a link without failing it. It is a flag rather than a
	// Status for the same reason as GaveUp.
	Skipped bool `json:"skipped,omitempty"`
	// SkipReason is why, in the app's own words.
	SkipReason string `json:"skipReason,omitempty"`
	// Hold is a link the user parked. Unlike paused, "resume everything" does
	// not start it.
	Hold bool `json:"hold,omitempty"`
	// Forced starts a task now, past the concurrency and per-host limits.
	Forced bool `json:"forced,omitempty"`
	// ConfirmDue is when the auto-confirm countdown holding this collected
	// link runs out, zero when none is. It is persisted so a restart counts
	// down again rather than leaving an unattended batch in the collector.
	ConfirmDue time.Time `json:"confirmDue,omitzero"`
	// DownloadPassword is the password a hoster asks for before handing over
	// the file. It is not Password, which is the archive password.
	DownloadPassword string `json:"downloadPassword,omitempty"`
	// ExpectedHash is a checksum supplied with the link, for when no sums
	// file comes with the download.
	ExpectedHash string `json:"expectedHash,omitempty"`
	// Connection is the outbound connection this download is routed over, by
	// the id proxycfg assigns. Empty is the machine's own connection.
	Connection string `json:"connection,omitempty"`
	// Host is the file host the link is on, which through a debrid service
	// differs from the resolver that fetched it.
	Host string `json:"host,omitempty"`
	// Source is the page a crawl found this link on.
	Source string `json:"source,omitempty"`
	// MirrorOf names the task this one is a second copy of, when
	// settings.KeepMirrors staged it rather than folding it away. Such a task
	// is parked (see app.stageSibling).
	MirrorOf string `json:"mirrorOf,omitempty"`
	// Resumable is whether an interrupted transfer can continue. Nil means
	// nobody has asked yet, so no data-loss warning is shown for a transfer
	// that resumes fine.
	Resumable *bool `json:"resumable,omitempty"`
	// Filename is the name to write the file under when it is not the one the
	// backend would choose, which is how a rename rule reaches the disk.
	Filename string `json:"filename,omitempty"`
	// Variant is which form of the resource was picked, such as a yt-dlp
	// format, so a re-run fetches the same one.
	Variant string `json:"variant,omitempty"`
	// VariantOff sets aside a collected variant row whose kind its host's
	// preset leaves out: kept, switched off and out of the collector's view,
	// so ticking the kind again brings the row back with its own pick.
	VariantOff bool `json:"variantOff,omitempty"`
	// Ext is a display-only file extension shown next to Name before a
	// download starts, since Name never carries one for a yt-dlp task. It is
	// set only where the extension is certain in advance (see
	// app_ytdlp_variants.go's applyProbeFormats); the backend's real name
	// supersedes it once the download runs.
	Ext string `json:"ext,omitempty"`
	// AvailableQualities narrows the quality picker to what a probed yt-dlp
	// video source offers. Empty means not probed yet, and the full menu is
	// shown.
	AvailableQualities []string `json:"availableQualities,omitempty"`
	// AvailableVideoFormats lists the distinct video tracks a probed source
	// offers, by height, frame rate, container and codec (see
	// ytdlp.VideoFormats), for the quality picker beside the height caps.
	AvailableVideoFormats []string `json:"availableVideoFormats,omitempty"`
	// AvailableAudioFormats narrows the audio format picker to the source's
	// own audio tracks (see ytdlp.AudioTracks) and codecs, so a lossy source
	// is not offered as flac. "best" is always kept; empty falls back to the
	// full menu.
	AvailableAudioFormats []string `json:"availableAudioFormats,omitempty"`
	// AvailableAudioBitrates narrows the audio bitrate picker to what the
	// source's best audio track supports. Empty falls back to the full menu.
	AvailableAudioBitrates []string `json:"availableAudioBitrates,omitempty"`
	// AudioBitrate is the audio bitrate pick (yt-dlp's --audio-quality, e.g.
	// "192"). It only matters when AudioFormat asks for a transcode; empty
	// leaves it to ffmpeg.
	AudioBitrate string `json:"audioBitrate,omitempty"`
	// Category is the id of the settings.Category this link is filed under,
	// or empty for the instance defaults.
	//
	// It is a reference, not a copy, so a category edit reaches every task not
	// yet started, and a deleted one leaves its tasks behaving as untagged
	// (settings.CategoryFor answers the zero category). The dead id is kept so
	// a category re-created under it picks its tasks back up. Only the folder
	// is copied, into Dir when the download starts.
	//
	// It lives on the task because a package is only a shared string here,
	// and one package often spans categories.
	Category string `json:"category,omitempty"`
	// ExtractDir is where the extracted content goes when a Packagizer rule
	// named a folder. Empty falls back to Settings.ExtractMoveTo (see
	// app.extractMoveTarget). Dir is where the archive lands; this is where
	// its content goes.
	ExtractDir string `json:"extractDir,omitempty"`
	// ManualPackage marks a package the user chose by hand, which automatic
	// re-packaging must leave alone.
	ManualPackage bool `json:"manualPackage,omitempty"`
	// Reason is the typed cause of the current failure; Error is the sentence
	// beside it.
	Reason Reason `json:"reason,omitempty"`
	// Waiting is why a queued task has not started. It is recomputed by every
	// dispatch pass.
	Waiting Waiting `json:"waiting,omitempty"`
	// Note is the backend's own word for what is happening now, such as
	// "Captcha recognition". See Update.Note.
	Note string `json:"note,omitempty"`
	// Mode says whether the download uses a paid account, for display only;
	// routing is jd.PriorityFor's job. It is not a Reason, which is
	// failure-only and cleared on requeue.
	Mode DownloadMode `json:"mode,omitempty"`
	// ResolverPin fixes this task to a backend; empty leaves the choice to
	// the dispatcher's ranking.
	//
	// It is separate from Resolver, which records where the task is now and
	// changes on every fallback; this is what the person asked for, and the
	// app never changes it. It names a service ("alldebrid") or one account
	// slot ("alldebrid#work"), as settings.ResolverOrder does.
	//
	// A pin does not outrank account health: a task pinned to a benched
	// account fails visibly rather than going out through another backend
	// (see app.pinFailureLocked). The store has no column for it, so a pin is
	// lost on restart.
	ResolverPin string `json:"resolverPin,omitempty"`
	// Origin is the intake path this link arrived by.
	Origin Origin `json:"origin,omitempty"`
	// ChangedAt is when this task last changed, for sorting by recent
	// activity.
	ChangedAt time.Time `json:"changedAt,omitempty"`
	// ArchivePart is the volume number inside a multi-volume set, or 0.
	ArchivePart int `json:"archivePart,omitempty"`

	// The torrent fields stay zero for every other task. Peers, Seeds, Ratio,
	// Uploaded and Seeding are live readings and not persisted; gopeed keeps
	// its own upload totals across a restart.

	// Peers is how many peers the swarm has shown, seeding or not.
	Peers int `json:"peers,omitempty"`
	// Seeds is how many of those are connected and complete.
	Seeds int `json:"seeds,omitempty"`
	// Ratio is uploaded over downloaded.
	Ratio float64 `json:"ratio,omitempty"`
	// Uploaded is bytes sent to the swarm.
	Uploaded int64 `json:"uploaded,omitempty"`
	// Seeding is a finished torrent still uploading. It is a flag beside
	// StatusDone, for the same reason as GaveUp, and a seeding task reads as
	// done everywhere.
	Seeding bool `json:"seeding,omitempty"`
	// TorrentFiles is the multi-file selection. It is persisted, since it is
	// the user's decision and forgetting it would fetch excluded files.
	TorrentFiles []TorrentFile `json:"torrentFiles,omitempty"`
	// InfoHash and Trackers identify the torrent. They are set once at stage
	// time and persisted.
	InfoHash string   `json:"infoHash,omitempty"`
	Trackers []string `json:"trackers,omitempty"`
}
