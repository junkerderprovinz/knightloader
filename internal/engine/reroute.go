package engine

import (
	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

// Reroute moves the task's HTTP transfer onto route r, keeping what it has
// written. A running transfer reconnects over r at once, any other goes over
// it when it next runs. It reports false when the engine holds no HTTP
// transfer of the task, or holds one that is being paused or reconnected.
func (e *Engine) Reroute(taskID string, r proxycfg.Route) bool {
	e.mu.Lock()
	gid := e.toGopeed[taskID]
	busy := gid == "" || e.torrents[taskID] || e.closed || e.reconnecting[gid] != nil || e.pausing[gid] > 0
	e.mu.Unlock()
	if busy {
		return false
	}
	proxy := requestProxy(r)
	if proxy == nil {
		proxy = &base.RequestProxy{Mode: base.RequestProxyModeFollow}
	}
	patch := func(gid string) error { return e.d.Patch(gid, &base.Request{Proxy: proxy}, nil) }
	var moved bool
	if t := e.d.GetTask(gid); t != nil && t.Status == base.DownloadStatusRunning {
		moved = e.reconnect(taskID, patch)
	} else {
		moved = patch(gid) == nil
	}
	if moved {
		e.mu.Lock()
		if j, ok := e.jobs[taskID]; ok {
			j.Route = r
			e.jobs[taskID] = j
		}
		e.mu.Unlock()
	}
	return moved
}
