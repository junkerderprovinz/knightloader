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

// filesAreLocal reports whether this task's bytes landed on this box. A JD
// download lives on the JD machine, so everything that opens or renames the
// finished file has to leave it alone. It is one predicate rather than a
// `Resolver != "jd"` at each of those places, because the day a second remote
// backend arrives, the one that was forgotten is the one that deletes or
// renames a file it cannot see.
func filesAreLocal(t *core.Task) bool { return t.Resolver != "jd" }

// extractWanted is the task's own unpacking switch when a Packagizer rule or
// the user set one, and the global setting otherwise. A rule that says "do not
// unpack this" has to survive a global that says otherwise, or the rule is a
// setting that does nothing.
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

// volumeSetLocked is every task that is a part of the same archive as t,
// including t itself. A file that is not one of several parts is alone in its
// own set, so a caller never has to special-case the ordinary download.
//
// The folder is part of the identity: two unrelated releases both called
// "film.part01.rar", downloaded into two packages, are two archives and not one
// five-part set with three parts missing. Caller holds a.mu.
func (a *App) volumeSetLocked(t *core.Task) []*core.Task {
	key, ok := extract.SetKey(t.Name)
	if !ok {
		return []*core.Task{t}
	}
	dir := a.dirFor(t)
	var out []*core.Task
	for _, other := range a.tasks {
		if k, ok := extract.SetKey(other.Name); ok && k == key && a.dirFor(other) == dir {
			out = append(out, other)
		}
	}
	if len(out) == 0 {
		// t is a copy rather than the live task — every caller inside the app
		// passes the live one, but answering "no set at all" here would silently
		// take a whole archive out of a decision that is about it.
		return []*core.Task{t}
	}
	return out
}

// extractionDueLocked decides whether a finished download now completes an
// archive that should be unpacked, and hands back the volume to open.
//
// The order is the whole point. The candidate is found FIRST and the unpacking
// switch is then read off THAT task, because extractCandidateLocked returns the
// first volume of a multi-part set and not the part that happened to finish
// last. Asked of the finishing part instead, one archive would extract or not
// depending on which of its five parts the hoster served quickest — the same
// rule, the same set, a different answer every time.
// Caller holds a.mu.
func (a *App) extractionDueLocked(done *core.Task, cfg settings.Settings) (*core.Task, string) {
	target, path := a.extractCandidateLocked(done)
	if target == nil || !extractWanted(target, cfg) {
		return nil, ""
	}
	return target, path
}

// extractNowLocked starts the extraction a finished download is due and reports
// the task it moved into StatusExtracting, which for a multi-volume set is the
// first volume rather than the one handed in — the caller has to publish that
// row too, or the list shows the wrong part working.
//
// It is called both when a download finishes and when the switch is turned on
// afterwards, and that is the point: the answer is read at extraction time, so
// a task told to unpack an hour after it landed still unpacks. Re-entry is
// free, because the target has left StatusDone by the time a second call could
// look at it. Caller holds a.mu.
func (a *App) extractNowLocked(done *core.Task, cfg settings.Settings) *core.Task {
	if done.Status != core.StatusDone || !filesAreLocal(done) {
		return nil
	}
	target, path := a.extractionDueLocked(done, cfg)
	if target == nil {
		return nil
	}
	target.Status = core.StatusExtracting
	go a.extractTask(target.ID, path, a.passwordsFor(target))
	return target
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
