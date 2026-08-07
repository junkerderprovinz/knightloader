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
	// Chunks overrides how many connections this download opens. Zero means the
	// resolver's answer, or the built-in default when it has none.
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
	// policy staged it instead of refusing it.
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
}
