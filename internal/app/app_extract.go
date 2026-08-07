package app

// Unpacking: which finished download completes an archive, which passwords are
// tried on it, and what a failed extraction does to the task.

import (
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// extractWanted is the task's own unpacking switch when a Packagizer rule set
// one, and the global setting otherwise. A rule that says "do not unpack this"
// has to survive a global that says otherwise, or the rule is a setting that
// does nothing.
func extractWanted(t *core.Task, cfg settings.Settings) bool {
	if t.AutoExtract != nil {
		return *t.AutoExtract
	}
	return cfg.Extract
}

// extractCandidateLocked decides whether a just-finished download completes an
// archive that can now be unpacked, and returns the task to unpack. For a
// multi-volume set that is the moment the LAST part arrives — and what gets
// unpacked is the first volume, not necessarily the part that finished last.
// Caller holds a.mu.
func (a *App) extractCandidateLocked(done *core.Task) (*core.Task, string) {
	key, isVolume := extract.SetKey(done.Name)
	if !isVolume {
		if extract.Supported(done.Name) {
			return done, filepath.Join(a.dirFor(done), done.Name)
		}
		return nil, ""
	}
	dir := a.dirFor(done)
	var first *core.Task
	for _, t := range a.tasks {
		k, ok := extract.SetKey(t.Name)
		if !ok || k != key || a.dirFor(t) != dir {
			continue
		}
		if t.Status != core.StatusDone {
			// A part is still missing (or already extracting, which means
			// another part got here first). Whoever finishes last triggers it.
			return nil, ""
		}
		if extract.Supported(t.Name) && (first == nil || t.Name < first.Name) {
			first = t
		}
	}
	if first == nil {
		return nil, "" // parts without a first volume: nothing to open
	}
	return first, filepath.Join(dir, first.Name)
}

// extractTask unpacks a finished archive download and settles the task back to
// done — extraction failures are recorded on the task but don't undo the
// completed download.
// passwordsFor is the order archive passwords are tried in: the task's own
// first, because it was set for exactly this file, then the global list.
func (a *App) passwordsFor(t *core.Task) []string {
	var out []string
	if t != nil && t.Password != "" {
		out = append(out, t.Password)
	}
	return append(out, a.Settings.Get().ArchivePasswords...)
}

func (a *App) extractTask(id, path string, passwords []string) {
	res, err := extract.ExtractWith(path, passwords)
	if err == nil && a.Settings.Get().DeleteArchive {
		for _, v := range res.Volumes {
			_ = os.Remove(v)
		}
	}
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Status = core.StatusDone
	if err != nil {
		t.Error = "extract: " + err.Error()
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}
