package app

// Media library hooks subscribe to the event bus rather than being called from
// every site that fires. Delivery is synchronous on the publisher's goroutine,
// so the subscriber only walks the task map and enqueues; the HTTP call happens
// later on mediahook.Runner's own goroutine.
//
// script.PackageView carries no folder or category, so the addresses to call
// are looked up in a.tasks. Adding fields to PackageView would change the "pkg"
// global that user scripts already rely on.

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

// stopMediaHooks is Close's half. An App assembled by hand in a test may never
// have run startMediaHooks.
func (a *App) stopMediaHooks() {
	if a.MediaHooks != nil {
		_ = a.MediaHooks.Close()
	}
}

// MediaHookStore is the sealed store the header values live in. A fresh wrapper
// per call is safe because mediahook.Store holds only the shared
// *accounts.Store; a second accounts.Store over the same file would overwrite
// the first one's writes.
func (a *App) MediaHookStore() *mediahook.Store { return mediahook.NewStore(a.Accounts) }

// TestMediaHook calls one address on demand and reports what came back. It goes
// through the runner's own client and the same Call a real firing makes, so the
// test proves something about the real request.
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
// and sorted. A package can span several categories (the sample beside the
// film), and each category's address gets one call. Sorting keeps the call order
// stable across runs.
func (a *App) hookIDsForPackage(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		// An empty name would match every unpackaged task.
		return nil
	}
	cfg := a.Settings.Get()
	if len(cfg.MediaHooks) == 0 {
		// Checked before taking a.mu, since this runs on the package sweep's
		// goroutine on every install.
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

// packageFilesLanded reports whether this package's finished files have left the
// working folder for their destination.
//
// A task turns Done before deliverDownload moves the file, and for a large file
// across filesystems that move takes minutes. package.done fires on the next
// sweep tick, so a media server told to scan at that moment would find nothing.
// The event keeps its published meaning for scripts; this caller waits instead.
//
// The check mirrors deliverDownload's own conditions and looks for the file in
// the working folder, so no marker can be left set by an early return. The Lstat
// runs outside a.mu, which every progress update needs.
func (a *App) packageFilesLanded(name string) bool {
	if a.workRoot() == "" {
		// No working folder, so nothing is ever moved.
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
		// The same refusals deliverDownload makes, in the same order.
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
