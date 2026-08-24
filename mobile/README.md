# KnightLoader — mobile

A companion app for Android and iOS (React Native / Expo, TypeScript). It
does not run a download engine itself — it talks to an already-running
KnightLoader server over the same REST + WebSocket API the web UI and the
browser extension use, the way My.JDownloader's mobile app is a client of a
JDownloader instance rather than a second JDownloader.

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
   paste the token in — or scan a QR code and let it fill in what it can.
   The Access tab carries two different QR codes, and the app's scanner
   (`src/api/pairing.ts`'s `decodePairingCode`) tells them apart rather than
   assuming: the plain remote-access QR encodes just a bare address, and the
   pairing-code QR (see "Instances" below) encodes name + address + a
   one-time token, so scanning THAT one here fills in both the name and the
   address fields at once. Either way the token itself still needs pasting
   by hand: even a decoded pairing offer's token is a short-lived federation
   handshake secret, not a general-purpose bearer API token — see
   `internal/api/routes_pairing.go`'s own doc comment on why. A single scan
   that also carries a real, reusable token needs the server to grow a
   dedicated QR payload for that, which does not exist yet.
3. The app stores every saved connection, tokens included, in the OS
   keychain (`expo-secure-store`), never in plain storage, and sends the
   active one's token as `Authorization: Bearer <token>` on every request —
   the same header a script or the browser extension would use.

### Instances (federation peers)

Once connected, the app also shows the peer instances that server itself
knows about (`GET /api/instances`, `internal/api/routes_federation.go`) —
the mobile equivalent of the web UI's own Instances tab. Opening a peer
shows its queue and lets you add links and flip its queue's master switch,
proxied through the connected server (`/api/instances/{name}/...`); the
proxy only forwards task/link/queue routes, and only plain REST, so a peer's
own queue is polled every few seconds there rather than streamed over the
WebSocket the connected server's own queue uses.

Adding a peer works two ways: type its name and address by hand, or scan the
pairing-code QR from the OTHER instance's own Access tab
(`POST /api/instances/pairing-code`, `internal/api/routes_pairing.go`) —
that code already carries the peer's name, address and a one-time token, so
one scan registers both directions. This is a genuine one-scan flow, unlike
onboarding a brand new saved connection above, because redeeming it happens
on an already-authenticated request to the connected server, which can act
on the token itself server-side — the app never needs to hold or reuse that
token, only relay the scanned code to it.

## Language

The app follows the device's own language setting (`expo-localization`'s
`getLocales()`), picking the first one it has a translation for and falling
back to English — there is no in-app language switcher, since nothing here
depends on overriding the device's choice. `src/i18n/en.ts` is the source of
truth (every UI string as a flat `key: string` dictionary); every other
locale is typed against it, so a translation missing a key — or carrying a
stray one — is a compile error, not a silent English string sneaking through
or a blank one. `src/i18n/index.ts` lazily loads a language's dictionary the
first time it is actually selected, the same interface the web UI's own
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
- `src/api/pairing.ts` — decodes a pairing-code string (pasted, or read off
  a scanned QR) into the name/address/token it carries, entirely client-side
  (mirrors `internal/api/routes_pairing.go`'s own encoding by hand, no
  server round-trip needed just to tell what kind of code it is).
- `src/components/QRScanner.tsx` — a full-screen camera modal
  (`expo-camera`) that hands back one decoded QR string; both scan buttons
  in the screens below use it.
- `src/i18n/` — the translation system; see "Language" above.
- `src/screens/` — Connections (the saved-server list), Connect (add one),
  Downloads (the live queue, a connected server's own or a peer's),
  Instances (that server's federation peers), Add Download.
- `src/theme.ts` — a small dark palette, not the full GlimStone/Carbon token
  set the web UI carries.

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
junctions instead of the real paths. The NDK version itself doesn't matter —
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

- A single scan that onboards a NEW connection with a real, reusable bearer
  token — the initial "add connection" step still needs the token pasted by
  hand even after scanning a pairing-code QR (its token is single-use and
  federation-only); see "How it connects" above for the full reasoning.
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
