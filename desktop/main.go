// Command desktop is KnightLoader's native desktop app: the same server running
// inside a Wails webview window, with its HTTP handler as the asset handler, so
// the UI and the REST and WebSocket API match the container build.
//
// It is a separate Go module so the Wails toolchain never touches the server
// build; .github/workflows/desktop.yml builds it per platform.
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
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/keepawake"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/provision"
	"github.com/junkerderprovinz/knightloader/internal/update"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	// Must be set before app.New; the default is "container".
	buildinfo.Deployment = "desktop"
	// The desktop opens no listener, so it never announces, but it still
	// listens for servers on its network.
	buildinfo.DiscoveryEnabled = true

	dataDir := dataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// A private headless JDownloader gives full hoster coverage out of the box.
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

	// The browser whose Click'n'Load buttons post to 127.0.0.1 runs on this
	// machine, so the desktop build listens the way the server does, KL_CNL
	// included.
	a.CnL = cnl.Listen(a)

	// Outside app.New because every test calls the constructor and this spawns
	// four processes. KL_STARTUP_CHECK=0 turns it off, as on the server.
	if os.Getenv("KL_STARTUP_CHECK") != "0" {
		a.StartStartupCheck()
	} else {
		a.MarkStartupCheckOff()
	}

	// Window and tray preferences stay out of settings.Settings, which every
	// connected browser reads and writes; see config.go.
	tc := newTrayController(a.Hub, filepath.Join(dataDir, "desktop.json"))

	// RequestExit stays nil here because the window and tray already shut
	// down through a.Close. Updating swaps the binary, starts the new one and
	// then quits through tc.quit, the path the tray's Quit item uses.
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
			// Apply already swapped the files. The install path is the
			// runnable on Windows and Linux; only a macOS bundle differs.
			newRunnable = installPath
		}
		// The new instance binds the Click'n'Load port as soon as it starts,
		// and this one would hold it until its window has been torn down.
		wasListening := a.CnL.Port() > 0
		a.CnL.Stop()
		if err := update.Relaunch(newRunnable, os.Args[1:]); err != nil {
			if wasListening {
				_ = a.CnL.Start()
			}
			return err
		}
		// Quit asynchronously so the HTTP response reaches the browser first.
		go tc.quit()
		return nil
	}

	// Only the desktop can put the machine to sleep; internal/idleaction offers
	// the action when this is set. See power.go.
	a.RequestSuspend = requestSuspend

	// The other half of power: while a download or the work after it runs and
	// the setting is on, the machine stays up. See awake_*.go for how each OS
	// is asked.
	awake := keepawake.New(keepawake.Options{
		Busy:    a.Working,
		Enabled: func() bool { return a.Settings.Get().KeepAwake },
		Hold:    preventSleep,
	})
	awake.Start()

	// Wails and systray both want the main thread on macOS and systray.Run
	// blocks, so the tray runs in a goroutine started before wails.Run. It is
	// not tracked by tc.spawn: onShutdown waits on that group before calling
	// systray.Quit, which is what ends this goroutine.
	if tc.isTrayAvailable() {
		go runTray(tc)
	}

	// Exposed to the frontend as window.go.main.DesktopFiles for reveal in
	// folder and open natively; see files.go.
	desktopFiles := newDesktopFiles(a)

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
		// With true, Wails v2.13.0 skips OnBeforeClose on Windows and always
		// hides. The hooks below decide from the live preference instead, so
		// a change in the tray menu applies without a restart.
		HideWindowOnClose: false,
		OnStartup:         tc.onWailsStartup,
		OnBeforeClose:     tc.onBeforeClose,
		OnShutdown: func(context.Context) {
			tc.onShutdown()
			a.CnL.Stop()
			// Before a.Close, whose task list the guard reads.
			_ = awake.Close()
			_ = a.Close()
			// After a.Close so the shutdown's own records reach the file.
			// Writes are unbuffered; closing releases the Windows handle.
			_ = logring.CloseFile()
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
