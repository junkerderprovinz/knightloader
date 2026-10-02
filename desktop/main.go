// Command desktop is KnightLoader's native desktop app: the same server running
// inside a Wails webview window, with its HTTP handler as the asset handler, so
// the UI and the REST API match the container build. The live stream the
// browser gets from /api/ws comes over Wails events instead; see stream.go.
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

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/junkerderprovinz/knightloader/internal/api"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
	"github.com/junkerderprovinz/knightloader/internal/keepawake"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

func main() {
	// The installer's scheduled task starts the program this way as the system
	// account. It must not provision JDownloader, listen for Click'n'Load or
	// open a window, so it runs before any of that.
	if len(os.Args) == 2 && os.Args[1] == "--update" {
		if err := updateInstalled(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Must be set before app.New; the default is "container".
	buildinfo.Deployment = "desktop"
	// The desktop opens no listener, so it never announces, but it still
	// listens for servers on its network.
	buildinfo.DiscoveryEnabled = true

	dataDir := dataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	a, err := app.New(dataDir)
	if err != nil {
		log.Fatalf("start: %v", err)
	}

	// A private headless JDownloader gives full hoster coverage out of the box.
	// Its first run downloads and updates JD for minutes, so it comes up in the
	// background and joins once it answers, and the window does not wait.
	var pv *provision.Provisioner
	jdCtx, stopJD := context.WithCancel(context.Background())
	jdDone := make(chan struct{})
	if os.Getenv("KL_JD") == "" {
		pv = provision.New(filepath.Join(dataDir, "jd"))
		go func() {
			defer close(jdDone)
			provisionJD(jdCtx, a, pv)
		}()
	} else {
		close(jdDone)
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
	// yt-dlp on the desktop is whatever PATH or KL_YTDLP offers, or a copy
	// fetched into the data folder, so the daily update keeps it current here
	// as it does in the container.
	a.StartYtdlpAutoUpdate()

	// Window and tray preferences stay out of settings.Settings, which every
	// connected browser reads and writes; see config.go.
	tray := newTray(a, filepath.Join(dataDir, "desktop.json"))

	// RequestExit stays nil here because the window and tray already shut
	// down through a.Close.

	// A newer release is downloaded in the background and starts next time;
	// see updates.go. The copy installed for all users leaves that to the
	// installer's scheduled task, which reads the switch from ProgramData, and
	// only passes on the news.
	installed := isInstalled()
	if installed {
		if dir, err := machineData(); err == nil {
			a.Settings.KeepAutoUpdateIn(filepath.Join(dir, machineSettingsFile))
		}
	}
	up := newUpdater(log.Printf, func(version string) { tray.emit(updateReadyEvent, version) })
	a.UpdateReady = up.readyVersion
	updateCtx, stopUpdates := context.WithCancel(context.Background())
	if installed {
		go up.follow(updateCtx)
	} else {
		go up.run(updateCtx, func() bool { return a.Settings.Get().AutoUpdate })
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

	// The page calls these as main.DesktopFiles, main.HubBridge and main.Tray;
	// see web/src/lib/desktop.ts.
	desktopFiles := newDesktopFiles(a)
	hubBridge := newHubBridge(a, tray.emitTo)

	wails := application.New(application.Options{
		Name:        "KnightLoader",
		Description: "Self-hosted, cross-platform download manager",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(desktopFiles),
			application.NewService(hubBridge),
			application.NewService(tray),
		},
		// The API is the whole asset server, the interface included, so the
		// window opens no port of its own.
		Assets: application.AssetOptions{Handler: api.Handler(a)},
		// The close button decides alone whether the program ends; see
		// Tray.closing.
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: "KnightLoader"},
	})
	// MinWidth leaves the page the 768px that keep it out of the phone layout
	// (web/src/lib/phoneLayout.ts), since the settings here have no row for its
	// bottom bar. On Windows the minimum also counts the 8px frame on either side.
	window := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "KnightLoader",
		Width:            1100,
		Height:           780,
		MinWidth:         784,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(22, 22, 22),
		URL:              "/",
		Hidden:           tray.effectiveStartHidden(),
	})
	tray.attach(wails, window)

	// Wails reads the clipboard on the main thread, so the watch starts once
	// the app runs.
	clip := newClipWatch(a, wails.Clipboard.Text, func(o clipOutcome) { tray.emitTo("main", clipWatchEvent, o) })
	a.WatchesClipboard = clip.watching
	clipCtx, stopClip := context.WithCancel(context.Background())
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go clip.run(clipCtx)
	})

	wails.OnShutdown(func() {
		stopClip()
		clip.stop()
		hubBridge.stop()
		stopUpdates()
		up.stop()
		tray.onShutdown()
		a.CnL.Stop()
		// Before a.Close, whose task list the guard reads.
		_ = awake.Close()
		_ = a.Close()
		// JD would otherwise keep running without the program that started
		// it and hold its port against the next start.
		stopJD()
		<-jdDone
		if pv != nil {
			_ = pv.Stop()
		}
		// After a.Close so the shutdown's own records reach the file.
		// Writes are unbuffered; closing releases the Windows handle.
		_ = logring.CloseFile()
	})
	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}

func provisionJD(ctx context.Context, a *app.App, pv *provision.Provisioner) {
	log.Printf("provisioning headless JDownloader (first run may take a few minutes)…")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	_, url, err := pv.Ensure(ctx)
	if err != nil {
		log.Printf("JD provisioning failed (%v); continuing without JD", err)
		return
	}
	a.UseJD(url)
	log.Printf("headless JDownloader provisioned at %s", url)
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
