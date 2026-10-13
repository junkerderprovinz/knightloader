package debrid

import (
	"context"
	"time"
)

// Listed is one download on a service's account, as its list shows it.
type Listed struct {
	// ID is the service's id for the download, as TorrentStatus takes it.
	ID   string
	Name string
	Size int64
	// Hash is a torrent's info hash, empty for any other kind of download.
	Hash string
	// Added is when the download was added there, zero when the service does
	// not say.
	Added time.Time
}

// Lister is a service whose downloads can be listed, which the debrid drive
// shows and which finds the job an account already holds for a torrent.
type Lister interface {
	// List returns the downloads on the account. complete reports whether that
	// is all of them rather than the newest page.
	List(ctx context.Context) (list []Listed, complete bool, err error)
}

// parseStamp reads an RFC 3339 time from a service, zero for one it left out
// or wrote some other way.
func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
