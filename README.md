# KnightLoader

> **Working title (KL).** A modern, self-hosted, cross-platform download manager — a clean-UI alternative to JDownloader that grabs files from everywhere and hauls them into one keep. Final name TBD before release.

**Status: WIP.** It downloads, and it runs on a box all day — the feature list below is real, not planned. What is not settled is the name, and there is no release yet.

## What it is

A single Go backend with an embedded download engine and a small **resolver** seam, driven by a modern web UI (IBM Carbon, dark) and shipped as both a Docker image and native desktop apps. Full hoster coverage comes from swappable resolvers — an invisible headless-JDownloader (arm's-length, via its local API), yt-dlp for media, and an optional debrid service — with native resolvers to follow. The user's own accounts stay the user's; nothing is bundled that shouldn't be.

## Stack

- **Backend:** Go, embedding [Gopeed](https://github.com/GopeedLab/gopeed)'s `pkg/download` engine (in-process; no aria2, no subprocess).
- **UI:** React + Tailwind (IBM Carbon palette, dark/light), a sidebar app with Overview, Collector, Downloads, Instances, Accounts and Settings; live speed graph, package-grouped lists, REST + WebSocket.
- **Desktop:** [Wails](https://github.com/wailsapp/wails) (Win/macOS/Linux). **Container:** Docker (multi-arch).
- **Resolvers** (priority order): `direct` (file links, fetched by the embedded engine) · [TorBox](https://torbox.app/) debrid (supported file hosters, unlocked to a direct CDN URL the engine downloads) · [yt-dlp](https://github.com/yt-dlp/yt-dlp) (media/streaming, when the binary is present) · headless [JDownloader](https://jdownloader.org/) (catch-all backup via its local API). Native resolvers come later.
- **Crawling:** a pasted page becomes the files it links to instead of one unusable task. Only a link no real backend recognised is opened, so a plain download never pays for the extra request, and a page that yields nothing is staged as itself.
- **Intake:** besides pasting, links arrive through [Click'n'Load](docs/clicknload.md) from a site's own button, and through a watched folder — drop a `.txt` or a JDownloader `.crawljob` onto a share and the box picks it up, with its package name, destination and archive password.
- **Collector (LinkGrabber):** pasted or dropped links are staged and analysed first (name, size via a HEAD probe, resolver, online check) and grouped into packages; you select and start them, then they move to Downloads. `POST /api/links` stages, `POST /api/tasks/start {ids}` starts (empty = all).
- **Accounts:** credentials (e.g. the TorBox API key) are stored encrypted at rest (AES-256-GCM under a per-install keyring) via the Accounts page or `POST /api/accounts {"service":"torbox","secret":"…"}`.
- **Scheduler:** a global and a per-host concurrency limit gate every backend (FIFO with per-host skip-ahead); both are live-tunable in Settings.
- **Extraction:** finished archive downloads (zip, rar incl. multi-volume, 7z incl. `.001` volumes, tar.gz) unpack automatically into a sibling folder — pure Go, zip-slip-safe, optional delete-after-extract. A multi-part set waits for every part before it opens. Encrypted rar and 7z accept passwords: the task's own first, then a configured list. Encrypted zip is refused, because Go cannot decrypt it.
- **Download folders:** a global folder, an optional per-package subfolder, and a per-task override. The folder may be a template — `/downloads/<jd:date>/<jd:hoster>/<jd:packagename>` — and is checked when you save it, so an unwritable path is refused rather than silently ignored.
- **Integrity:** a finished download is verified against an `.sfv`/`.md5`/`.sha*` that arrived with the batch, or against a CRC in its own file name. Nothing is marked as passing that was not actually checked.
- **Speed limit:** a total for everything, applied while downloads run rather than only to the next one. The embedded engine has no rate-limit hook (Gopeed v1.9.x), so its traffic goes through a loopback proxy where the bytes are metered; yt-dlp uses `--limit-rate` and JDownloader its own API.
- **Nothing is dropped:** a pasted link that no backend handles, or that fails to resolve, is still staged with the reason attached. Links carry an online state and can be rechecked without re-pasting.
- **Fallback chain:** when a backend says a link is not its business, the next one gets a turn. Only an explicit signal advances the chain, so a hoster link that genuinely failed is never re-fetched as a plain web page.
- **Retries:** a failed download is retried automatically with a growing delay, bounded by a setting.
- **Access:** an optional password lock, off by default. Sessions are signed cookies, so a restart does not sign anyone out. The API is same-origin only and the WebSocket rejects foreign origins.
- **Languages:** 26, each fetched only when chosen, with right-to-left layout for Arabic and Hebrew.
- **Multi-instance:** register other KnightLoader instances (Settings → Instances) and switch between them in the header — one dashboard views and controls every box (self-hosted federation, no relay; only task/link routes are proxied, a peer's settings stay its own).
- **Click'n'Load:** a listener on `127.0.0.1:9666` speaks the standard CnL protocol (`jdcheck.js`, `/flash/add`, `/flash/addcrypted2`), so existing browser extensions and site buttons hand links straight to KnightLoader. Disable or move it with `KL_CNL` (`0` = off).
- **Desktop app:** a native Windows/macOS/Linux build (Wails) lives in [`desktop/`](desktop/) — the same server in a native window, which provisions a private headless JDownloader on first run (`KL_PROVISION_JD=1` on the server build) for full hoster coverage out of the box.

## Running

```sh
go run ./cmd/knightloader      # then open http://localhost:8749
```

Configuration (all optional, via env):

| Var | Default | Meaning |
|---|---|---|
| `KL_ADDR` | `:8749` | listen address |
| `KL_DATA` | user config dir | data directory (SQLite DB + downloads) |
| `KL_YTDLP` | `yt-dlp` (PATH) | path to the yt-dlp binary; media links route through it when present |
| `KL_TORBOX` | — | TorBox API key (or store it via `/api/accounts`); supported hoster links are unlocked through TorBox's debrid |
| `KL_JD` | — | a headless JDownloader Deprecated-API URL (e.g. `http://jd:3128`); when reachable, it is the catch-all backup for hoster links nothing else claims |
| `KL_CNL` | `9666` | Click'n'Load listener port on `127.0.0.1`; `0` disables it |
| `KL_PROVISION_JD` | `0` | `1` = provision a private headless JDownloader on first run (downloads JD, enables its local API, launches it) and use it as the hoster backup |

Two flags turn the same binary into a Click'n'Load bridge for an instance that
runs somewhere else — see [docs/clicknload.md](docs/clicknload.md):

```sh
knightloader -bridge http://nas:8749 [-bridge-password '…']
```

## Feasibility spikes

The two riskiest integrations, both proven end-to-end before the rest was built:

| Spike | Proves | Run |
|---|---|---|
| `cmd/spike-download` | Gopeed embeds in a Go binary; **custom per-request headers are sent** (token round-trips through an echo server); live progress/speed streams. | `go run ./cmd/spike-download` |
| `cmd/spike-jd` | A Go client drives **headless JDownloader via its local "Deprecated API"** (plain HTTP JSON, no cloud): `addLinks` + `queryLinks` with live name/size/status. | `KL_JD=http://<jd-host>:3128 go run ./cmd/spike-jd` |

To exercise `spike-jd` against a JDownloader instance, enable its Deprecated API (Advanced Settings → RemoteAPI → *Deprecated API* on, *Localhost only* off; listens on `:3128`) and point `KL_JD` at it.

## Licence

[AGPL-3.0](LICENSE). Own code; name and branding reserved.
