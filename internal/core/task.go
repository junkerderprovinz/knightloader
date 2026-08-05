// Package core holds KnightLoader's shared domain types.
package core

import "time"

// Status is the lifecycle state of a download task.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusRunning    Status = "running"
	StatusPaused     Status = "paused"
	StatusExtracting Status = "extracting" // download finished, archive unpacking
	StatusDone       Status = "done"
	StatusError      Status = "error"
)

// Update is a change to a task reported by a download backend (the Gopeed
// engine or the JD backend). Empty fields are left untouched by the app.
type Update struct {
	Status Status
	Name   string
	Size   int64
	Loaded int64
	Speed  int64
	Err    string
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
}
