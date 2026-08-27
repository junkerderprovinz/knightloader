# KnightLoader — mobile

A companion app for Android and iOS (React Native / Expo, TypeScript). It
does not run a download engine itself — it talks to an already-running
KnightLoader server over the same REST + WebSocket API the web UI and the
browser extension use, the way My.JDownloader's mobile app is a client of a
JDownloader instance rather than a second JDownloader.

## Versions

The app carries its own version in `app.json` and is tagged on its own
(`mobile/vX.Y.Z`), separately from KnightLoader itself - an APK on a phone does
not change when a container is pulled. See the Versioning section of the root
`CHANGELOG.md`.

Bump **both** fields together: `expo.version` is the name people see, and
`expo.android.versionCode` is what Android decides upgrade order by. A build
handed to anybody needs a higher versionCode than the one before it, even when
the version name is unchanged - two builds sharing a versionCode are
indistinguishable to the phone. `.github/workflows/release-mobile.yml` refuses
a tag whose version does not match `app.json`.

**Signing.** The release APK is signed with the Android debug key the Expo
template ships (`CN=Android Debug`) - public, and identical for everyone. That
is what makes a build install cleanly over an earlier one, and it is fine for
installing on your own devices. It is not fine for publishing anywhere: anyone
can sign an APK that Android accepts as an update to it. A real key belongs in
a repository secret, never in the repo.

## Why a companion app, not a second engine

KnightLoader's engine, resolvers and JD sidecar are built to run 24/7 on a
server or desktop, not on a phone that sleeps, gets killed by the OS to save
battery, and is not always on the same network as the hosters it would need
to reach. A remote-control app avoids re-solving all of that for a device
that was never going to run the engine anyway.

## How it connects

There is no relay and no account, the same as the rest of KnightLoader (see
`internal/api/routes_remote.go`'s own doc comment on why). The app can hold
several saved connections — one per KnightLoader server, each with its own
token — and switch between them; onboarding a new one is still manual:

1. On the server's web UI, open the Access tab and create a named API token
   (`POST /api/tokens` — see `internal/api/routes_tokens.go`). The secret is
   shown once.
2. In the app's "add connection" screen, enter the server's address and
   paste the token in — or scan the Access tab's remote-access QR, which
   encodes the address, and paste only the token.

   Or skip all of that: **enter the connection phrase instead**. Twelve
   words, and every instance in the group appears at once, with no address
   and no token to look up. See "Joining a group" below. This numbered path
   is the direct one, for a server this phone can reach on its own.
3. The app stores every saved connection, tokens included, in the OS
   keychain (`expo-secure-store`), never in plain storage, and sends the
   active one's token as `Authorization: Bearer <token>` on every request —
   the same header a script or the browser extension would use.

### Joining a group (the phrase)

The flow above needs a network path from the phone to the instance. When there
is none — every instance behind a NAT with no port forwarding and no reverse
proxy — the phone joins the group instead. Twelve words, and every instance in
it appears at once.

The connect screen's link leads to one field. The words are decoded on the
phone (`src/api/seedphrase.ts`), which derives the same group key the
instances derive, and the app dials the same relay they dial. The relay's
address is compiled in, which is what keeps a phrase to twelve words instead
of a URL plus a key; a group on a self-hosted relay is the one case that still
wants the address typed, and is not wired up here yet.

From there a relay connection behaves like any other: the same screens, the same
calls. `src/api/client.ts`'s `request()` is the only place that knows the
difference, and it swaps `fetch` for a relay frame — so the federation proxy
prefix keeps working through it too, and a relay-reached instance's own peers
stay browsable.

Three things are genuinely different, all of them consequences of the transport
rather than choices:

- **It polls, it does not stream.** The relay carries request/response frames,
  not a tunnelled WebSocket, so there is no `/api/ws` to attach to. `liveTasks()`
  picks streaming or polling per connection; a federation peer already had the
  same limitation for the same reason.
- **No token, even for an instance with a password.** Being on the relay under
  the group key IS the credential now: a request arriving that way came off a
  socket the relay only joins to connections presenting the same key, so the
  instance accepts it. This is what the phrase bought. What it admits is an
  allowlist, not the whole API — tasks, links and the queue, plus reading the
  auth state, the peer list and the instance's own accent. Not the settings,
  not the accounts, not the phrase itself.
- **The phrase is the whole federation's admission ticket.** Every instance in
  the group is reachable by anything holding it, which is worth knowing before
  putting one on a device that gets lost. Leaving the group on that phone does
  not revoke it for anybody else — the phrase is a group, not a per-device
  credential.

Worth being explicit about: **the relay operator carries your frames**, so they
see who is talking and when. What they never see is the phrase — the instances
and the phone all send a hash of it, never the words. Frames themselves are
forwarded as they are, so paths and bodies are visible to whoever runs the
relay. Ours is at `relay.knightloader.app`; run your own if that matters.

The app announces itself to the relay with `client: true` (`relay.Announce`), so
it never appears as a browsable instance on anyone else's Instances page — it
consumes the relay without being something on it. It answers any call made to it
anyway with 501 rather than letting the caller time out.

### Instances (federation peers)

Once connected, the app also shows the peer instances that server itself
knows about (`GET /api/instances`, `internal/api/routes_federation.go`) —
the mobile equivalent of the web UI's own Instances tab. Opening a peer
shows its queue and lets you add links and flip its queue's master switch,
proxied through the connected server (`/api/instances/{name}/...`); the
proxy only forwards task/link/queue routes, and only plain REST, so a peer's
own queue is polled every few seconds there rather than streamed over the
WebSocket the connected server's own queue uses.

Adding a peer here means typing its name and address by hand, which
registers an address and nothing else — a peer with a password will refuse
it. There used to be a second way, a pairing-code QR that carried name,
address and a one-time token, and it was removed along with pairing itself.
What replaced it is the connection phrase: put both instances in the same
group and they authenticate each other by holding the same key, with nothing
to copy per peer.

## Language

The app follows the device's own language setting by default
(`expo-localization`'s `getLocales()`), picking the first one it has a
translation for and falling back to English. Settings → Language can
override that with a specific one instead (stored in `AsyncStorage`, a
plain preference, unlike the connections list's own OS-keychain storage);
picking "Automatic" there clears the override and goes back to following
the device. `src/i18n/en.ts` is the source of truth (every UI string as a
flat `key: string` dictionary); every other locale is typed against it, so a
translation missing a key — or carrying a stray one — is a compile error,
not a silent English string sneaking through or a blank one.
`src/i18n/index.ts` lazily loads a language's dictionary the first time it
is actually selected, the same interface the web UI's own
`lib/locales/index.ts` uses, though on a native bundle every language still
ships inside the one APK either way — see that file's own doc comment.
Covers the same 42-language catalogue as the web UI and the browser
extension, so the family offers one language list rather than three
different ones.

## Live updates

`GET /api/ws` is the same task/queue stream the web UI subscribes to. React
Native's `WebSocket` supports a non-standard third constructor argument for
headers (browsers' does not), so the token rides as a real `Authorization`
header on the socket too, not a query parameter — see `src/api/client.ts`'s
`subscribeTasks` for the exact mechanics and the reconnect/backoff behaviour.

## Structure

- `src/api/types.ts` — mirrors `internal/core/task.go`'s `Task` shape (plus
  `Instance`/`QueueState`, mirroring `internal/federation` and
  `internal/app.QueueState`). Keep these in sync with the Go structs, not
  the other way round.
- `src/api/client.ts` — REST calls (each taking a `base`, `/api` for the
  connected server or `/api/instances/{name}` for a proxied peer), the
  WebSocket task subscription for the connected server, and `pollTasks` as
  its polling equivalent for a peer.
- `src/storage/connections.ts` — every saved connection plus which one is
  active, in the OS keychain.
- `src/api/seedphrase.ts` — twelve words to the group key, entirely on the
  phone, because the whole point of a phrase is the case where there is no
  server to ask yet. A port of `internal/seedphrase`, checked against that
  package's own vectors; `src/api/wordlist.ts` is generated from its
  `english.txt` so the two cannot disagree, and `src/api/sha256.ts` is
  SHA-256 written out rather than a native module.
- `src/api/relayClient.ts` — this app's own client for
  `internal/relay`'s wire protocol; see "When nothing here can reach it at
  all" above. One shared socket per (relay, key), because the relay treats a
  second connection under the same identity as the first one reconnecting.
- `src/api/base64.ts` — base64 and UTF-8 in both directions, by hand rather
  than from the engine (`atob`/`TextEncoder` are not guaranteed present on
  every Hermes build). Used for the relay's frame bodies, which Go marshals
  as base64 `[]byte`.
- `src/storage/relayIdentity.ts` — this device's stable id on a relay.
- `src/components/QRScanner.tsx` — a full-screen camera modal
  (`expo-camera`) that hands back one decoded QR string; both scan buttons
  in the screens below use it.
- `src/i18n/` — the translation system; see "Language" above.
- `src/components/IconBadge.tsx` — the small round glyph buttons in a
  screen's top bar (add, settings) - text glyphs, not an icon font/SVG set,
  matching `QRScanner`'s own "QR" label and the back chevron already used
  elsewhere.
- `src/screens/` — Connections (the saved-server list and the app's own
  landing screen), Connect (add one), RelayConnect (add one that is only
  reachable through a relay), Downloads (the live queue, a connected
  server's own or a peer's), Instances (that server's federation peers),
  Add Download, Settings, Language (the picker Settings opens).
- `src/theme/` — GlimStone, the same design language the web UI carries:
  `tokens.ts` (palette, radii, type scale), `appearance.ts` (the
  framework-free helpers, a straight copy of the shared reference so the two
  never disagree about what "Sunflower" is) and `AppearanceContext.tsx`,
  which resolves instance settings, a local override and the device's
  light/dark into the one object every screen reads. React Native has no CSS
  custom properties and no cascade, so a screen applies colours and radii
  inline from that object rather than from its stylesheet — a
  `StyleSheet.create` block is built once and cannot follow a theme change.

## Why the app allows cleartext HTTP

`app.json` sets `expo-build-properties`' `android.usesCleartextTraffic: true`,
and it has to. Android disables cleartext HTTP by default for any app whose
`targetSdk` is 28 or higher (this one targets 36), and the ordinary
KnightLoader install is a container on the LAN answering plain
`http://192.168.x.x:8749` — `cmd/knightloader/main.go` serves HTTP and
terminates no TLS of its own. Without this flag a release build cannot reach
the most common setup at all, and fails with a transport error that names
nothing.

It was missing until 2026-08-25 and the first release APKs shipped with it
missing, so a plain-HTTP LAN address simply could not connect. Note that debug
builds hide this: Expo adds the flag itself for the dev server, so the failure
only ever appears in a release build.

A per-domain `networkSecurityConfig` would be narrower, but Android matches it
by domain and has no CIDR form, so there is no way to express "permit cleartext
to private address ranges only" — the choice is all or nothing, and for a tool
whose whole job is talking to a self-hosted box on your own network, nothing is
the wrong half. HTTPS is still used whenever the address is `https://`.

## App icon

`assets/icon.png`/`favicon.png` (full-bleed, own white/grey backdrop baked
in) and `assets/android-icon-foreground.png` (the logo alone, on true
transparency, scaled to ~56% of the canvas) are generated from
`.github/assets/kl_app_logo.svg` — a dedicated square variant of the repo's
logo, made for exactly this — by `.github/assets/gen-mobile-icon.mjs`
(`node .github/assets/gen-mobile-icon.mjs` from the repo root). Never hand-export
a new `android-icon-foreground.png` by just re-exporting the flat square icon
at a smaller size: Android's adaptive-icon system masks that layer itself
(circle, squircle, rounded square... the shape varies by launcher) and only
guarantees the inner ~61% of the canvas survives every shape, so a full-bleed
or lightly-padded export gets its edges clipped on most of them — confirmed
live, that was exactly the original bug (jdp: "ist falsch beschnitten. Ich
hab ja extra ein quadratisches gemacht" — a square SOURCE image is right,
but Android's own masking still needs the extra padding on the exported
foreground layer specifically, a different requirement than the plain
`icon.png`/`favicon.png` a square source is otherwise already correct for).

## Running it

```sh
cd mobile
npm install
npm run android   # needs Android Studio/an emulator, or a device with Expo Go
npm run ios       # needs a Mac — see below if you don't have one
```

## Building a local Android APK

`npx expo prebuild --platform android && cd android && ./gradlew assembleDebug`
works, but on Windows it needs one one-time machine setup step first: **the
Android SDK/NDK and the JDK must live at a path with no spaces.** The default
install locations (`C:\Users\<Your Name>\AppData\Local\Android\Sdk`,
`C:\Users\<Your Name>\scoop\apps\...`) break the NDK's native build the
moment the Windows account name contains a space — Windows falls back to an
8.3 short filename (`CLANG~1.EXE`) to invoke `clang++.exe` through the spaced
path, and Clang decides C vs. C++ mode from that exact filename, so losing
the `++` makes it silently compile as C and drop `libc++`. The symptom is
`ld.lld: error: undefined symbol: operator new/delete` (and similar libc++
symbols) failing inside `expo-modules-core`/`react-native-screens`/
`react-native-safe-area-context`, not in this repo's own code.

Fix (one-time, per machine, not something this repo needs to carry):

```powershell
New-Item -ItemType Junction -Path "C:\AndroidSdk" -Target "<your real Android SDK path>"
New-Item -ItemType Junction -Path "C:\Temurin17" -Target "<your real JDK path>"
```

Then set `sdk.dir=C:/AndroidSdk` (forward slashes) in `android/local.properties`
(gitignored, regenerated by `expo prebuild`, so this needs redoing after a
clean prebuild) and build with `JAVA_HOME`/`ANDROID_HOME` pointed at the
junctions instead of the real paths.

Also delete the CMake caches, which remember the old spaced toolchain path and
are the reason a rebuild after fixing the above can fail identically. Two of
them live outside `android/` and survive `expo prebuild --clean`:

```bash
rm -rf android/app/.cxx node_modules/*/android/.cxx
```
 The NDK version itself doesn't matter —
whatever the SDK Manager installs by default works fine once the path is
space-free; don't chase an NDK-version pin, `expo-build-properties` doesn't
even have an `ndkVersion` option in the version this project uses. Full
diagnostic trail (including the dead ends before this was found) is in
Claude's memory file `knightloader-mobile-android-ndk-linker-blocker.md`, not
committed to this repo, ask if you need it reconstructed here instead.

**Expo Go** (see "Running it" above) is still the faster loop for day-to-day
UI/logic iteration, no build step at all. **EAS Build** (below) is the way to
get a build without a local toolchain at all, e.g. from a machine that isn't
set up for Android development, or once actual signed release builds are
needed.

## Building without a Mac (iOS)

Xcode only runs on macOS, so a local `ios` build is not possible from this
machine. [EAS Build](https://docs.expo.dev/build/introduction/) (Expo's
hosted build service) builds signed iOS binaries in the cloud from any
machine — no local Mac needed to produce the `.ipa`. It does need:

- An Expo account, logged in via `eas login` (`npx eas-cli login`).
- An Apple Developer Program membership to sign and eventually submit to
  TestFlight/the App Store — the same kind of account-gated step as the
  browser extension's store listings, not something that can be set up on
  your behalf.

Once both exist: `npx eas-cli build --platform ios` for a cloud build, or
`--platform android` for a cloud Android build instead of a local one. Not
run yet — this repo has no `eas.json` committed until there's an Expo
project to point it at.

## Not done yet

- Scanning the phrase QR. The web UI shows one next to the twelve words,
  and the phone still wants them typed. The scanner and the decoder are both
  already here, so this is wiring, not design.
- No keepalive on the relay socket. The Go client pings every 30s to hold the
  connection open through a reverse proxy that drops idle upstreams; the
  WebSocket API React Native exposes cannot send a ping frame at all, so this
  client relies on reconnecting after the drop instead. In practice an open
  Downloads screen polls often enough to keep the link warm, and a backgrounded
  app reconnects when it comes back.
- Push notifications for captcha challenges / finished downloads — the
  desktop tray already has an attention mechanism for captchas
  (`desktop/tray.go`); the mobile equivalent would need Expo push
  notifications plus a server-side trigger, neither exists yet.
- Per-task actions beyond adding links — pause/resume/delete a single task
  exist on the server's `/api/tasks/*` routes but have no UI here yet; only
  the queue's whole master switch does (the halted/running toggle on the
  Downloads screen).
- RTL layout mirroring — Arabic, Hebrew and Persian have real translations
  (see "Language" above), but the screens themselves are not mirrored for
  right-to-left reading yet, unlike the web UI.
