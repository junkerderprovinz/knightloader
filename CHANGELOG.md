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

Both are released at `mobile/v1.0.0` and `extension/v1.0.0`. KnightLoader
itself is released at `v1.0.0`.

The extension's earlier 1.1 and 1.2 were numbers in `manifest.json` that were
never tagged or published, so its first release folds them in rather than
starting at a version nobody ever had.

Each tag runs its own workflow and no other: a `*` in a GitHub ref filter does
not cross a `/`, so `mobile/v1.0.0` is invisible to the bare `v*.*.*` pattern
and the reverse. Each workflow refuses a tag whose version does not match the
file it claims to describe.

`versionCode` matters as much as the version string: Android decides upgrade
order by it, and it must go up on every build you hand anybody, even when the
version name is unchanged.

The copy of the extension most people run does not come from its tag. Settings
> Browser & App serves a zip built from the copy embedded in whatever server
binary is running, so that one tracks the server. The tag exists for a store
submission and for a fixed download.

## [1.0.0] - 2026-09-02

The first release of KnightLoader itself: server, web interface and desktop
build. The app and the extension are versioned separately, see Versioning
above.

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
- **A twelve-word connection phrase.** Read it off one instance, type it into
  the next, and they find each other across networks - no account, no login,
  no port forward, no domain, and no third-party site to visit. The words are
  BIP39's, the list hardware wallets use, chosen there for the properties that
  matter here too: no two words share their first four letters, none are
  near-homophones, none carry accents. So a phrase survives being read down a
  phone line and typed on a mobile keyboard, and its checksum refuses a
  mistyped or swapped word on the spot - naming the word and its position -
  instead of letting it become a connection that silently never finds its
  sibling. The relay is told `SHA-256(domain || secret)`, never the secret, so
  whoever runs one cannot reconstruct anybody's words; showing a phrase again
  needs the instance password re-entered, because a session opened hours ago
  is not evidence anybody is still sitting there. Run the relay yourself and
  the same phrase works against it.
- **The relay cannot read what it forwards.** A second key comes out of the
  same secret under its own domain, and every proxy frame is sealed with
  AES-256-GCM under it. A relay sees which instance a frame is for and which
  request it answers, because it routes on those - not the path, not the body,
  not the API token a phone attaches. The two domains are the point: a relay
  is handed the group key in every hello frame, so a frame key derived from
  that would be one it already holds. Routing fields are bound into the seal,
  so a frame cannot be redirected and still open. A relay configured by
  hand-entered key instead of by phrase seals against everything between the
  instances and the relay, but not against its operator, who holds that key.
- **The card explains itself before it asks anything.** Twelve words is an
  odd enough thing to be handed that "what am I looking at" comes before
  "what do I press", so connecting opens with three numbered steps and a
  paragraph on what actually happens when you press them. The button names
  inside the steps come from the buttons' own translation keys rather than
  being written into the sentence, so a step cannot end up quoting a label
  that says something else in that language.
- **Getting the app lives with getting the extension.** Both answer the same
  question - how do I reach this from somewhere that is not this browser tab -
  so the app card moved onto the tab that already held the bookmarklet and the
  extension, now called Browser & App. The store badges are the real artwork,
  with a direct APK download beside them, which is the one of the three routes
  that works before a store listing exists.
- **Instances can be hidden from the sidebar** the way Accounts already could,
  through a settings tab of the same shape - useful for anybody running the
  one instance, whose Instances page lists exactly itself, forever. Each
  instance's card now carries the app's mark down its left edge.
- **The unprotected-instance warning stopped being a banner.** It fired on
  every load of every container, because a container binds every interface in
  its own namespace by design, and it was saying what the password card three
  centimetres above it already said. It is now a second line on that card, in
  the warning colour, next to the field that fixes it.
- **Tailscale is gone.** It had been in this card since before the relay
  existed, when it was the only way in from outside, and merging the cards
  moved it rather than removing it - so the page whose whole point is not
  needing a third-party login kept offering one. Nobody ever had to use it;
  the phrase never touched it. Its one unique job was handing out a public
  address a stranger's browser could open, and the answer to that is now your
  own domain in front of a reverse proxy. Self-hosting the relay moves to
  Settings → Advanced (`relayUrl`, `relayServe`), which lists every setting
  this instance has.
- **The relay gets its own certificate.** Set `KL_RELAY_DOMAIN` and it
  terminates TLS itself over TLS-ALPN-01 - no reverse proxy, no certbot, no
  renewal cron, and no port 80, because the challenge completes inside a
  handshake on 443. Repeated failed handshakes from one address back off, so a
  relay on a public address is not a free guessing gallery.
- **Being in the group is the credential.** A request arriving over the relay
  came off a socket the relay only joins to connections presenting the same
  group key, so the sender has already proved it holds the phrase - and a
  password-protected instance accepts its own siblings instead of answering
  401 to all of them. A pairing code used to be what closed that gap, one peer
  at a time; it is gone, and nothing replaced it because nothing needs to.
  What a sibling may reach is an allowlist rather than a property each route
  happens to have: tasks, links and the queue, plus reading the auth state,
  the peer list and the instance's own accent. Not the settings, not the
  accounts, not the phrase.
- **The phone joins the group too.** Twelve words, and every instance appears
  at once - where it used to want a relay address, a relay key and then an API
  token per instance, and saved one instance per visit. It decodes the phrase
  itself, because the case the phrase exists for is the one where there is no
  server to ask.
- **The Android app asks for four fewer permissions**, and the four it dropped are the reason Play Protect blocked the install: `SYSTEM_ALERT_WINDOW` ("draw over other apps"), the microphone, biometrics and external storage. None of them came from this code. expo-camera brings the microphone along because it can also record video, React Native brings the overlay permission for its own developer overlay, and expo-secure-store brings biometrics for an option that is not used here. A download manager asking for the microphone and permission to draw over other apps looks like malware, and Play Protect was right to say so. What remains is camera, internet, and network and Wi-Fi state.
- **One instance can be the relay**, from a switch on the Access tab, instead of
  a second program on a second address. It answers under `/relay/connect` on the
  address that instance already uses, behind the same reverse proxy and the same
  certificate, and it admits only the relay key that instance stores - so
  turning it on does not make a published address a meeting place for whoever
  finds it. Off, the route answers 404, exactly as a build without the feature
  does. What it cannot change is the one thing a relay needs: it is the third
  point both sides dial out to, so the instance hosting it has to be reachable.
- **One card for connecting.** It used to be several, each showing a different
  piece of the plumbing to somebody who came for an answer. There is one now,
  named for the two things it does - connect your instances, and reach this one
  from anywhere - and it opens with the answer rather than the machinery: one
  sentence saying whether the group is up, then the phrase. Everything that was
  a third and fourth way to arrive at the same place is gone rather than folded
  away, because a fold is still a thing to wonder about.
- **The app and the extension follow GlimStone**, the design language the web
  UI already speaks: the same palette, the same corner shapes, the same
  Sunflower-gold accent before anyone touches a picker. The app takes its look
  from the instance it is connected to, so opening the app and that instance's
  web interface side by side shows one product rather than two opinions - with
  a local override for anyone who wants a different colour on their own phone.
- **Rainbow in the app**: a long list of downloads reads as distinct rows
  instead of one gold wall. Shown rather than set, because the palette offset
  lives on the instance - two clients of one server disagreeing about the
  colour of a download is a bug, not a preference.
- **Problems?** in the app's settings and the extension's options: the version,
  the platform and the shape of the configuration, copied in one tap or opened
  as a prefilled report. No address, no token and no relay key is in it - an
  address is somebody's home network and a token is a credential, and both
  would otherwise be pasted into a public issue by anyone who trusted the
  button.
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

- Removing a peer now ends its credentials. It used to delete a line and
  nothing else, leaving that peer a live, full-power API token indefinitely,
  and leaving this instance's own credential for it to be inherited by whatever
  was registered under the same name next.
- Nothing announced over multicast is trusted: fields are length-capped on
  arrival and the peer list is bounded, so a device on the network cannot grow
  it without limit.
- Adding a discovered instance exchanges no credentials, and the interface now
  says so instead of claiming otherwise. A password-protected peer added that
  way is reported as having refused, rather than as offline.
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
  happens on its own whenever the other side sets or changes its password, and
  reported as offline it reads as a machine somebody unplugged - so the thing
  that would actually fix it is the last thing anyone would try. The status dot
  has a third state now, and it names the connection phrase.
- A YouTube link no longer lands in a folder called "watch" when its title
  arrives quickly. The name and the folder were decided by two things racing,
  and the one that lost left the guessed folder in place - so the fix worked
  only when the title took its time.
- An instance whose name is not plain ASCII, or is longer than 32 characters,
  can be addressed at all. The name is folded into one that works as a URL path
  segment ("Bürglers Keller" becomes "Burglers Keller"); before, the far side
  refused it as invalid, about a name nobody had typed and nobody could see.
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
