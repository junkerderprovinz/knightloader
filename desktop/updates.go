package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

// launchInstaller starts a downloaded installer; the tests stand in for it.
var launchInstaller = startInstaller

var (
	errUpdating     = errors.New("an update is already under way")
	errNothingNewer = errors.New("there is no newer release to install")
)

// updater keeps the desktop app current. It is the only caller of
// update.Updater in its process, and busy keeps the daily check and the
// Install button from running at the same time.
type updater struct {
	u     *update.Updater
	logf  func(format string, args ...any)
	ready atomic.Pointer[string]
	busy  atomic.Bool
	// progress is what the Updates card shows of an update it asked for.
	progressMu sync.Mutex
	progress   update.Progress
	// announce tells the window which version waits for the next start.
	announce func(version string)
	// swapping is held while the program is being replaced, and by shutdown
	// for good, so the process never exits halfway through a swap.
	swapping sync.Mutex
}

func newUpdater(logf func(format string, args ...any), announce func(version string)) *updater {
	up := &updater{
		logf:     logf,
		announce: announce,
	}
	up.u = &update.Updater{
		Repo:    update.Repo,
		Version: buildinfo.Version,
		Assets:  update.Assets,
		API:     updateAPI,
		// Long enough for the zip on a slow line, short enough that a
		// connection that stalls does not hold up every later check.
		Client:   &http.Client{Timeout: 30 * time.Minute},
		Logf:     logf,
		Progress: up.downloaded,
	}
	return up
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
	os.RemoveAll(installerDir())
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
	if !up.busy.CompareAndSwap(false, true) {
		return
	}
	defer up.busy.Store(false)
	version, err := up.swap(ctx)
	switch {
	case err != nil:
		up.logf("update: %v", err)
	case version == "":
		up.logf("update: no newer release")
	}
}

// swap downloads the newest release and puts it in place for the next start.
// It returns the version it put there, or "" when none is newer.
func (up *updater) swap(ctx context.Context) (string, error) {
	rel, err := up.u.Check(ctx)
	if err != nil || rel == nil {
		return "", err
	}
	up.nameDownload(rel.Version)
	download, err := up.u.Fetch(ctx, rel)
	if err != nil {
		return "", fmt.Errorf("not updating to %s: %w", rel.Version, err)
	}
	defer os.Remove(download)

	up.swapping.Lock()
	err = up.u.Swap(download, rel)
	up.swapping.Unlock()
	if err != nil {
		return "", fmt.Errorf("not updating to %s: %w", rel.Version, err)
	}
	up.logf("update: %s is in place and starts next time", rel.Version)
	up.ready.Store(&rel.Version)
	up.announce(rel.Version)
	return rel.Version, nil
}

// install is the Updates card's Install button: it fetches the newest release
// in the background and reports how far it has come through progress. The
// copy installed for all users cannot replace itself, so it downloads the
// installer and starts it, which asks Windows for permission, closes this
// program and opens the new version. Any other copy puts the new version in
// place for its next start, as the daily check does.
func (up *updater) install(ctx context.Context, installed bool) error {
	if !up.busy.CompareAndSwap(false, true) {
		return errUpdating
	}
	up.setProgress(update.Progress{State: update.Downloading})
	go func() {
		defer up.busy.Store(false)
		ctx, cancel := context.WithTimeout(ctx, time.Hour)
		defer cancel()
		done, err := up.installNow(ctx, installed)
		if err != nil {
			up.logf("update: %v", err)
			up.setProgress(update.Progress{State: update.Failed, Error: err.Error()})
			return
		}
		up.setProgress(done)
	}()
	return nil
}

func (up *updater) installNow(ctx context.Context, installed bool) (update.Progress, error) {
	// A version already waiting needs only a restart.
	if v := up.readyVersion(); v != "" {
		return update.Progress{State: update.Ready, Version: v}, nil
	}
	if !installed {
		v, err := up.swap(ctx)
		if err == nil && v == "" {
			err = errNothingNewer
		}
		return update.Progress{State: update.Ready, Version: v}, err
	}

	dir := installerDir()
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return update.Progress{}, err
	}
	u := *up.u
	u.Assets = update.Installers
	u.Dir = dir
	rel, err := u.Check(ctx)
	if err != nil {
		return update.Progress{}, err
	}
	if rel == nil {
		return update.Progress{}, errNothingNewer
	}
	up.nameDownload(rel.Version)
	file, err := u.Fetch(ctx, rel)
	if err != nil {
		return update.Progress{}, fmt.Errorf("downloading the installer of %s: %w", rel.Version, err)
	}
	up.setProgress(update.Progress{State: update.Starting, Version: rel.Version})
	if err := launchInstaller(file); err != nil {
		return update.Progress{}, fmt.Errorf("starting the installer of %s: %w", rel.Version, err)
	}
	up.logf("update: the installer of %s is running", rel.Version)
	return update.Progress{State: update.Started, Version: rel.Version}, nil
}

// installerDir holds the installer an update starts. The installer closes this
// program, so the folder is cleared on the next start rather than afterwards.
func installerDir() string {
	return filepath.Join(os.TempDir(), product+" update")
}

func (up *updater) setProgress(p update.Progress) {
	up.progressMu.Lock()
	up.progress = p
	up.progressMu.Unlock()
}

// downloaded counts the bytes of a download the card asked for. The daily
// check's own downloads leave the card alone.
func (up *updater) downloaded(done, total int64) {
	up.progressMu.Lock()
	defer up.progressMu.Unlock()
	if up.progress.State == update.Downloading {
		up.progress.Done, up.progress.Total = done, total
	}
}

// nameDownload puts the version in a download the card asked for.
func (up *updater) nameDownload(version string) {
	up.progressMu.Lock()
	defer up.progressMu.Unlock()
	if up.progress.State == update.Downloading {
		up.progress.Version = version
	}
}

// currentProgress is what GET /api/system/update-install answers.
func (up *updater) currentProgress() update.Progress {
	up.progressMu.Lock()
	defer up.progressMu.Unlock()
	return up.progress
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
