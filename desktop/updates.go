package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/update"
)

// The first check waits for the window and JDownloader to settle; one a day
// follows.
var (
	updateAPI  = "https://api.github.com"
	firstCheck = time.Minute
)

const checkEvery = 24 * time.Hour

// updateReadyEvent carries the version that waits for the next start, so an
// open window can say so.
const updateReadyEvent = "updateReady"

// updater keeps the desktop app current. It is the only caller of
// update.Updater, so there is never more than one update in flight.
type updater struct {
	u     *update.Updater
	a     *app.App
	ready atomic.Pointer[string]
	// announce tells the window which version waits for the next start.
	announce func(version string)
	// swapping is held while the program is being replaced, and by shutdown
	// for good, so the process never exits halfway through a swap.
	swapping sync.Mutex
}

func newUpdater(a *app.App, announce func(version string)) *updater {
	return &updater{
		a:        a,
		announce: announce,
		u: &update.Updater{
			Repo:    update.Repo,
			Version: buildinfo.Version,
			Assets:  update.Assets,
			// Wails names the installer's entry after the company and the
			// product, and wails.json leaves the company to default to the
			// product.
			UninstallKey: "KnightLoaderKnightLoader",
			API:          updateAPI,
			// Long enough for the zip on a slow line, short enough that a
			// connection that stalls does not hold up every later check.
			Client: &http.Client{Timeout: 30 * time.Minute},
			Logf:   log.Printf,
		},
	}
}

// run removes what the last update left and then checks for updates until ctx
// ends. A dev build never checks, since it has no version to compare.
func (up *updater) run(ctx context.Context) {
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
		if up.a.Settings.Get().AutoUpdate {
			up.once(ctx)
		}
		timer.Reset(checkEvery)
	}
}

func (up *updater) once(ctx context.Context) {
	rel, err := up.u.Check(ctx)
	if err != nil {
		log.Printf("update: %v", err)
		return
	}
	if rel == nil {
		log.Printf("update: no newer release")
		return
	}
	download, err := up.u.Fetch(ctx, rel)
	if err != nil {
		log.Printf("update: not updating to %s: %v", rel.Version, err)
		return
	}
	defer os.Remove(download)

	up.swapping.Lock()
	err = up.u.Swap(download, rel)
	up.swapping.Unlock()
	if err != nil {
		log.Printf("update: not updating to %s: %v", rel.Version, err)
		return
	}
	log.Printf("update: %s is in place and starts next time", rel.Version)
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
