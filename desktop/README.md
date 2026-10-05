# KnightLoader desktop

The native desktop build (Windows / macOS / Linux), packaged with
[Wails 3](https://v3.wails.io). It runs the exact same server as the container build
(engine, resolvers, REST + WebSocket API, embedded UI) inside a native
webview window, and provisions a private headless JDownloader on first run so
hoster coverage works out of the box (JD's own UI is never shown).

This is a **separate Go module** on purpose: the Wails toolchain and its
per-platform native dependencies never touch the server module, which stays a
clean, pure-Go build.

Its `go.sum` is committed, and must stay that way. It used to be in
`.gitignore`, on the reading that anything the Wails build regenerates is
output. It is not: `go.sum` is the module's integrity file, and while it was
ignored, four Renovate security bumps landed as a changed `go.mod` with no
matching hashes, the CI cache never resolved, and every desktop build trusted
whatever the proxy happened to serve. `go.mod` also carries a `go` directive
that must never fall behind the root module's, because the `replace` at its
foot consumes the server sources from `../`.

## Building

Desktop bundles are built **per platform in CI** (`.github/workflows/desktop.yml`)
because each target needs its own toolchain (WebView2 on Windows, Cocoa on
macOS, GTK/WebKit2GTK on Linux) and signing.

Locally, with the Wails 3 CLI at the version `go.mod` requires and a JDK-free
Go toolchain, from the repository root:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28
node scripts/desktop.mjs               # bundle for the current OS → build/bin/
node scripts/desktop.mjs --installer   # on Windows, the NSIS installer as well
```

The script has Wails write the manifest, the icons, the version resource, the
Info.plist and the installer's helper macros into `build/` from
`build/config.yml`, and git ignores all of them. The interface is the
committed `web/dist`, so run `npm run build` in `web/` first after changing
it. On Linux, install the CLI with `-tags gtk3`; without it the CLI wants
GTK 4.

## Updates

The app only updates itself when it knows its version. `scripts/desktop.mjs`
stamps the tag on a tag build, and `KL_VERSION` anywhere else:

```sh
KL_VERSION=v1.3.0 node scripts/desktop.mjs
```

A build without it is a dev build and never looks for updates. `updates.go`
runs the daily check and reads the Update automatically setting;
`internal/update` finds the release, downloads this platform's zip, checks it
against `checksums.txt` and swaps the program inside it in for the next start.
On Windows the running exe steps aside as `KnightLoader.exe.old`, which the next
start removes.

The Windows installer (`build/windows/nsis/project.nsi`) puts the app under
Program Files for all users, where it cannot replace itself. It creates the
scheduled task **KnightLoader Update**, which starts `KnightLoader.exe --update`
as the system account once a day and five minutes after boot. That run
(`machine_windows.go`) returns before JDownloader, Click'n'Load or the window
start, checks that it is the copy named by `InstallLocation` in the uninstall
entry under HKLM, reads the switch from `%ProgramData%\KnightLoader\settings.json`
and runs the same check, download and swap, logging to `update.log` beside it.
The installed app keeps the switch in that file too and only watches the entry's
`DisplayVersion` for a newer version to announce. Any other copy is portable and
updates itself.

To try the whole path locally, build with `-tags updatetest`. That build reads
`KL_UPDATE_API` (a stand-in for `https://api.github.com` that serves
`/repos/junkerderprovinz/knightloader/releases/latest`) and `KL_UPDATE_DELAY`
(how long to wait before the first check, such as `15s`). Release builds leave
the tag out, so no environment variable can change where an update comes from.

The same build installs as **KnightLoader Test**, beside a real installation,
with a task and a folder under ProgramData of that name:

```sh
KL_VERSION=v1.2.1 node scripts/desktop.mjs --installer --updatetest
```

Its task starts without your environment and reads the stand-in's address from
`updatetest-api` in `%ProgramData%\KnightLoader Test`, which an administrator
writes there after installing.

## How it fits together

- `main.go` boots `app.New`, provisions JD if `KL_JD` is unset, then starts
  Wails with the server's `api.Handler` as the whole **asset server**, so the
  SPA and `/api/*` are served in-window, identical to the browser build.
- The page calls the Go side through Wails' runtime endpoint by name:
  `main.DesktopFiles` (`files.go`), `main.HubBridge` (`stream.go`) and
  `main.Tray` (`tray.go`). `web/src/lib/desktop.ts` holds every such call and
  does without Wails' JavaScript runtime. v3 finds the bound methods by
  reflection at run time, so no build step runs the program to generate
  bindings.
- The asset handler cannot carry a WebSocket, so the window never reaches
  `/api/ws`. `HubBridge` carries the stream instead: each stream a page opens
  is a hub connection of its own whose messages arrive as Wails events in the
  window that opened it, and `connectWS` in `web/src/lib/api.ts` picks it
  whenever it runs in a window.
- The frontend is the shared `../web` project (Carbon UI).

## Tray and window behaviour

`tray.go` (plus `config.go`, `tray_probe_*.go` and the embedded icon in
`assets.go`) puts an icon in the notification area or the menu bar with
Wails' own tray, and adds the window's close/minimise/start-hidden behaviour.
A click on the icon opens a small window beside it (`overview.go`, the page
at `/tray`) with the speed, the counts, the captchas waiting, what is
downloading and the latest downloads, a button that stops or starts the queue
and one that opens the main window. Its edges can be dragged, and
`desktop.json` keeps the size. A double click opens the main window, and a
right click the menu. The menu shows and hides the window, stops or starts
the queue, quits, and holds the preferences, which are set there rather than
on a settings page. The page hands the menu its words in the interface's
language. The preferences belong to one installation on one machine. They
are saved to `desktop.json` next to the rest of this build's data directory
(`KL_DATA`, or the OS config directory), and never sent to
`settings.Settings`, which every connected browser shares.

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

## Keeping the computer awake

While "Keep the computer awake while downloading" is on (Automation page, Idle
card, and the same switch on the Modules page) and `App.Working` reports work
under way (a transfer, an unpacking, a finished file being checked or moved, a
retry waiting to start, an event program), the app holds off system sleep, and
it gives the hold back once there is none. `internal/keepawake` decides when;
`awake_windows.go`, `awake_darwin.go` and `awake_linux.go` ask the operating
system: `SetThreadExecutionState` on Windows, `caffeinate -i -w <pid>` on
macOS, and a logind sleep inhibitor over the system D-Bus on Linux. None of
them keeps the screen on, and each hold ends with the process if it dies. The
server binary never imports any of it.
