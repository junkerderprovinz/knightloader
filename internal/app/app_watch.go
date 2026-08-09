package app

// The drop folders and what a file dropped into one asked for.
//
// This is the one intake nobody is sitting in front of. A file lands on a share
// and the links are staged minutes later, so every question the collector would
// normally put to a person has to be answered from the file itself. That is what
// shapes the two halves below: applyWatchFolders keeps the set of watched
// folders matching the configuration without ever tearing down a folder that did
// not change, and stageWatchJob carries out as much of one job's stated intent
// as this app has a switch for.
//
// The single-folder path in app.go (applyWatcher) is what this replaces; the
// switch is one line, `a.applyWatchFolders(applied)` in place of
// `a.applyWatcher(applied.WatchDir)`, in New and in ApplySettings.

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// envWatchDirs names drop folders beyond the one the settings hold, separated
// the way the platform separates a path list.
//
// It is an environment variable and not a second setting because the settings
// form replaces the whole configuration object on save: a list stored beside
// WatchDir that no page renders would come back empty from the first save
// anybody made, and a watch folder that disappears when you change the speed
// limit is a bug nobody would connect to the two. An operator who mounted three
// shares into the container names all three here, and nothing in the interface
// can reach the value to lose it.
const envWatchDirs = "KL_WATCH_DIRS"

// watchFolders is the set of drop folders the configuration asks for. It is the
// one place the sources are combined, so the day the settings grow a list of
// their own this is the only reader that has to learn about it.
func watchFolders(s settings.Settings) []watch.Folder {
	var out []watch.Folder
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		// A textual match only, to keep the same folder from being logged twice
		// when the environment repeats what the settings already say. Two
		// spellings of one directory are the watcher's problem, and it resolves
		// them properly.
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, watch.Folder{Dir: dir})
	}
	add(s.WatchDir)
	for _, dir := range filepath.SplitList(os.Getenv(envWatchDirs)) {
		add(dir)
	}
	return out
}

// applyWatchFolders makes the running watcher match the configuration. It runs
// on every settings change, so turning a folder on does not need a restart.
//
// It takes the whole settings rather than a folder list because where the
// folders come from is watchFolders' business and not the caller's.
//
// The live watcher is reconciled rather than rebuilt. Rebuilding was what the
// single-folder version did, and with more than one folder it is actively
// wrong: saving an unrelated setting would stop and restart every poller, throw
// away what each of them knew about the files being copied into its folder, and
// re-probe shares the save had nothing to do with.
func (a *App) applyWatchFolders(s settings.Settings) {
	folders := watchFolders(s)
	a.wmu.Lock()
	defer a.wmu.Unlock()

	if len(folders) == 0 {
		if a.watcher != nil {
			_ = a.watcher.Close()
			a.watcher = nil
		}
		return
	}
	if a.watcher == nil {
		w, err := watch.New(watch.Options{Folders: folders, OnJob: a.onWatchIntake})
		if err != nil {
			log.Printf("no drop folder could be watched (%v); intake is off", err)
			return
		}
		w.Start()
		a.watcher = w
		log.Printf("watching %s for dropped links", strings.Join(w.Dirs(), ", "))
		return
	}
	for _, err := range a.watcher.Apply(folders) {
		log.Printf("drop folder is not being watched: %v", err)
	}
	dirs := a.watcher.Dirs()
	if len(dirs) == 0 {
		// Every configured folder failed. The watcher is closed rather than kept,
		// so that fixing the permission and saving again builds a fresh one
		// instead of reviving whatever this one was left holding.
		_ = a.watcher.Close()
		a.watcher = nil
		log.Print("no drop folder could be watched; intake is off")
		return
	}
	log.Printf("watching %s for dropped links", strings.Join(dirs, ", "))
}

// onWatchIntake receives one job from a drop folder. It runs on that folder's
// polling goroutine, so the work goes onto a goroutine of its own: a poll that
// waits for the collector to resolve twenty links is a poll that is not looking
// at the folder, and the next file to land sits there until it returns.
func (a *App) onWatchIntake(j watch.Job) {
	go a.stageWatchJob(j)
}

// stageWatchJob carries out one dropped job.
//
// The order is the whole of it: the links are staged, then everything the file
// said about them is written on, and only then is anything started. Starting
// first would race the folder override onto a download that had already chosen
// where to put its bytes.
func (a *App) stageWatchJob(j watch.Job) {
	created := a.AddLinksWithPasswords(j.URLs, j.Package, j.Passwords, OriginWatch)
	if len(created) == 0 {
		return
	}
	ids := make([]string, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}
	a.applyWatchJobOptions(ids, j)

	if j.Disabled {
		// The file asked for these to be parked: added, kept, and passed over by
		// everything that starts downloads. What this cannot undo is a global
		// auto-start that has already dispatched them on the way in - the flag
		// stops the next dispatch, it does not call bytes back.
		a.SetEnabled(ids, false)
		return
	}
	if j.Forced {
		a.SetForced(ids, true)
	}
	// A start the file asked for, over and above the global setting, which has
	// already started them if it was on.
	if (j.AutoStart || j.Forced) && !a.Settings.Get().AutoStart {
		a.StartTasks(ids)
	}
}

// applyWatchJobOptions writes what the job said onto the tasks it created.
//
// The file wins over the Packagizer, deliberately: a rule is a standing
// instruction and the dropped file is somebody saying what they want for these
// links, now. That is also why the values are written after staging rather than
// merged into it - the rules run as a link is staged, and anything applied here
// lands on top of what they decided.
func (a *App) applyWatchJobOptions(ids []string, j watch.Job) {
	var (
		opts TaskOptions
		set  bool
	)
	if j.Dir != "" {
		dir := j.Dir
		opts.Dir, set = &dir, true
	}
	if j.Comment != "" {
		comment := j.Comment
		opts.Comment, set = &comment, true
	}
	if j.Chunks > 0 {
		chunks := j.Chunks
		opts.Chunks, set = &chunks, true
	}
	if j.Priority != nil {
		// Copied rather than passed on: the Job's pointer belongs to the parsed
		// file, and handing it to a setter that may keep it is how two tasks end
		// up sharing one priority.
		priority := *j.Priority
		opts.Priority, set = &priority, true
	}
	if j.Extract != nil {
		extract := *j.Extract
		opts.AutoExtract, set = TriBool{Set: true, Value: &extract}, true
	}
	if set {
		if err := a.SetTaskOptions(ids, opts); err != nil {
			log.Printf("dropped job: %v", err)
		}
	}

	// The file name goes in a call of its own, because SetTaskOptions refuses the
	// whole request when one field in it is bad: a crawljob carrying a name with a
	// slash in it would otherwise cost the destination folder and the priority as
	// well, and nobody is watching to notice.
	//
	// It is applied only to a job carrying a single link. A name is one file's
	// identity, and writing it onto twenty tasks points twenty downloads at one
	// destination.
	if j.Filename != "" && len(ids) == 1 {
		filename := j.Filename
		if err := a.SetTaskOptions(ids, TaskOptions{Filename: &filename}); err != nil {
			log.Printf("dropped job: %v", err)
		}
	}
}
