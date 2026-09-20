package app

// Drop folders and the jobs dropped into them. Nobody is at the collector for
// this intake, so every question it would ask has to be answered from the file
// itself.

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// envWatchDirs names drop folders beyond the one in the settings, separated as
// the platform separates a path list.
//
// It is an environment variable rather than a setting because the settings
// form replaces the whole object on save, so a list no page renders would be
// wiped by the first unrelated save.
const envWatchDirs = "KL_WATCH_DIRS"

// watchFolders is the set of drop folders the configuration asks for, combined
// from the settings and the environment in one place.
func watchFolders(s settings.Settings) []watch.Folder {
	var out []watch.Folder
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		// A textual match only, so a repeated entry is not logged twice. The
		// watcher resolves two spellings of one directory itself.
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
// on every settings change, so turning a folder on needs no restart.
//
// The watcher is reconciled rather than rebuilt, so an unrelated save does not
// restart every poller and lose what each knew about files still being copied.
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
		// Every folder failed. Closing it means the next save builds a fresh
		// watcher.
		_ = a.watcher.Close()
		a.watcher = nil
		log.Print("no drop folder could be watched; intake is off")
		return
	}
	log.Printf("watching %s for dropped links", strings.Join(dirs, ", "))
}

// onWatchIntake receives one job on the folder's polling goroutine and hands it
// off, so the poll is not blocked while the links resolve.
func (a *App) onWatchIntake(j watch.Job) {
	a.spawn(func() { a.stageWatchJob(j) })
}

// stageWatchJob carries out one dropped job: stage the links, write on what the
// file asked for, and only then start anything, so a folder override cannot
// arrive after a download has chosen where to write.
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
		// Parked: added and kept, and never passed to ConfirmTasks or
		// StartTasks.
		a.SetEnabled(ids, false)
		return
	}
	if j.Forced {
		a.SetForced(ids, true)
	}
	// addLinksFrom applies no AutoConfirm of its own, so it is checked here.
	// A forced link bypasses the confirm policy, since the file asked for it
	// explicitly.
	switch {
	case j.Forced:
		a.StartTasks(ids)
	case a.Settings.Get().AutoConfirm || j.AutoStart:
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerWatch)
	}
}

// applyWatchJobOptions writes what the job said onto the tasks it created. It
// runs after staging so the file's values override the Packagizer: the file is
// a request for these links, a rule a standing default.
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
		// Copied so no task shares the parsed job's pointer.
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

	// SetTaskOptions rejects the whole request over one bad field, so a bad file
	// name gets its own call and cannot cost the folder and priority. It only
	// applies to a single-link job; one name on twenty tasks would point them all
	// at one file.
	if j.Filename != "" && len(ids) == 1 {
		filename := j.Filename
		if err := a.SetTaskOptions(ids, TaskOptions{Filename: &filename}); err != nil {
			log.Printf("dropped job: %v", err)
		}
	}
}
