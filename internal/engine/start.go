package engine

// A start resolves its link before the library has a task for it, and a
// magnet can take minutes to name its files. Pause and Remove find no task to
// act on in that time, so they leave a mark on the start instead, and the
// start reads it before it hands the task over.

// start is one Start between its call and the task's creation in the library.
// ready is closed when it ends either way. creating is set once the marks have
// been read; from then on Pause and Remove wait for ready and find the task.
type start struct {
	ready    chan struct{}
	torrent  bool
	creating bool
	paused   bool
	removed  bool
}

func (e *Engine) beginStart(j Job, torrent bool) *start {
	s := &start{ready: make(chan struct{}), torrent: torrent}
	e.mu.Lock()
	e.starting[j.TaskID] = s
	e.mu.Unlock()
	return s
}

// startEnded marks a start as over, mapped or given up.
func (e *Engine) startEnded(taskID string, s *start) {
	e.mu.Lock()
	if e.starting[taskID] == s {
		delete(e.starting, taskID)
	}
	e.mu.Unlock()
	close(s.ready)
}

// proceed reports whether a start goes on to create its task or report its
// failure. A start removed meanwhile ends silently, and one paused meanwhile
// is parked for Resume to start again.
func (e *Engine) proceed(j Job) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.starting[j.TaskID]
	switch {
	case s == nil || s.creating:
		return true
	case s.removed:
		return false
	case s.paused:
		e.parked[j.TaskID] = j
		return false
	}
	s.creating = true
	return true
}

// markStart applies mark to the task's start if it has not read its marks yet,
// and reports whether it did. A start already creating its task is waited for,
// so the caller then finds the task mapped.
func (e *Engine) markStart(taskID string, mark func(*start)) bool {
	e.mu.Lock()
	s := e.starting[taskID]
	if s == nil {
		e.mu.Unlock()
		return false
	}
	if !s.creating {
		mark(s)
		e.mu.Unlock()
		return true
	}
	e.mu.Unlock()
	select {
	case <-s.ready:
	case <-e.done:
	}
	return false
}

// unpark starts a parked task again, and reports whether there was one.
func (e *Engine) unpark(taskID string) bool {
	e.mu.Lock()
	j, ok := e.parked[taskID]
	delete(e.parked, taskID)
	e.mu.Unlock()
	if ok {
		e.Start(j)
	}
	return ok
}
