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
)

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
}
