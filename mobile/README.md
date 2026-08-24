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
`internal/api/routes_remote.go`'s own doc comment on why). Onboarding for
v1 is manual:

1. On the server's web UI, open the Access tab and create a named API token
   (`POST /api/tokens` — see `internal/api/routes_tokens.go`). The secret is
   shown once.
2. In the app's connect screen, enter the server's address and paste that
   token in.
3. The app stores the token in the OS keychain (`expo-secure-store`), never
   in plain storage, and sends it as `Authorization: Bearer <token>` on every
   request — the same header a script or the browser extension would use.

A QR-based pairing flow (scan instead of type/paste) is a natural follow-up
once the server grows a QR payload that encodes both the address and a fresh
token in one code, the way the existing Access tab's QR card already encodes
just the address. Not built yet — tracked as an open item, not started.

## Live updates

`GET /api/ws` is the same task/queue stream the web UI subscribes to. React
Native's `WebSocket` supports a non-standard third constructor argument for
headers (browsers' does not), so the token rides as a real `Authorization`
header on the socket too, not a query parameter — see `src/api/client.ts`'s
`subscribeTasks` for the exact mechanics and the reconnect/backoff behaviour.

## Structure

- `src/api/types.ts` — mirrors `internal/core/task.go`'s `Task` shape. Keep
  these in sync with the Go struct, not the other way round.
- `src/api/client.ts` — REST calls plus the WebSocket task subscription.
- `src/storage/connection.ts` — the one saved server connection, in the OS
  keychain.
- `src/screens/` — Connect, Downloads (the live queue), Add Download.
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
currently fails on Windows: the New Architecture native codegen
(`expo-modules-core`, `react-native-screens`, `react-native-safe-area-context`)
hits `ld.lld: error: undefined symbol: operator new/delete` — libc++ isn't
linking. This is a known class of issue (matching upstream reports like
`react-native-reanimated#8269`), normally fixed by pinning NDK
`26.1.10909125` instead of the SDK Manager's default `27.x` — already
configured here via the `expo-build-properties` plugin in `app.json` — but
that pin did not actually resolve it in testing; the autolinked native
modules seem not to read `rootProject.ext.ndkVersion` for their own CMake
config. Not resolved. Full diagnostic trail, including what was tried and
ruled out, is in Claude's memory file
`knightloader-mobile-android-ndk-linker-blocker.md` (not committed to this
repo, ask if you need the details reconstructed here instead).

Until that's fixed, **Expo Go** (see "Running it" above) is the practical way
to test on a real device without a native build at all, and **EAS Build**
(below) sidesteps it entirely by building on Linux.

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

- QR-based pairing (see above).
- Push notifications for captcha challenges / finished downloads — the
  desktop tray already has an attention mechanism for captchas
  (`desktop/tray.go`); the mobile equivalent would need Expo push
  notifications plus a server-side trigger, neither exists yet.
- Only one saved connection; multi-instance support (the desktop/web
  federation dashboard's mobile equivalent) is future scope.
- No task actions yet beyond adding links (pause/resume/delete exist on the
  server's `/api/tasks/*` routes but have no UI here yet).
