# KnightLoader — desktop

The native desktop build (Windows / macOS / Linux), packaged with
[Wails](https://wails.io). It runs the exact same server as the container build
— engine, resolvers, REST + WebSocket API, embedded UI — inside a native
webview window, and provisions a private headless JDownloader on first run so
hoster coverage works out of the box (JD's own UI is never shown).

This is a **separate Go module** on purpose: the Wails toolchain and its
per-platform native dependencies never touch the server module, which stays a
clean, pure-Go build.

## Building

Desktop bundles are built **per platform in CI** (`.github/workflows/desktop.yml`)
because each target needs its own toolchain (WebView2 on Windows, Cocoa on
macOS, GTK/WebKit2GTK on Linux) and signing.

Locally, with the [Wails CLI](https://wails.io/docs/gettingstarted/installation)
and a JDK-free Go toolchain:

```sh
cd desktop
wails build          # bundle for the current OS → build/bin/
wails dev            # live-reload dev run
```

## How it fits together

- `main.go` boots `app.New`, provisions JD if `KL_JD` is unset, then calls
  `wails.Run` with the server's `api.Handler` as the Wails **AssetServer
  handler** — so the SPA, `/api/*` and `/api/ws` are served in-window,
  identical to the browser build.
- The frontend is the shared `../web` project (Carbon UI).

## Tray and window behaviour

`tray.go` (plus `config.go`, `tray_probe_*.go` and the embedded icon in
`assets.go`) adds a system tray icon, a tray menu and the window's
close/minimize/start-hidden behaviour, all configured from the tray menu
itself rather than a settings page: these are preferences for one
installation on one machine, saved to `desktop.json` next to the rest of
this build's data directory (`KL_DATA`, or the OS config directory), and
never sent to `settings.Settings`, which every connected browser shares.

At startup the app probes whether a tray icon can actually appear before
offering any tray-dependent behaviour:

- **Windows and macOS** always have a notification area / menu bar, so the
  probe is a formality.
- **Linux** does not: GNOME ships no tray host without a shell extension
  (look for "AppIndicator and KStatusNotifierItem Support" in the GNOME
  Extensions app), and some window manager setups have none at all. The
  probe checks the exact D-Bus name (`org.kde.StatusNotifierWatcher`) the
  tray library itself needs, so it fails accurately rather than guessing.

When the probe fails, "start hidden", "close to tray" and "minimize to
tray" are disabled for that run (regardless of what is saved in
`desktop.json`) and a one-time dialog explains why, so the window is never
left with no way back. The tray menu also lets you choose how hard the
window asks for attention when a captcha challenge needs you while it is
hidden or in the background.

Quitting from the tray menu always goes through the same graceful shutdown
as the window's own close button and `OnShutdown` (drain, then `a.Close()`),
never a raw process kill.
