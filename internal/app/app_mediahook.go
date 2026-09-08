package app

// The second subscriber the event bus was built for, and the two questions only
// this package can answer for it.
//
// internal/script/bus.go named this feature by name as the next consumer - "a
// media library told to rescan" - and said in the same breath that adding it
// must be a Subscribe call rather than an edit to every site that fires. This
// file is that Subscribe call. Nothing in internal/mediahook knows what a task,
// a package or a category is; nothing in internal/app knows what an HTTP call
// is. What crosses the line is a hook id and a package name.
//
// THE SUBSCRIBER MUST RETURN PROMPTLY, and Subscribe says so in capitals:
// delivery is synchronous on the publisher's own goroutine, which for
// package.done is watchPackagesForScripts' ticker. So the subscriber below does
// one walk of the task map and hands the result to a bounded channel. The
// network call happens in mediahook.Runner's own loop, minutes later, on its own
// goroutine.
//
// # The two questions
//
// WHICH ADDRESSES DOES THIS PACKAGE CALL. script.PackageView carries a name and
// five counts and nothing else - no folder, no category, no rule - so the answer
// cannot come out of the Firing and has to come back to a.tasks. That is not an
// oversight in PackageView: it is the "pkg" global in the script sandbox and its
// doc comment enumerates its whole surface, so adding a field there to save this
// walk would change a published API for every script anybody has written.
//
// HAVE THE FILES ACTUALLY LANDED. See packageFilesLanded, which is the whole
// reason this feature is not four lines.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// startMediaHooks builds the runner and subscribes it to the bus. Called from
// New once a.Accounts, a.Settings and a.Events all exist.
func (a *App) startMediaHooks() {
	a.MediaHooks = mediahook.New(mediahook.Options{
		Store: a.MediaHookStore(),
		// A function and not a snapshot: a coalesce window is up to an hour
		// long, and an address edited during one has to be called as it is now.
		Hooks: func() []mediahook.Hook { return a.Settings.Get().MediaHooks },
		Ready: a.packageFilesLanded,
	})
	a.MediaHooks.Start()
	a.Events.Subscribe("mediahook", func(f script.Firing) {
		if f.Trigger != script.TriggerPackageDone || f.Package == nil {
			return
		}
		name := f.Package.Name
		for _, id := range a.hookIDsForPackage(name) {
			a.MediaHooks.Enqueue(id, name)
		}
	})
}

// stopMediaHooks is Close's half. Nil-guarded because an App assembled by hand in
// a test may never have run startMediaHooks.
func (a *App) stopMediaHooks() {
	if a.MediaHooks != nil {
		_ = a.MediaHooks.Close()
	}
}

// MediaHookStore is the sealed store the header values live in.
//
// A FRESH WRAPPER EVERY TIME, and that is safe here where it would not be for
// header profiles. hostheaders.Store keeps an origin index that only a write
// through the same instance invalidates, so a second one there would seal a
// profile the live resolver never sees; mediahook.Store holds nothing but the
// *accounts.Store pointer and has nothing to go stale. What must NOT happen is a
// second accounts.Store over the same accounts.json - each would hold its own
// snapshot of the whole file and the second to write would erase the first - and
// that is exactly what handing a.Accounts on avoids.
func (a *App) MediaHookStore() *mediahook.Store { return mediahook.NewStore(a.Accounts) }

// TestMediaHook calls one address on purpose and reports what came back. It is
// the Test button's whole implementation, and it goes out through the runner's
// own client and the same Call a real firing makes - a test that followed
// redirects, used another client or skipped the header would prove nothing about
// the thing it is testing.
//
// A runner that was never started (an App assembled in a test) still answers,
// with a client of its own, so the route has one code path.
func (a *App) TestMediaHook(ctx context.Context, h mediahook.Hook) mediahook.Result {
	if a.MediaHooks == nil {
		res := mediahook.Call(ctx, nil, h, a.MediaHookStore().Value(h.ID))
		res.Test = true
		return res
	}
	return a.MediaHooks.CallNow(ctx, h)
}

// LastMediaHookCall is the last call this process made for one address, test
// calls included, and false when it has made none.
func (a *App) LastMediaHookCall(id string) (mediahook.Result, bool) {
	if a.MediaHooks == nil {
		return mediahook.Result{}, false
	}
	return a.MediaHooks.Last(id)
}

// hookIDsForPackage is every address the files of one package point at, distinct
// and sorted.
//
// A SET, because a package whose links belong in different drawers is normal and
// settings_categories.go argues for it at length: the sample beside the film, the
// subtitle beside the episode. Two drawers with two different addresses is
// therefore two calls, one each, and a first-task-wins implementation would pick
// the sample's drawer about half the time and read as random.
//
// Sorted for the same reason watchPackagesForScripts sorts what it fires: map
// order is random, and two addresses called in a different order on every run is
// the kind of nondeterminism that makes an intermittent report impossible to
// reproduce.
func (a *App) hookIDsForPackage(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		// No package is not a package. scriptPackageTallies already skips these,
		// so this is unreachable through the bus and is here for the direct
		// callers a test can be: an empty name would otherwise match every task
		// whose Package is empty and call an address once per unpackaged
		// download.
		return nil
	}
	cfg := a.Settings.Get()
	if len(cfg.MediaHooks) == 0 {
		// The whole feature switched off, which is every install until somebody
		// stores an address. Answered before the lock is taken: this runs on the
		// package sweep's goroutine, twice a second's worth of other work behind
		// it.
		return nil
	}
	seen := map[string]bool{}
	a.mu.Lock()
	for _, t := range a.tasks {
		if strings.TrimSpace(t.Package) != name {
			continue
		}
		if id := cfg.NotifyHookFor(t.Category); id != "" {
			seen[id] = true
		}
	}
	a.mu.Unlock()
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// packageFilesLanded answers whether this package's finished files have left the
// working folder for the folder they actually belong in.
//
// THIS IS THE ONE THAT MAKES THE FEATURE WORK AT ALL, and it is not obvious from
// anywhere else in the tree. app_dispatch.go sets a task's status to Done under
// a.mu and THEN spawns the checksum and the delivery; app_deliver.go's
// deliverDownload is what moves the finished file out of the working folder, and
// for a 40 GB film across a filesystem boundary that is minutes. packageTaskPending
// answers "not pending" the instant the status is Done, so package.done fires on
// the sweep's next 2-second tick with the file still in the working folder. A
// media server told to scan at that moment finds nothing and never looks again -
// and it bites exactly the installs that configured a working folder BECAUSE a
// scanner was picking up half-written files.
//
// It is asked here rather than solved by making package.done itself wait, and
// that is a deliberate choice between two correct answers. Making the sweep wait
// would change what package.done MEANS for every script anybody has already
// written against it - a published trigger with its own documented definition of
// "nothing left to wait for" - to fix a problem only this feature has. So the
// event keeps its meaning and the caller that cares does the waiting.
//
// THE TEST IS THE MOVER'S OWN PREDICATE, not a flag set beside it. deliverDownload
// moves filepath.Join(workDirFor(t), t.Name) and does nothing at all when the
// destination and the working folder are the same, when the task is not one this
// app can deliver, or when the file is not there. So "is that file still there"
// is the same question the mover asks, answered from the same three pieces - and
// unlike a flag it cannot be left set by a path that returns early, which is
// exactly how a "still delivering" marker would silently stop every call on the
// box for good.
//
// The Lstat happens OFF a.mu. It is a filesystem call, the lock it would
// otherwise hold is the one every download's progress update needs, and the paths
// it needs are decided under the lock in one pass.
func (a *App) packageFilesLanded(name string) bool {
	if a.workRoot() == "" {
		// Nothing is ever moved on this install: the bytes were written straight
		// into the folder they belong in, so there was never anything to wait
		// for. This is the majority of installs and it costs one string read.
		return true
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	var pending []string
	a.mu.Lock()
	for _, t := range a.tasks {
		if strings.TrimSpace(t.Package) != name || t.Status != core.StatusDone {
			continue
		}
		// The same three refusals deliverDownload makes, in the same order. A
		// task it will not move is a task with nothing to wait for.
		if !deliverable(t) || t.Name == "" || t.Name == t.URL {
			continue
		}
		dest, work := a.dirFor(t), a.workDirFor(t)
		if dest == work {
			continue
		}
		pending = append(pending, filepath.Join(work, t.Name))
	}
	a.mu.Unlock()
	for _, src := range pending {
		if _, err := os.Lstat(src); err == nil {
			return false
		}
	}
	return true
}
