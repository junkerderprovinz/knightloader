# Torrent / magnet-link support — design spec

Grilled 2026-08-11. Slots into the v1 campaign as a new wave between the
already-running Wave 11 and the already-prepared Wave 12, so Wave 12's own
final locale-parity sweep covers this feature's strings too instead of
needing a second translation pass afterward.

## Goal

Add BitTorrent (magnet links and uploaded `.torrent` files) as a fourth
resolver alongside JD, yt-dlp and Debrid — full depth: selective file
download, peer/seed visibility, a seed-to-ratio default, port mapping
guidance, and private-tracker safety. Decided in the 2026-08-11 grilling
session (see the six decisions below); this document is the design those
decisions imply, not a re-litigation of them.

## What is already true, verified in the tree — read this before designing anything else

The single most important fact this spec rests on, confirmed by reading the
actual vendored source rather than assumed from gopeed's marketing:

- **gopeed's embedded downloader already silently supports BitTorrent
  today**, unused. `internal/engine/engine.go:33-39` builds a
  `download.DownloaderConfig{}` with no `FetchManagers` override and calls
  `.Init()` on it. Gopeed's own `Init()`
  (`pkg/download/model.go:162-172`, vendored at
  `github.com/GopeedLab/gopeed@v1.9.3`) defaults `FetchManagers` to
  `[http.FetcherManager, bt.FetcherManager, ed2k.FetcherManager]` whenever
  the caller leaves it empty — which KnightLoader does. **The underlying
  `download.Downloader` gopeed instance can already fetch a magnet link or
  a `.torrent` file today; no change to gopeed's own configuration, and no
  second download pipeline, is needed to make that possible.** What
  KnightLoader's own `internal/engine` wrapper still needs is a new call
  shape into that same downloader — see Architecture below — because its
  existing `Engine.DownloadTo` is HTTP-shaped (a URL plus headers), not a
  magnet-URI-or-torrent-bytes shape.
- The real BT fetcher lives at `github.com/GopeedLab/gopeed/internal/protocol/bt`
  — a Go `internal/` package, unreachable from KnightLoader directly. This
  does not matter: KnightLoader never touches it directly today for HTTP
  either, it goes through `download.Downloader`'s own public API
  (`Resolve`/`CreateTask`/events), and BT rides the same path.
- A **separate, public** package, `github.com/GopeedLab/gopeed/pkg/protocol/bt`,
  exists purely as a data-model package (verified: it contains only
  `model.go`, 19 lines) — `ReqExtra{Trackers []string}` for supplying extra
  trackers on resolve, and `Stats{TotalPeers, ActivePeers,
  ConnectedSeeders, SeedBytes, SeedRatio, SeedTime}`. This is almost
  certainly how peer/seed/ratio numbers reach the outside world, but the
  **exact path from a live `download.Task` to a populated `bt.Stats` is
  not yet confirmed** — `download.Task` has a top-level `Meta
  *fetcher.FetcherMeta` field and a top-level `Uploading bool` (confirming
  gopeed's own model already distinguishes "actively seeding" from a
  finished, non-uploading task), but whether `Stats` hangs off `Meta`
  directly or requires a protocol-specific cast needs five minutes with
  the actual running engine at build time, not more research now. **Verify
  this first, before writing the resolver**, the same way M0's spike
  proved the JD API shape before M2 was built on top of it.
- `ReqExtra.Trackers` on the request side, and the fact that a `.torrent`
  file's own metadata carries a `private` flag standard to the BitTorrent
  spec (BEP 27, `info.private == 1`), is what the private-tracker
  DHT/PEX-disable decision below hooks into — gopeed almost certainly
  reads and honours this flag itself (standard BT client behaviour), but
  confirm by reading `internal/protocol/bt` behaviour indirectly (its
  public `Stats`/`ReqExtra` shape, and testing live against a private
  tracker's `.torrent`) rather than assuming.

## The six decisions from the grilling session

1. **Timing**: build into the current v1 push, not deferred to v1.1. A new
   wave, inserted between the already-running Wave 11 and the
   already-prepared Wave 12.
2. **Depth**: full — peer/seed UI, port/NAT guidance, tracker awareness.
   Not the minimal "bare resolver, default behaviour" alternative.
3. **Seed default**: a finished torrent keeps seeding until a configurable
   ratio target (or duration) is reached, then stops on its own — not
   JDownloader/every-other-resolver's "done means done, no afterlife."
4. **Idle-queue interaction**: a torrent that is only seeding (not
   downloading, not queued) does **not** count as active work for Wave
   10's end-of-queue idle action. Without this, one perpetually-seeding
   torrent would permanently disable that entire feature the moment
   torrents exist in the same instance.
5. **Private trackers**: DHT and Peer Exchange are automatically disabled
   for any torrent whose metadata marks it private, with no user toggle
   needed to get this right — the alternative (a global switch the user
   has to remember to flip per-torrent) is a trap most private trackers
   punish with a ban.

   **STATUS (Wave 11.5's own build): NOT DELIVERED, and not currently
   deliverable.** The "gopeed almost certainly reads and honours this flag
   itself" hedge above (this section's own opening) was checked rather than
   assumed, by three independent readings of the vendored source, and it
   does not hold: `bt.Fetcher.initClient()` builds ONE package-level
   singleton `torrent.Client` shared by every torrent task in the process,
   and neither it nor anywhere else in `github.com/GopeedLab/gopeed@v1.9.3`
   ever sets a `NoDHT` or `DisablePEX` field (or equivalent) — confirmed by
   grep across the whole vendored module, zero matches. There is currently
   no API surface through which KnightLoader could disable DHT/PEX for one
   torrent, or even for all of them. The honest fix landed instead:
   `web/src/pages/settings/Torrents.tsx`'s private-torrent note was reworded
   from a present-tense enforcement claim ("private torrents automatically
   get DHT/PEX disabled") to naming this exact gap, in every locale. Whoever
   picks this back up needs a path INTO gopeed's fork or a patch upstream,
   not another pass over this codebase's own torrent package - the
   constraint is entirely on the other side of that boundary.
6. **Selective download**: multi-file torrents get a file tree at add-time
   to check/uncheck individual files, the same way ordinary torrent
   clients do, rather than always fetching every file.

Reconfirmed, unchanged: the original 2026-07-30 grilling's guardrail (no
piracy framing, no hosted leech instance, no built-in search/indexing)
applies to this feature exactly as it already applies to JD/yt-dlp/Debrid.
KnightLoader accepts a magnet link or `.torrent` file the user already has;
it does not help find one.

## Architecture

**New resolver**, `internal/resolver/torrent`, implementing the existing
`Resolver` interface (`Info`/`Match`/`CheckLink`/`Resolve`) the same as
`internal/resolver/{jd,ytdlp,torbox,debrid}` already do:
- `Match` recognises `magnet:` URIs (regex on the scheme) and `.torrent`
  files (by content — a bencoded dict starting `d8:announce` or similar —
  not by filename, the same reason every other resolver in this codebase
  checks bytes over extensions where it can).
- `Resolve` hands the magnet URI or `.torrent` bytes to the embedded
  `Engine` (a thin new method, `Engine.DownloadTorrent` or similar,
  parallel to the existing `Engine.DownloadTo`) rather than building a
  second, parallel download pipeline — the whole point of the fact above
  is that there is nothing new to wire at the engine level, only a new
  call shape into the same `download.Downloader`.
- `.torrent` FILE upload is a new intake path (see UI below), not a URL at
  all — the collector's existing add-link flow assumes a string; this
  needs a real file upload, closer in shape to Wave 10's own restore
  upload (`multipart/form-data`, `internal/api/routes_backup.go`'s
  `uploadRestore` is the pattern to copy) than to a pasted link.

**Port mapping** reuses Wave 3's UPnP discovery
(`internal/reconnect/upnp.go`'s `ssdpSearch`/`gateways`/`wanServices` —
read that file in full before building this) rather than re-implementing
SSDP search: that machinery already finds the router's `WANIPConnection`
service. What's new is one additional SOAP action, `AddPortMapping`, with
real parameters (internal IP, internal port, external port, protocol,
description) where the existing reconnect actions take none — `soap()`'s
current signature may need a params argument, or a sibling function.
Whether this lives inside `internal/reconnect` itself (widening its
scope beyond "get a new public IP") or a new `internal/portmap` package
that reuses `reconnect`'s discovery types is an implementation choice for
whoever builds this wave, not decided here — lean toward a new package if
`reconnect`'s own doc comments frame it narrowly around reconnection
specifically (check before choosing).

## Data model

- `core.Task` gains torrent-specific fields, all optional/omitempty so
  every non-torrent task is unaffected: `Peers int`, `Seeds int`, `Ratio
  float64`, `Uploaded int64`. A "currently seeding" state is a **flag with
  a typed reason**, never a new `core.Status` value — build-plan.md
  section 4 conflict 2's rule, unbroken by every wave since. Likely
  `Seeding bool` alongside the existing `Status == core.StatusDone`,
  matching gopeed's own `Task.Uploading bool` shape.
- Multi-file selection: `core.Task` (or a new sub-type staged before the
  task itself is created — check whether selection has to happen *before*
  a `core.Task` exists at all, since the collector's staging model may
  need a distinct "resolved torrent, awaiting file selection" state ahead
  of today's `StatusCollected`) needs a file list: `TorrentFile{Path
  string, Size int64, Selected bool}`.
- New settings block, `settings.Torrent`: `SeedRatioTarget float64`,
  `SeedDurationSeconds int` (whichever is reached first stops seeding —
  confirm this "first of either" semantic is what's wanted, it was not
  asked explicitly), `UploadLimitKiBs int`, `Port int` (0 = let gopeed/the
  OS pick), `DHTEnabled bool` / `PEXEnabled bool` (the *default* — always
  overridden to false for a private torrent regardless of this setting,
  decision 5 above).
- `Counters()` (wherever Wave 10's idle-detection reads "owed" work —
  `internal/app`, the exact function Wave 10's own brief already named)
  gains one more exclusion: a task that is `Seeding == true` does not
  count, the same way done/error/collected already don't. Cross-reference
  Wave 10's `queueIdleForAction` (`internal/app/app_idle.go`) directly —
  this is the one existing function whose definition of "idle" this
  feature must agree with, not a new parallel definition.

## UI

- **Collector intake**: a new entry point alongside paste/drop/CnL/watch
  folder — paste a magnet link (fits the existing text-paste path once
  `Match` recognises the scheme) or upload a `.torrent` file (new,
  file-upload-shaped intake, see Architecture above). For a multi-file
  torrent, resolving it (reading the `.torrent`/fetching magnet metadata)
  surfaces a file tree to check/uncheck before staging continues — this is
  a new step in the collector flow, not a retrofit of the existing
  single-file staging card.
- **Downloads table**: new columns for Peers/Seeds/Ratio, registered in
  the existing column-customisation system (`DEFAULT_HIDDEN`, the column
  menu — Wave 4) and **hidden by default**, the same treatment six other
  low-traffic columns already get.
- **Row tooltip** (Wave 9's `useTooltip`/`RowTooltipContent`,
  `web/src/components/columns.tsx`): tracker list, info hash, full
  peer/seed detail — available even with the columns hidden, matching how
  the tooltip already surfaces columns most users leave off.
- **New settings page**, "Torrents" — its own tab (own icon; check
  `web/src/lib/icons.tsx` for something already fitting, e.g. a
  peer/network glyph, before adding a new one), seed ratio/duration
  target, upload limit, port field plus an "attempt UPnP mapping" button
  (surfaces success/failure, does not silently retry forever), DHT/PEX
  default toggle with a note that private torrents override it
  automatically.

## Risks and open items (verify during the build, do not guess)

- **The exact `download.Task` → `bt.Stats` path** (see the verified-facts
  section above) — the first thing whoever builds the resolver should
  confirm, live, against a real magnet link, before writing UI against an
  assumed field path.
- **Multi-file selection's exact staging-model fit** — whether it needs a
  new pre-`core.Task` state or can reuse `StatusCollected` with an
  attached file list is a real design question the collector's own
  existing code should settle, not this spec.
- **`AddPortMapping` failure modes** — double NAT, no UPnP support at all,
  a router that accepts the call but silently drops the mapping. The UI
  must report an honest "could not confirm" rather than claiming success
  on a fire-and-forget SOAP call with no verification read-back.
- **`SeedRatioTarget` vs `SeedDurationSeconds`** — confirm "whichever is
  reached first" is the intended combination rule before building the
  controller logic for it; not explicitly settled in the grilling.
- Whether `internal/reconnect` is the right home for `AddPortMapping` or a
  new sibling package should reuse its types is an implementation choice,
  not pre-decided here (see Architecture above).

## Sequencing

Inserted as a new wave after Wave 11 lands and before Wave 12 runs, so
Wave 12's final locale-parity sweep (already scoped as a full audit across
everything Waves 1–11 added) naturally covers this wave's new strings too.
Given the surface area (resolver + engine call shape, port-mapping
package, a new settings page, collector file-tree UI, new columns, an
idle-detection cross-reference, private-tracker safety), this likely wants
more build agents than an ordinary wave — decided when the workflow script
for it is written, not here.
