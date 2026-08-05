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
