package main

import (
	"context"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/update"
)

// The first check waits for the window and JDownloader to settle; one a day
// follows. An installed copy looks every followEvery for a version the
// scheduled task has put in place.
var (
	updateAPI   = "https://api.github.com"
	firstCheck  = time.Minute
	followEvery = 10 * time.Minute
)

const checkEvery = 24 * time.Hour

// updateReadyEvent carries the version that waits for the next start, so an
// open window can say so.
const updateReadyEvent = "updateReady"

// updater keeps the desktop app current. It is the only caller of
// update.Updater in its process, so there is never more than one update in
// flight.
type updater struct {
	u     *update.Updater
	logf  func(format string, args ...any)
	ready atomic.Pointer[string]
	// announce tells the window which version waits for the next start.
	announce func(version string)
	// swapping is held while the program is being replaced, and by shutdown
	// for good, so the process never exits halfway through a swap.
	swapping sync.Mutex
}

func newUpdater(logf func(format string, args ...any), announce func(version string)) *updater {
	return &updater{
		logf:     logf,
		announce: announce,
		u: &update.Updater{
			Repo:    update.Repo,
			Version: buildinfo.Version,
			Assets:  update.Assets,
			API:     updateAPI,
			// Long enough for the zip on a slow line, short enough that a
			// connection that stalls does not hold up every later check.
			Client: &http.Client{Timeout: 30 * time.Minute},
			Logf:   logf,
		},
	}
}

// run removes what the last update left and then checks for updates until ctx
// ends, whenever autoUpdate allows. A dev build never checks, since it has no
// version to compare.
func (up *updater) run(ctx context.Context, autoUpdate func() bool) {
	up.u.Cleanup()
	if buildinfo.Version == "dev" {
		return
	}
	timer := time.NewTimer(firstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if autoUpdate() {
			up.once(ctx)
		}
		timer.Reset(checkEvery)
	}
}

// follow is run for the installed copy, which cannot replace itself. It waits
// for the scheduled task to record a newer version in the uninstall entry and
// says once that it starts next time.
func (up *updater) follow(ctx context.Context) {
	tick := time.NewTicker(followEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if v := update.InstalledVersion(product); update.Newer(v, buildinfo.Version) {
			up.ready.Store(&v)
			up.announce(v)
			return
		}
	}
}

func (up *updater) once(ctx context.Context) {
	rel, err := up.u.Check(ctx)
	if err != nil {
		up.logf("update: %v", err)
		return
	}
	if rel == nil {
		up.logf("update: no newer release")
		return
	}
	download, err := up.u.Fetch(ctx, rel)
	if err != nil {
		up.logf("update: not updating to %s: %v", rel.Version, err)
		return
	}
	defer os.Remove(download)

	up.swapping.Lock()
	err = up.u.Swap(download, rel)
	up.swapping.Unlock()
	if err != nil {
		up.logf("update: not updating to %s: %v", rel.Version, err)
		return
	}
	up.logf("update: %s is in place and starts next time", rel.Version)
	up.ready.Store(&rel.Version)
	up.announce(rel.Version)
}

// readyVersion is the version waiting for the next start, or "".
func (up *updater) readyVersion() string {
	if v := up.ready.Load(); v != nil {
		return *v
	}
	return ""
}

// stop takes the swap lock and keeps it, so a swap under way finishes and none
// starts while the program exits.
func (up *updater) stop() {
	up.swapping.Lock()
}
