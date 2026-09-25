package app

// What keeping the computer awake counts as work. Only the desktop build asks,
// through internal/keepawake; the machine under a container decides its own
// sleep.

import "github.com/junkerderprovinz/knightloader/internal/core"

// Working reports whether something is under way that sleep would cut off: a
// transfer, an archive being unpacked, a finished file being checked or moved
// out of the working folder, a retry waiting to start again, or an event
// program. A paused, held or switched-off download is not under way, and
// neither is anything while the queue is stopped, or the computer would stay
// up for a download nobody is going to start.
func (a *App) Working() bool {
	if a.delivering.Load() > 0 || (a.EventPrograms != nil && a.EventPrograms.Busy()) {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		switch {
		case t.Status == core.StatusRunning, t.Status == core.StatusExtracting:
			return true
		case t.Status == core.StatusError && !t.NextTry.IsZero() && t.Enabled && !a.halted:
			return true
		}
	}
	return false
}
