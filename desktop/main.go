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
	"github.com/junkerderprovinz/knightloader/internal/provision"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
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
		OnShutdown:       func(context.Context) { _ = a.Close() },
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
