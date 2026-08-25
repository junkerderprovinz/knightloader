// Command desktop is KnightLoader's native desktop app: the same server (engine,
// resolvers, API, embedded UI) running inside a Wails webview window, plus
// provision-on-first-run of a private headless JDownloader for full hoster
// coverage. It reuses the server's HTTP handler as the Wails asset handler, so
// the UI and the entire REST/WebSocket API are identical to the container build.
//
// This module is built per-platform in CI (see .github/workflows/desktop.yml);
// `wails build` produces the Windows/macOS/Linux bundles. It is intentionally a
// separate Go module so the Wails toolchain never touches the server build.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/provision"
	"github.com/junkerderprovinz/knightloader/internal/update"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	// buildinfo.Deployment defaults to "container" - correct for
	// cmd/knightloader, wrong here. Set before app.New below, per that
	// var's own doc comment ("whichever one constructs the App sets this
	// before serving a single request") - GET /api/system/deployment and
	// the Diagnostics page both read it, and both exist specifically so a
	// user can tell which build they are running.
	buildinfo.Deployment = "desktop"
	// The desktop opens no listener, so it never announces - but it still
	// LISTENS, so it can find the server on its own network and add it.
	buildinfo.DiscoveryEnabled = true

	dataDir := dataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// Provision a private headless JDownloader on first run so the desktop app
	// has full hoster coverage out of the box (no JD UI ever shown).
	if os.Getenv("KL_JD") == "" {
		pv := provision.New(filepath.Join(dataDir, "jd"))
		log.Printf("provisioning headless JDownloader (first run may take a few minutes)…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		if _, url, err := pv.Ensure(ctx); err != nil {
			log.Printf("JD provisioning failed (%v); continuing without JD", err)
		} else {
			_ = os.Setenv("KL_JD", url)
		}
		cancel()
	}

	a, err := app.New(dataDir)
	if err != nil {
		log.Fatalf("start: %v", err)
	}

	// Desktop-local window/tray preferences: never settings.Settings, which
	// is served whole to every browser connected to this same server and
	// would let a phone on the LAN decide whether this one installation's
	// window starts hidden. See config.go's doc comment.
	tc := newTrayController(a.Hub, filepath.Join(dataDir, "desktop.json"))

	// Wired here, not left nil like RequestExit above: RequestExit stays
	// unset on desktop because window/tray already own a graceful path to
	// a.Close() with no need of it (see its own doc comment on App), but
	// self-updating needs a NEW capability neither of those existing paths
	// has - swap the running binary for a newer one, spawn it, THEN exit -
	// so it gets its own field rather than overloading RequestExit's
	// existing, unrelated meaning. update.Download/Apply/Relaunch (all
	// deployment-agnostic, independently tested) do the actual work; this
	// closure only supplies what only desktop/main.go knows: this process's
	// own executable, and tc.quit() - the exact same shutdown path the
	// tray's own Quit menu item already uses, verified to reach
	// OnShutdown -> a.Close() correctly regardless of the live
	// close-to-tray preference.
	a.RequestUpdateInstall = func(ctx context.Context) error {
		zipPath, _, err := update.Download(ctx, buildinfo.Version)
		if err != nil {
			return err
		}
		installPath, _, err := update.CurrentExecutable()
		if err != nil {
			os.Remove(zipPath)
			return err
		}
		if err := update.Apply(zipPath, installPath); err != nil {
			return err
		}
		_, newRunnable, err := update.CurrentExecutable()
		if err != nil {
			// Apply already swapped the files - report the resolve
			// failure but fall back to the path we swapped in at, which is
			// correct on Windows/Linux (install path IS the runnable) and
			// only wrong on the macOS bundle case this error path implies
			// something already unexpected about.
			newRunnable = installPath
		}
		if err := update.Relaunch(newRunnable, os.Args[1:]); err != nil {
			return err
		}
		// The HTTP response for this request still needs to reach the
		// browser before the process tears down - same reasoning as
		// requestExit's own "shutting down" response racing the actual
		// exit, which routes_lifecycle.go's own comment already accepts as
		// expected. Async so this closure (and the HTTP handler awaiting
		// it) returns first.
		go tc.quit()
		return nil
	}

	// Both Wails and the tray library want the real OS main thread on macOS,
	// and systray.Run blocks in its own native loop until Quit() - so it is
	// started in a goroutine before wails.Run, the established community
	// pattern for this exact combination (see tray.go's package doc for the
	// research this rests on). Never started at all when the probe already
	// found no tray host: an icon nothing can show is not worth the log
	// noise, and it keeps "close/minimize to tray" unreachable by
	// construction rather than by a runtime check that could be missed.
	//
	// Deliberately a raw goroutine, not tc.spawn: tc.onShutdown calls
	// tc.wg.Wait() before calling systray.Quit(), and this goroutine only
	// returns once systray.Quit() is called - tracking it in the same
	// WaitGroup that gates that same call would deadlock shutdown.
	if tc.isTrayAvailable() {
		go runTray(tc)
	}

	// Bound so the frontend can reach reveal-in-folder and open-natively
	// (files.go): package 20's two desktop-only actions, reachable from the
	// frontend as window.go.main.DesktopFiles.*. The container/browser build
	// never registers this type at all, which is what makes those two
	// buttons refuse with a stated reason there instead of doing nothing -
	// see files.go's package doc.
	desktopFiles := newDesktopFiles(a)

	// The server's HTTP handler serves the SPA, REST and WebSocket; Wails runs
	// it as the in-window asset handler.
	err = wails.Run(&options.App{
		Title:            "KnightLoader",
		Width:            1100,
		Height:           780,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 22, G: 22, B: 22, A: 1},
		AssetServer:      &assetserver.Options{Handler: api.Handler(a)},
		Bind:             []interface{}{desktopFiles},
		StartHidden:      tc.effectiveStartHidden(),
		// Left false deliberately: the Windows, macOS and Linux frontends
		// all route the native close signal through OnBeforeClose only when
		// this is false (verified against v2.13.0's per-platform frontend
		// sources - true skips OnBeforeClose entirely on Windows and just
		// hides unconditionally). Everything close/minimize/tray do is
		// decided dynamically inside the hooks below instead, from the live
		// preference, so a change from the tray menu takes effect without a
		// restart.
		HideWindowOnClose: false,
		OnStartup:         tc.onWailsStartup,
		OnBeforeClose:     tc.onBeforeClose,
		OnShutdown: func(context.Context) {
			tc.onShutdown()
			_ = a.Close()
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func dataDir() string {
	if v := os.Getenv("KL_DATA"); v != "" {
		return v
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "KnightLoader")
	}
	return "kl-data"
}
