# Changelog

All notable changes to KnightLoader. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Versioning

Three things here are installed separately and upgrade separately, so they are
versioned and tagged separately. An APK on a phone does not change when a
container is pulled, and a browser extension does not change because a server
did - tying them to one number would mean either bumping it for changes it does
not contain, or not bumping it for changes it does.

| What | Version lives in | Tag |
| --- | --- | --- |
| KnightLoader itself (server, web UI, desktop) | the tag | `vX.Y.Z` |
| Android app | `mobile/app.json` (`expo.version` **and** `expo.android.versionCode`) | `mobile/vX.Y.Z` |
| Browser extension | `extension/src/manifest.json` | `extension/vX.Y.Z` |

Both are released: `mobile/v1.0.0` and `extension/v1.3.0`. KnightLoader itself
has no tag yet - see [Unreleased] below.

Each tag runs its own workflow and no other: a `*` in a GitHub ref filter does
not cross a `/`, so `mobile/v1.0.0` is invisible to the bare `v*.*.*` pattern
and the reverse. Each workflow refuses a tag whose version does not match the
file it claims to describe.

`versionCode` matters as much as the version string: Android decides upgrade
order by it, and it must go up on every build you hand anybody, even when the
version name is unchanged.

The copy of the extension most people run does not come from its tag. Settings
> Browser tools serves a zip built from the copy embedded in whatever server
binary is running, so that one tracks the server. The tag exists for a store
submission and for a fixed download.

## [Unreleased]

This section is KnightLoader itself, which has no tag yet: the name and the
first release are still open. The app and the extension are tagged already -
see Versioning above.

### Added

- **Collector** that stages links before anything downloads, with name, size,
  availability and the backend that will take each one. A link no backend
  handles is still shown, with the reason attached, and can be rechecked
  without re-pasting.
- **Crawling**: a pasted page becomes the files it links to. Only a link no real
  backend recognised is opened, so a plain download costs no extra request.
- **Resolvers** for direct links, TorBox, AllDebrid and Real-Debrid, yt-dlp, and
  a headless JDownloader as the catch-all. When a backend says a link is not its
  business, the next one gets a turn.
- **Scheduler** with global and per-host concurrency, priorities, manual queue
  order, and automatic retries with a growing delay.
- **Speed limit** that applies to everything and takes effect on downloads
  already running. The embedded engine has no rate-limit hook, so its traffic
  goes through a loopback proxy where the bytes are metered.
- **Download folders**: a global folder, an optional per-package subfolder, a
  per-task override, and path templates such as
  `/downloads/<jd:date>/<jd:hoster>/<jd:packagename>`.
- **Extraction** of zip, rar including multi-volume, 7z including split volumes,
  tar, gz, bz2, xz and zst. A multi-part set waits for every part before it
  opens. Encrypted rar and 7z take passwords, tried per task first and then from
  a configured list.
- **Checksum verification** against an `.sfv`, `.md5` or `.sha*` that arrived
  with the batch, or a CRC in the file name.
- **Click'n'Load** from a site's own button, including the preflight modern
  browsers require before a page may reach a loopback address. The same binary
  runs as a bridge for instances that are not on the browser's own machine.
- **Watched folder** for `.txt` and JDownloader `.crawljob` drop files, carrying
  package name, destination and archive password.
- **Multi-instance federation**: register other KnightLoaders and drive them all
  from one dashboard. Instances on the same network announce themselves over
  multicast and are one click to add, with nothing configured. Two that cannot
  reach each other directly meet through a relay you host yourself.
- **Pairing**: one code, scanned or pasted, connects two instances in both
  directions and gives each side its own named, revocable credential for the
  other. Neither has to be reachable from the internet. Reachability is
  reported per direction, because it fails per direction.
- **Pairing over a relay**: two instances with no address either can dial pair
  through the relay they already share, which is the one case pairing could not
  cover before - so a password-protected instance there refused every call
  forever. That includes two containers behind separate NATs, each announcing a
  LAN address the other cannot use: a code that carries an address tries it
  first and falls back to the relay when it does not carry, and the credential
  is filed under every key the peer can be addressed by rather than under a
  guess about which one will be used. A desktop build can issue a code now for
  the same reason.
- **Connect from anywhere**: the Android app finds servers on its own network
  and fills the address in; the browser extension can send to a peer that has
  no address of its own, by routing through an instance that does; the desktop
  build finds and adds instances even though nothing can dial it back. See
  [docs/connecting.md](docs/connecting.md).
- **Access control**: an optional password lock with signed session cookies,
  off by default.
- **The download list is a real table**: sortable columns you choose, packages
  drawn as rows of their own with a folder glyph and a triangle that collapses
  them, and a right-click menu on packages and files. Which packages are folded
  survives a reload.
- **Settings** as thirteen sub-pages behind one shell, with the set and the order
  coming from the server so the tab bar, the modules page and the index all read
  one list. Among them a **module registry** whose switches really do switch a
  subsystem off, a **connection manager**, and an **Advanced** page generated
  from the Go configuration struct by reflection, so a new setting cannot be
  added without becoming visible.
- **Rules**: the Packagizer and the link filter in one editor, and a holding area
  that keeps what a filter caught instead of discarding it, so a rule that was
  too broad can be seen and undone.
- **Reconnect** in four methods: run a program, replay recorded HTTP requests,
  UPnP (which needs no router details at all, because it asks the network where
  the gateway is), or run a script through a named interpreter. A recorded
  LiveHeader `[[[HSRC]]]` script imports directly, reporting which of its blocks
  mapped alongside which did not. The automatic reconnect fires only when a
  backend itself asked for a later retry, never while the queue is halted, and
  never more than one at a time.
- **Native desktop applications** for Windows, macOS and Linux alongside the
  container. The window is a webview served by the very same HTTP handler the
  container serves, so the engine, the resolvers, the API and the interface are
  not a second implementation that can drift from the first. Every release tag
  builds all three and attaches them to the release.
- **42 languages**, each fetched only when chosen, right-to-left included.
- **GlimStone**, the design language the interface is built on, documented in
  `docs/design-language.md`. Its CSS prefix is `glim-`. The palette is IBM
  Carbon, the same values the sibling apps already use: a shared design language
  has to share the ground first, so GlimStone contributes the system rather than
  a second set of greys.
- **Adjustable corners** — round, soft or square — driven by one token, so the
  whole interface changes together instead of arriving half converted.
- **Adjustable accent** with eight presets and a free colour. The text placed on
  the accent is derived from its luminance rather than configured separately.
- **Rainbow accent**: a palette of eight hues handed out by position, so a long
  download list reads as separate rows. Position, not a hash of the task id: the
  hash kept a row's colour when the rows above it finished, which sounds better
  until three rows share eight buckets and two neighbours come out the same
  colour, which is the one thing the mode exists to prevent. It has a reactive
  mode that rests neutral and colours only what is hovered or running, an
  optional rotation of the starting hue, and all eight colours are editable.
- **Info bubbles.** An explanation now sits behind a neutral `(i)` beside its
  label instead of as grey prose under the control. It opens on hover and on
  focus, closes on Escape, and is rendered at document level so no card or
  scroll container can clip it.
- **BitTorrent and magnet links** as a fourth resolver alongside direct links,
  yt-dlp and JDownloader: selective per-file download, a seed-to-ratio-or-
  duration target, port mapping, and a private torrent's own metadata
  switching off DHT and peer exchange for it automatically, no toggle
  required.

### Security

- The relay identifier in a pairing completion is checked for shape before it is
  used as a credential key. `/complete` accepts a pairing code without
  authentication, so an unchecked field there was a free hand at the key space
  every peer already lives in: naming an existing peer got a full-power token
  minted and labelled after it, and then had that peer's real credential revoked
  as a superseded duplicate - inbound federation from it dying silently, with
  nothing visible on any page. An instance id is 40 hex characters and a pairing
  name is capped at 32, so checking the shape separates the two key spaces
  entirely.
- A pairing completion must offer a way to be reached - an address, or a usable
  relay identifier. A name alone is a label, not somewhere to call, and minting
  a full-power token for one was work done on behalf of nobody.
- A pairing code naming an instance you are ALREADY paired with can no longer
  walk off with that peer's credential. Registering a peer overwrites by name
  and a credential is filed by name, so re-pointing a name at a new address
  left the old peer's token attached to it - and the reachability check that
  runs next put that token in an Authorization header addressed to whoever now
  owned the name.
- Removing a peer now ends its credentials. It used to delete a line and
  nothing else, leaving that peer a live, full-power API token indefinitely,
  and leaving this instance's own credential for it to be inherited by whatever
  was registered under the same name next.
- A peer credential is addressed by token ID rather than by name, so a pairing
  attempt that fails can no longer revoke the credential from the pairing that
  worked. Names are not unique, and "revoke what I just minted" was revoking
  every token ever minted for that peer.
- Nothing announced over multicast is trusted: fields are length-capped on
  arrival and the peer list is bounded, so a device on the network cannot grow
  it without limit.
- Adding a discovered instance exchanges no credentials, and the interface now
  says so instead of claiming otherwise. A password-protected peer added that
  way is reported as needing a pairing code rather than as offline.
- The API no longer sends a wildcard CORS header and the WebSocket no longer
  accepts any origin, which together stopped another website from driving an
  instance through the visitor's browser.
- The Click'n'Load endpoints accept POST only. Answering GET made them a browser
  simple request, which any page could have used to queue downloads and archive
  passwords without the user knowing.
- The JDownloader provisioner fetches over HTTPS and checks the downloaded bytes
  really are an archive before executing them.

### Fixed

- A peer that REFUSES this instance is no longer shown as simply offline. That
  happens on its own whenever the other side sets or changes its password -
  every credential it ever issued is revoked with it - and reported as offline
  it reads as a machine somebody unplugged, so the pairing that would fix it is
  the last thing anyone would try. The status dot has a third state now, and it
  names the pairing code.
- A YouTube link no longer lands in a folder called "watch" when its title
  arrives quickly. The name and the folder were decided by two things racing,
  and the one that lost left the guessed folder in place - so the fix worked
  only when the title took its time.
- An instance whose name is not plain ASCII, or is longer than 32 characters,
  can be paired at all. The name is folded into one that works as a URL path
  segment ("Bürglers Keller" becomes "Burglers Keller"); before, the far side
  refused it as invalid, about a name the person redeeming the code never typed
  and could not see.
- Pairing no longer reports plain success when only one of the two directions
  works, and no longer hangs on a peer that accepts a connection and then says
  nothing - a probe that outlived the other side's own budget could turn a
  working pairing into a timeout.
- The browser extension keeps peers it cannot open a connection to, instead of
  dropping them silently and reporting "No new instances found" - the sentence
  it also showed for an empty list, a sign-in problem, an unreachable host and
  a timeout. Each of those now says what actually happened.
- The Android app's network scan reaches the whole subnet. It probed every
  address at once, and Android's HTTP client queues past 64 concurrent
  requests, so everything past the first batch timed out while still waiting
  for a slot - never sent, never answered. A server anywhere in a typical DHCP
  range was simply never found.
- Renaming an instance reaches the network immediately, rather than leaving
  every other machine showing the old name until the process restarts.
- An instance bound to loopback no longer announces a network address nothing
  serves.
- Removing a task no longer deletes what was downloaded. That was data loss on
  the ordinary "clear finished" path.
- Task IDs are checked for collisions before entering the map, where a duplicate
  would have silently orphaned a running download.
- A single compressed file unpacks beside its archive instead of into a folder
  named after the file it produces.
- A bracketed run of eight digits is no longer read as a CRC32, which had been
  stamping intact downloads as corrupt.
- A byte-order mark no longer eats the first link of a dropped text file.
- One slow WebSocket viewer no longer delays progress updates for everybody
  else.
- The embedded UI carries an ETag and revalidates, so a redeploy cannot leave a
  browser on a stale bundle.
- A private torrent's DHT and peer exchange refusal, and the seed-ratio,
  seed-duration and port settings on the Torrents page, are now actually
  carried into a running download - all four were previously saved and
  validated but never reached the torrent engine.
- A script started at the moment of shutdown can no longer outlive it. Asking
  "is the host still open?" and signing up to be waited for were two separate
  steps, so a run that slipped between them went untracked: shutdown returned
  while the script kept calling into an app being torn down around it, and the
  same gap could trip Go's own guard against that pattern and take the process
  down. The two steps are now one.
- The same gap is closed in the two other places it existed: the background jobs
  the app itself starts - an availability probe, a checksum pass, a dropped job
  file, the update that publishes a finished task - and the desktop build's own
  window and tray helpers. A shutdown now either waits for a job or refuses it
  outright, with nothing in between, so none of them can still be writing to a
  database or a window that has just been closed underneath them.
