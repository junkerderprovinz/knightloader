package engine

import (
	"errors"

	"github.com/GopeedLab/gopeed/pkg/download"
)

var (
	// ErrNotStreaming is a task whose bytes the engine does not hand out
	// while it downloads, because it does not run it or has not yet handed it
	// to the library.
	ErrNotStreaming = errors.New("the engine is not downloading this task")
	// ErrMending is a finished transfer whose missing ranges are being
	// fetched again, which the library already calls complete.
	ErrMending = errors.New("part of this download is being fetched again")
)

// Stream opens a file of a task the engine runs, for reading before the
// transfer has finished: file is the index of a torrent's file and 0 for
// anything else. A read waits for bytes that have not arrived, and the
// transfer fetches what is read, and what lies just ahead of it, before the
// rest until the reader is closed. For a torrent the pieces at both ends of
// the file come first too, since players read a container's index there.
func (e *Engine) Stream(taskID string, file int) (download.StreamReader, error) {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	_, mending := e.mends[taskID]
	e.mu.Unlock()
	switch {
	case mending:
		return nil, ErrMending
	case gid == "":
		return nil, ErrNotStreaming
	}
	return e.d.Stream(gid, file)
}
