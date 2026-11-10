package app

import (
	"context"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// startAfterDownload builds the runner and subscribes it to the bus, as
// startMediaHooks does, with the same wait for the files to land.
func (a *App) startAfterDownload() {
	a.AfterDownload = afterdownload.New(afterdownload.Options{
		Fortress: a.joinedFortress,
		Ready:    a.packageFilesLanded,
	})
	a.AfterDownload.Start()
	a.Events.Subscribe("afterdownload", func(f script.Firing) {
		if f.Trigger != script.TriggerPackageDone || f.Package == nil {
			return
		}
		for _, t := range a.afterDownloadTargets(f.Package.Name) {
			a.AfterDownload.Enqueue(t, f.Package.Name)
		}
	})
}

// SetFortress is called once this instance has joined a fortress, and with
// nil once it has left.
func (a *App) SetFortress(f afterdownload.Fortress) {
	a.joinedMu.Lock()
	defer a.joinedMu.Unlock()
	a.joined = f
}

// InFortress reports whether this instance is in a fortress.
func (a *App) InFortress() bool { return a.joinedFortress() != nil }

func (a *App) joinedFortress() afterdownload.Fortress {
	a.joinedMu.Lock()
	defer a.joinedMu.Unlock()
	return a.joined
}

// afterDownloadTargets is every job the drawers of one package's files name, each
// once.
func (a *App) afterDownloadTargets(pkg string) []afterdownload.Target {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return nil
	}
	cfg := a.Settings.Get()
	var out []afterdownload.Target
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		if strings.TrimSpace(t.Package) != pkg {
			continue
		}
		after := cfg.CategoryFor(t.Category).AfterDownload
		if after == nil {
			continue
		}
		target := afterdownload.Target{Member: after.Member, Job: after.Job}
		if !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out
}

// LastAfterDownload is the last hand-over the station of one category made in
// this process.
func (a *App) LastAfterDownload(categoryID string) (afterdownload.Result, bool) {
	after := a.Settings.Get().CategoryFor(categoryID).AfterDownload
	if after == nil || a.AfterDownload == nil {
		return afterdownload.Result{}, false
	}
	return a.AfterDownload.Last(afterdownload.Target{Member: after.Member, Job: after.Job})
}

// AfterDownloadEngines lists the ArrowLoop instances of the fortress with their
// jobs, for the station's picker.
func (a *App) AfterDownloadEngines(ctx context.Context) []afterdownload.Engine {
	return afterdownload.Engines(ctx, a.joinedFortress())
}
