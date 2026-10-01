package engine

// Carrying a transfer over to another folder. A package renamed on the app's
// side takes its folder along, with the files still being written in it; the
// library keeps writing where it was told at the start, so the transfer is
// stopped while the folder moves and then pointed at the new place, where it
// carries on with the bytes it already has.

import (
	"time"

	"github.com/GopeedLab/gopeed/pkg/download"
)

// hold is a transfer stopped for its files to move. paused is closed once the
// library has stopped writing, and running says whether it was fetching when
// it was stopped, so Release knows whether to carry on.
type hold struct {
	paused  chan struct{}
	running bool
}

// holdWait bounds the wait for the library to report a pause, as
// reconnectWait does.
const holdWait = reconnectWait

// Hold stops the task's HTTP transfer and returns once nothing writes to its
// file any more; the app hears of no pause. A transfer whose link is still
// being resolved is waited for first, since it is about to write. Hold reports
// whether the engine has a transfer for the task that Release can carry over.
// A torrent is not one: the library opens its files in the folder it was added
// in, for as long as it keeps it.
func (e *Engine) Hold(taskID string) bool {
	e.mu.Lock()
	s := e.starting[taskID]
	e.mu.Unlock()
	if s != nil {
		if s.torrent {
			return false
		}
		select {
		case <-s.ready:
		case <-e.done:
			return false
		}
	}
	if e.holdMend(taskID) {
		return true
	}
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	if gid == "" || e.torrents[taskID] || e.closed || e.holds[taskID] != nil {
		e.mu.Unlock()
		return false
	}
	h := &hold{paused: make(chan struct{})}
	e.holds[taskID] = h
	r := e.reconnecting[gid]
	e.mu.Unlock()
	if r != nil {
		<-r.done
	}
	// The library refuses to pause what has already stopped, and reports
	// nothing then.
	if e.d.Pause(&download.TaskFilter{IDs: []string{gid}}) != nil {
		return true
	}
	e.mu.Lock()
	h.running = true
	e.mu.Unlock()
	select {
	case <-h.paused:
	case <-time.After(holdWait):
	case <-e.done:
	}
	return true
}

// holdMend is Hold for a transfer whose missing ranges are being fetched
// again, and reports whether the task has such a mend.
func (e *Engine) holdMend(taskID string) bool {
	e.mu.Lock()
	m := e.mends[taskID]
	if m == nil {
		e.mu.Unlock()
		return false
	}
	h := &hold{running: m.cancel != nil}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	e.holds[taskID] = h
	ended := m.ended
	e.mu.Unlock()
	<-ended
	return true
}

// Release carries a held transfer over to where moved puts each path it wrote
// to, and with resume lets it go on if it was fetching. moved returns a path
// outside the moved folders as it is.
func (e *Engine) Release(taskID string, moved func(string) string, resume bool) {
	e.mu.Lock()
	h := e.holds[taskID]
	delete(e.holds, taskID)
	if j, ok := e.jobs[taskID]; ok {
		j.Dir, j.WorkDir = moved(j.Dir), moved(j.WorkDir)
		e.jobs[taskID] = j
	}
	if j, ok := e.parked[taskID]; ok {
		j.Dir, j.WorkDir = moved(j.Dir), moved(j.WorkDir)
		e.parked[taskID] = j
	}
	m := e.mends[taskID]
	if m != nil {
		m.file = moved(m.file)
		m.job.Dir, m.job.WorkDir = moved(m.job.Dir), moved(m.job.WorkDir)
	}
	gid := e.toGopeed[taskID]
	if f := e.files[gid]; f != "" {
		e.files[gid] = moved(f)
	}
	e.mu.Unlock()
	if gid != "" {
		// The library opens the file again from these options when it goes on.
		if t := e.d.GetTask(gid); t != nil && t.Meta != nil && t.Meta.Opts != nil {
			t.Meta.Opts.Path = moved(t.Meta.Opts.Path)
		}
	}
	if h == nil || !h.running || !resume {
		return
	}
	if m != nil {
		e.resumeMend(taskID)
		return
	}
	_ = e.d.Continue(&download.TaskFilter{IDs: []string{gid}})
}
