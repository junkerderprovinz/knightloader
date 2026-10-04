package settings

// The debrid drive: what is on the debrid accounts, served read-only over
// WebDAV at /dav/ for an rclone mount (internal/debriddrive).

// DebridDrive is the drive's switch and its refresh interval.
type DebridDrive struct {
	// Enabled opens /dav/. It is off by default because it hands whoever
	// holds a token that can read every file on every debrid account, and
	// even when on, the route refuses a request without such a token.
	Enabled bool `json:"enabled"`
	// RefreshMinutes is how long a listing read from a service is shown
	// before the service is asked again.
	RefreshMinutes int `json:"refreshMinutes"`
}

// DefaultDriveRefresh is the refresh interval an install starts with, in
// minutes: rclone's own directory cache is five minutes long, and a download
// added on the service shows within ten.
const DefaultDriveRefresh = 5

// maxDriveRefresh is a day. A longer interval would hide a download added on
// the service until the next restart, for all anybody could tell.
const maxDriveRefresh = 24 * 60

func defaultDebridDrive() DebridDrive {
	return DebridDrive{RefreshMinutes: DefaultDriveRefresh}
}

// sanitizeDebridDrive puts the interval inside its range. A minute is the
// floor, so the drive never asks a service for its list on every request.
func sanitizeDebridDrive(n Settings) Settings {
	switch {
	case n.DebridDrive.RefreshMinutes <= 0:
		n.DebridDrive.RefreshMinutes = DefaultDriveRefresh
	case n.DebridDrive.RefreshMinutes > maxDriveRefresh:
		n.DebridDrive.RefreshMinutes = maxDriveRefresh
	}
	return n
}
