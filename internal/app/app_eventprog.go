package app

// Event programs: the seam between this package and internal/eventprog, which
// subscribes to the event stream on its own. What it needs from here is the
// configuration, a way to read its health, and where an event's file is, which
// no script.Firing carries.

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newEventPrograms builds the dispatcher New subscribes to the bus. run is
// execx.Run when nil; a test hands in one that records instead.
func (a *App) newEventPrograms(run eventprog.Runner) *eventprog.Dispatcher {
	return eventprog.New(eventprog.Options{
		InstanceName: func() string { return a.Settings.Get().InstanceName },
		Locate:       a.locateEvent,
		Ready:        a.eventReady,
		Run:          run,
	})
}

// applyEventPrograms makes the running dispatcher match the configuration, the
// Modules page's switch included. It runs on every settings change, so a new
// program needs no restart.
func (a *App) applyEventPrograms(s settings.Settings) {
	if a.EventPrograms == nil {
		return
	}
	a.EventPrograms.Set(s.EventPrograms)
	a.EventPrograms.SetOff(s.ModuleOff("eventprograms"))
}

// EventProgramHealth reports what each program has done since this process
// started, or nil when there is no dispatcher.
func (a *App) EventProgramHealth() []eventprog.Health {
	if a.EventPrograms == nil {
		return nil
	}
	return a.EventPrograms.Health()
}

// locateEvent says where an event's file is and which category it is in, for a
// program about to start. It runs on the program's worker, so the answer is
// where the file is at that moment.
func (a *App) locateEvent(f script.Firing) eventprog.Where {
	cfg := a.Settings.Get()
	switch {
	case f.Extract != nil:
		w := eventprog.Where{Folder: a.extractedTo(f.Extract.JobID, f.Extract.Dir)}
		if f.Task != nil {
			w.File, _, w.Category = a.taskWhere(f.Task.ID, cfg)
		}
		return w
	case f.Package != nil:
		return a.packageWhere(f.Package.Name, cfg)
	case f.Task != nil:
		var w eventprog.Where
		w.File, w.Folder, w.Category = a.taskWhere(f.Task.ID, cfg)
		return w
	}
	return eventprog.Where{}
}

// eventReady reports whether a finished download's file, or every finished
// file of a package, has left the working folder. A task turns Done before its
// checksum is checked and the file moved, and a program started then would be
// handed a file the app is about to hash and take away; on Windows, a program
// holding it open would make the move fail.
func (a *App) eventReady(f script.Firing) bool {
	switch {
	case f.Trigger == script.TriggerTaskDone && f.Task != nil:
		return a.taskFileLanded(f.Task.ID)
	case f.Trigger == script.TriggerPackageDone && f.Package != nil:
		return a.packageFilesLanded(f.Package.Name)
	}
	return true
}

// taskWhere is one download's file, the folder it is in and its category's
// name. A file fetched on another machine has no path here, and a download
// that never got a name of its own, whose name is still its link, never had a
// file, so both are left empty rather than made up.
func (a *App) taskWhere(id string, cfg settings.Settings) (file, folder, category string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	if t == nil {
		return "", "", ""
	}
	category = categoryName(cfg, t.Category)
	if !filesAreLocal(t) || (t.File == "" && (t.Name == "" || t.Name == t.URL)) {
		return "", "", category
	}
	file = a.fileOfLocked(t)
	return file, filepath.Dir(file), category
}

// packageWhere is the folder and category most of a package's files went to.
// A package can span categories, the sample beside the film, and a program is
// given the one that describes the package best rather than none.
func (a *App) packageWhere(name string, cfg settings.Settings) eventprog.Where {
	name = strings.TrimSpace(name)
	if name == "" {
		return eventprog.Where{}
	}
	var folders, categories []string
	a.mu.Lock()
	for _, t := range a.tasks {
		if strings.TrimSpace(t.Package) != name || t.Status != core.StatusDone {
			continue
		}
		if filesAreLocal(t) {
			folders = append(folders, a.dirFor(t))
		}
		categories = append(categories, categoryName(cfg, t.Category))
	}
	a.mu.Unlock()
	return eventprog.Where{Folder: mostCommon(folders), Category: mostCommon(categories)}
}

// extractedTo is where an unpacking's content ended up: the folder it was
// moved to afterwards when it was, and the one it was unpacked into otherwise.
func (a *App) extractedTo(jobID, dir string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if j := a.unpackLocked().jobs[jobID]; j != nil && j.MovedTo != "" {
		return j.MovedTo
	}
	return dir
}

// categoryName is what a program is told a category is called: its name, its
// ID when it has none, and nothing for a deleted category, since a task filed
// under one behaves as having none.
func categoryName(cfg settings.Settings, id string) string {
	c := cfg.CategoryFor(id)
	if c.ID == "" {
		return ""
	}
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	return c.ID
}

// mostCommon is the value that occurs most often, ties going to the one that
// sorts first so the answer does not change between two identical runs. Empty
// values are not counted.
func mostCommon(values []string) string {
	count := map[string]int{}
	for _, v := range values {
		if v != "" {
			count[v]++
		}
	}
	keys := make([]string, 0, len(count))
	for v := range count {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	best := ""
	for _, v := range keys {
		if count[v] > count[best] {
			best = v
		}
	}
	return best
}
