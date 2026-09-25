package app

// The engine as the backends that resolve a link and pass the transfer on see
// it: a debrid service or TorBox after its unlock, and the user's own servers
// for a WebDAV address. The file goes where the task's folder rules put it,
// through the working folder and under the collision policy, as a direct
// download's does. Handed the engine as it is, they would write into its own
// folder, whatever the category or package says.

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/engine"
)

type engineHandoff struct {
	*engine.Engine
	a *App
}

// Download is the WebDAV handover, whose login travels as headers.
func (h engineHandoff) Download(taskID, url string, headers map[string]string, conns int) {
	h.start(taskID, url, headers, conns, nil)
}

// Handover is a debrid service's or TorBox's, with its way back to a fresh
// link (see engine.Job.Relink).
func (h engineHandoff) Handover(taskID, url string, conns int, relink func(context.Context) (string, error)) {
	h.start(taskID, url, nil, conns, relink)
}

// start starts the transfer, unless the task was removed while its link was
// being unlocked.
func (h engineHandoff) start(taskID, url string, headers map[string]string, conns int, relink func(context.Context) (string, error)) {
	h.a.mu.Lock()
	t := h.a.tasks[taskID]
	var job engine.Job
	if t != nil {
		job = h.a.engineJobLocked(t, h.a.Settings.Get(), url, headers, conns)
	}
	h.a.mu.Unlock()
	if t == nil {
		return
	}
	job.Relink = relink
	h.Engine.Start(job)
}
