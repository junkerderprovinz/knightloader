# KnightLoader

> **Working title (KL).** A modern, self-hosted, cross-platform download manager — a clean-UI alternative to JDownloader that grabs files from everywhere and hauls them into one keep. Final name TBD before release.

**Status: early WIP — M0 (feasibility spike) is green.** Not yet a usable product.

## What it is

A single Go backend with an embedded download engine and a small **resolver** seam, driven by a modern web UI (IBM Carbon, dark) and shipped as both a Docker image and native desktop apps. Full hoster coverage comes from swappable resolvers — an invisible headless-JDownloader (arm's-length, via its local API), yt-dlp for media, and an optional debrid service — with native resolvers to follow. The user's own accounts stay the user's; nothing is bundled that shouldn't be.

## Stack

- **Backend:** Go, embedding [Gopeed](https://github.com/GopeedLab/gopeed)'s `pkg/download` engine (in-process; no aria2, no subprocess).
- **UI:** React + [IBM Carbon](https://carbondesignsystem.com/) (dark), REST + WebSocket live.
- **Desktop:** [Wails](https://github.com/wailsapp/wails) (Win/macOS/Linux). **Container:** Docker (multi-arch).
- **Resolvers** (priority order): `direct` (file links, fetched by the embedded engine) · [TorBox](https://torbox.app/) debrid (supported file hosters, unlocked to a direct CDN URL the engine downloads) · [yt-dlp](https://github.com/yt-dlp/yt-dlp) (media/streaming, when the binary is present) · headless [JDownloader](https://jdownloader.org/) (catch-all backup via its local API). Native resolvers come later.
- **Accounts:** credentials (e.g. the TorBox API key) are stored encrypted at rest (AES-256-GCM under a per-install keyring) via `POST /api/accounts {"service":"torbox","secret":"…"}`.

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

## M0 — feasibility spikes (green)

The two riskiest integrations, both proven end-to-end:

| Spike | Proves | Run |
|---|---|---|
| `cmd/spike-download` | Gopeed embeds in a Go binary; **custom per-request headers are sent** (token round-trips through an echo server); live progress/speed streams. | `go run ./cmd/spike-download` |
| `cmd/spike-jd` | A Go client drives **headless JDownloader via its local "Deprecated API"** (plain HTTP JSON, no cloud): `addLinks` + `queryLinks` with live name/size/status. | `KL_JD=http://<jd-host>:3128 go run ./cmd/spike-jd` |

To exercise `spike-jd` against a JDownloader instance, enable its Deprecated API (Advanced Settings → RemoteAPI → *Deprecated API* on, *Localhost only* off; listens on `:3128`) and point `KL_JD` at it.

## Licence

[AGPL-3.0](LICENSE). Own code; name and branding reserved.
