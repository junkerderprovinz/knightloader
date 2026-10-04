# KnightLoader mobile

A companion app for Android and iOS (React Native / Expo, TypeScript). It
does not run a download engine itself. It talks to running KnightLoader
instances over the same REST API the web UI uses, carried through the relay
(see "How it connects"), the way My.JDownloader's mobile app is a client of a
JDownloader instance rather than a second JDownloader.

## Versions

The app has KnightLoader's version and is released with everything else under
the `vX.Y.Z` tag, as `knightloader-vX.Y.Z-android.apk` and
`knightloader-android.apk`. `expo.version` in `app.json` names it, and
`.github/workflows/release.yml` refuses a tag that it does not match. See the
Versioning section of the root `CHANGELOG.md`.

`app.json` carries no `versionCode`. Android decides upgrade order by it, so
the release build works it out from the version as
`major * 10000 + minor * 100 + patch`, and every release gets a higher one than
the release before.

**Signing.** A release APK is signed with KnightLoader's own key, which lives in
repository secrets and never in the repository. `plugins/withReleaseSigning.js`
picks it up from the environment, and the release workflow checks the
certificate of the finished APK. A local build without those variables falls
back to the Android debug key the Expo template ships, which is public and fine
only for your own devices.

## Why a companion app, not a second engine

KnightLoader's engine, resolvers and JD sidecar are built to run 24/7 on a
server or desktop, not on a phone that sleeps, gets killed by the OS to save
battery, and is not always on the same network as the hosters it would need
to reach. A remote-control app avoids re-solving all of that for a device
that was never going to run the engine anyway.

## How it connects

The connection phrase is the one way in, as it is in the browser extension.
The **+** on the overview, or the button on its empty screen, opens a single
field: type the twelve words, paste them, or scan the QR the web UI shows
beside them. Every instance in the group then appears at once and is saved as
a connection of its own, with no address and no token to look up. The overview
is the list of those connections, and the app switches between them.

A connection saved by address in an earlier build keeps working if the address
is `https://`, but the app no longer makes one. One saved with an `http://`
address cannot work, because the app permits no cleartext traffic; its card
says it has to be added again with the twelve words. Those builds took the
server's address and an API token
from the web UI's Access tab (`POST /api/tokens`, see
`internal/api/routes_tokens.go`). The app only uses Read, Add and Control, so a
Custom token with those three was enough. Such a connection sends its token as
`Authorization: Bearer <token>` on every request, the same header a script
would use.

Every saved connection sits in the OS keychain (`expo-secure-store`), never in
plain storage: one made with the phrase holds the group key, one saved by
address holds its token.

### Joining a group (the phrase)

The phone joins the group as a member rather than as a client of one instance,
so it needs no network path to any of them. The words are decoded on the
phone (`src/api/seedphrase.ts`), which derives the same group key the
instances derive, and the app dials the same relay they dial. The default
relay's address is compiled in (`DEFAULT_RELAY_URL`), which is what keeps a
phrase to twelve words instead of a URL plus a key. A group on a self-hosted
relay reaches the app through the QR code: an instance on its own relay puts
that relay's address on a second line under the words (`relay.PairingCode`),
and `src/api/pairingCode.ts` reads it back and takes it only at a `wss://`
address. Typed words always pair on the default relay.

From there a relay connection behaves like one saved by address: the same
screens, the same calls. `src/api/client.ts`'s `request()` is the only place
that knows the difference, and it swaps `fetch` for a relay frame.

Three things are different, all of them consequences of the transport rather
than choices:

- **It polls, it does not stream.** The relay carries request/response frames,
  not a tunnelled WebSocket, so there is no `/api/ws` to attach to. `liveTasks()`
  picks streaming or polling per connection; a federation peer already had the
  same limitation for the same reason.
- **No token, even for an instance with a password.** Being on the relay under
  the group key is the credential: a request arriving that way came off a
  socket the relay only joins to connections presenting the same key, so the
  instance accepts it. This is what the phrase bought. What it admits is an
  allowlist, not the whole API (`relayForwardable` in
  `internal/api/routes_relay.go`): tasks, links, the queue and the captchas
  holding it up, the instance's look to read and to set, plus reading the auth
  state, the peer list and the addresses the instance answers on. Not the
  settings, not the accounts, not the phrase itself.
- **The phrase is the whole federation's admission ticket.** Every instance in
  the group is reachable by anything holding it, which is worth knowing before
  putting one on a device that gets lost. Leaving the group on that phone does
  not revoke it for anybody else. The phrase is a group, not a per-device
  credential.

**The relay operator carries your frames**, so they see who is talking and
when. They cannot read the frames: each proxy frame is sealed with AES-256-GCM
under a second key derived from the same phrase (`src/api/relayFrame.ts`), so
the relay sees which instance a frame is for and which request it answers, and
nothing of its path or body. It never sees the phrase either: the instances
and the phone send only a hash of it. Ours is at `parleyport.halleluja.design`; run
your own if the metadata matters.

The app announces itself to the relay with `client: true` (`relay.Announce`), so
it never appears as a browsable instance on anyone else's Instances page. It
consumes the relay without being something on it. It answers any call made to it
anyway with 501 rather than letting the caller time out.

### Instances

There is no Instances screen. Every member of the group is a connection of its
own on the overview, so the overview is the list of instances. The Downloads
and Add Download screens keep a branch for a federation peer reached through
the connected instance's proxy (`/api/instances/{name}/...`), but nothing in
the app opens it.

## Language

The app follows the device's own language setting by default
(`expo-localization`'s `getLocales()`), picking the first one it has a
translation for and falling back to English. Settings → Language can
override that with a specific one instead (stored in `AsyncStorage`, a
plain preference, unlike the connections list's own OS-keychain storage);
picking "Automatic" there clears the override and goes back to following
the device. `src/i18n/en.ts` is the source of truth (every UI string as a
flat `key: string` dictionary); every other locale is typed against it, so a
translation missing a key, or carrying a stray one, is a compile error,
not a silent English string sneaking through or a blank one.
A value that is still English passes that check, so `untranslated.json`
names every one: `locales` lists what still waits for a translation,
`identical` what was checked and stays English (PayPal, Premium, Server).
`node check-untranslated.mjs` fails on an English value that neither list
names. A new key therefore goes into every catalogue in English, and
`node seed-untranslated.mjs` lists it as owed; `node prune-untranslated.mjs`
takes out what has been translated since. The file has the same format as
the web UI's `web/untranslated.json`. German is never listed, because it is
written by hand.
`src/i18n/index.ts` lazily loads a language's dictionary the first time it
is actually selected, the same interface the web UI's own
`lib/locales/index.ts` uses, though on a native bundle every language still
ships inside the one APK either way. See that file's own doc comment.
Covers the same 42-language catalogue as the web UI and the browser
extension, so the family offers one language list rather than three
different ones.

## Live updates

A connection saved by address follows `GET /api/ws`, the same task/queue
stream the web UI subscribes to; one made with the phrase polls, as above. React
Native's `WebSocket` supports a non-standard third constructor argument for
headers (browsers' does not), so the token rides as a real `Authorization`
header on the socket too, not a query parameter. See `src/api/client.ts`'s
`subscribeTasks` for the exact mechanics and the reconnect/backoff behaviour.

## Captchas

A captcha that holds up a download shows in three places: a card at the top of
that instance's downloads, a count on its card in the overview, and a banner over
whatever screen is open when a new one arrives. Each leads to the Captchas
screen, which lists what is waiting, nearest deadline first, and answers what it
can:

- **Picture and click captchas** are answered on the phone. Type what the
  picture says, or tap the points it asks for; the taps go out in the picture's
  own pixels, in the shape JD takes (`clickAnswer` in `src/api/captcha.ts`).
- **reCAPTCHA, hCaptcha and Cloudflare Turnstile** open the instance's own
  widget page (`internal/api/routes_captcha_widget.go`) in a WebView, on
  either kind of connection. The app asks for it on the phone's route,
  `GET /api/captcha/{id}/widget/phone` (`fetchCaptchaWidgetPage`), which the
  relay forwards like the other captcha calls. The instance looks the
  challenge up and answers JSON: the page's HTML and the hoster's page address
  from the challenge (JD's `siteUrl`, or its `contextUrl` when that is
  missing). The WebView loads the HTML with `source={{ html, baseUrl }}`, so
  the page's origin is the hoster's, the one a browser on that page would
  show. That is what a site key locked to the hoster's domains needs, and
  every Turnstile key is locked that way. The page is the one the web UI puts
  in an iframe, so the app runs no vendor script of its own and the vendor
  loading lives in one place. Its Content-Security-Policy travels in a meta
  element, without `'self'`, which would be the hoster, and without
  `frame-ancestors`, which a meta element cannot carry. The page posts its
  result to its own origin, and in a WebView it is the top window, so the
  message lands on the page itself; `WIDGET_BRIDGE` hands it to the app, and
  the token goes back through `/api/captcha/{id}/answer` like any answer.
  The WebView runs with `scalesPageToFit={false}`, because the page sets no
  viewport and a wide one draws the checkbox at a third of its size.

  When the page cannot be fetched, the vendor's script does not arrive or the
  vendor reports an error, the window says who is to blame (`widgetFailure`):
  the instance with its status, such as the 400 for a challenge JD sent
  without a site key, an instance too old for the phone's route (a bare 403
  over the relay), the network, or the vendor with its code. It also reports
  the failure on the phone's own path (`reportCaptchaUnanswerable`,
  `/api/captcha/{id}/unanswerable/phone`), and the phone's reads of the list
  stop holding the captcha accounts back for it. Once Refresh loads the widget
  after all, the app withdraws the report (`widgetReport`, through
  `reportWidget` in `CaptchaWatch`). A challenge answered or dropped while
  the window opened closes it (a 404 with the code `gone`). An instance from
  before the phone's route answers it with a plain 404; on a connection saved
  by address the app then loads the web UI's page from the instance's own
  address, as it used to, where a Turnstile gets the page's refusal.
- Anything else shows JD's name for it and offers Cancel.

The list comes from `/api/captcha`, the route the web UI's `CaptchaModal` reads,
polled every five seconds while the app is in front (`CaptchaWatch`), over
either transport, for the active connection only. The read names what this
phone answers in `watch` (`WATCHED`): pictures, clicks and widgets, and
`turnstile` to say that it runs a Turnstile as well, which an older app that
names only the widget kind cannot. With "Only when nobody is watching" on, the
instance holds the captcha accounts back for those alone. A card
shows what the captcha accounts are doing (`solverStatus`), and leaves out the
explanation that assumes you can answer when the phone cannot. An instance forwards these
routes over the relay (`relayCaptchaRoute` in `internal/api/routes_relay.go`);
an older one refuses them with a 403, which the screen words as "update
KnightLoader there". The overview counts the captchas on every saved instance
from the `captchas` field of `/api/queue/counters`, which it reads anyway, so
counting sends no pictures.

The relay carries no socket, so the app does not get the web UI's
`captchaResolved` event. `noticeFor` works out the same thing from two looks
in a row: a captcha that has gone after its deadline timed out, one gone before
it was answered or dropped elsewhere, unless this phone answered or skipped it
itself. The banner then says so, as the web UI's toast does.

**Notifications while the app is closed.** `modules/watch` is a small native
module with an Android foreground service. The service runs one pass of
`src/watch/watch.ts` as a headless JavaScript task: every 10 to 30 seconds
while a download runs or a captcha waits, once a minute while nothing does. A
pass reads `/api/tasks/watch` and `/api/captcha?watch=0` from every saved
instance, over the same direct or relay transport the screens use, and posts a
notification for a captcha that arrived, a package or lone download that
finished, and a download that failed for good. A failure the instance retries by
itself is no news yet. `/api/tasks/watch` sends each task's state without its
progress, and given the tag of its last answer it sends only the tag until
something changes, so a long history costs a few bytes a look. An instance from
before that route is read through `/api/tasks`. A look that gets no answer
within 20 seconds counts as one the instance missed. The relay carries no
socket, so asking is the only way to hear about anything, and `watch=0` keeps a
phone in a pocket from holding the captcha accounts back. Nothing here comes from Firebase or any
other Google service.

"Stay connected", on by default, keeps the service running for as long as an
instance is saved, so a download that starts on the instance while the app is
closed is noticed too. `WatchBoot` starts it again after a reboot and after an
update of the app; both broadcasts are among the cases where Android 14 and 15
still allow a foreground service from the background, and Android 15's list of
types a boot receiver may not start leaves `specialUse` out. The app stores in
native preferences whether that applies, since the receiver has no JavaScript to
ask. With the setting off, the service stops itself two minutes after the last
busy look, or fifteen when an instance stopped answering while it was busy, and
the app starts it from the front: when it is opened, every 15 seconds while it
is open and something is running, and when a download is added. Nothing is
announced for what finished while nobody looked, after the service stopped or
while the app was away without it: the next look starts over like the first.

While a download runs or a captcha waits, the service holds a partial wake lock
and a handler times the passes, since that clock stops while the phone sleeps;
the pace drops to one look every 30 seconds while nothing changes. While
nothing runs, it lets go of the lock and an inexact alarm that is allowed while
idle wakes the phone for the next look, so an idle phone sleeps between looks.
A download that stands still and a torrent a debrid service is still fetching
count as nothing running here, since either can stay that way for hours, but
with "Stay connected" off they still keep the service going.
The first time the service starts with "Stay connected", the app offers
Android's battery optimisation list once, and the settings card keeps a button
to it while the app is optimised. It opens the list rather than asking for the
exemption directly, because REQUEST_IGNORE_BATTERY_OPTIMIZATIONS is a Google
Play policy question and one build goes to both stores. On phones whose maker
adds background rules of its own (Xiaomi, OnePlus, OPPO, vivo, Huawei, Samsung,
Asus) the card also offers that maker's autostart or background page, falling
back to the app's details. The instance open on screen while the app is in front gets no notifications, since
its banner and its list already say it. The rules (what counts as news, when to
stop, how fast to look) live in `src/watch/rules.ts`, which
`check-watch.mjs` runs as they are.

## Structure

- `src/api/types.ts`: mirrors `internal/core/task.go`'s `Task` shape (plus
  `Instance`/`QueueState`, mirroring `internal/federation` and
  `internal/app.QueueState`). Keep these in sync with the Go structs, not
  the other way round.
- `src/api/client.ts`: REST calls (each taking a `base`, `/api` for the
  connected instance or `/api/instances/{name}` for a proxied peer), the
  WebSocket task subscription for a connection saved by address, and
  `pollTasks` as its polling equivalent for the relay.
- `src/api/captcha.ts`: the rules the captcha screen follows (the order, the
  countdown, a click answer, the widget page's paths and the vendors it runs,
  which of the page's messages count, who to blame when it does not load and
  when to tell the instance, the bridge script, and what the banner says after
  a look), kept free of React so
  `check-captcha.mjs` runs them as they are.
- `src/components/CaptchaWatch.tsx`, `CaptchaCard.tsx` and `CaptchaWidget.tsx`:
  the watch and its banner, one captcha's card, and the WebView window. See
  "Captchas" above. `react-native-webview` is a native module, so it needs a
  build of the app rather than an update over Expo Go's bundle.
- `src/watch/` and `modules/watch/`: the background watch behind the
  notifications, its rules, its settings and the foreground service that runs
  it. See "Captchas" above.
- `src/storage/connections.ts`: every saved connection plus which one is
  active, in the OS keychain.
- `src/api/seedphrase.ts`: twelve words to the group key, entirely on the
  phone, because a phrase exists for the case where there is no server to ask
  yet. A port of `internal/seedphrase`, checked against that
  package's own vectors; `src/api/wordlist.ts` is generated from its
  `english.txt` so the two cannot disagree, and `src/api/sha256.ts` is
  SHA-256 written out rather than a native module.
- `src/api/relayClient.ts`: this app's own client for
  `internal/relay`'s wire protocol; see "Joining a group" above. One shared
  socket per (relay, key), because the relay treats a second connection
  under the same identity as the first one reconnecting.
- `src/api/base64.ts`: base64 and UTF-8 in both directions, by hand rather
  than from the engine (`atob`/`TextEncoder` are not guaranteed present on
  every Hermes build). Used for the relay's frame bodies, which Go marshals
  as base64 `[]byte`.
- `src/storage/relayIdentity.ts`: this device's stable id on a relay.
- `src/components/QRScanner.tsx`: a full-screen camera modal
  (`expo-camera`) that hands back one decoded QR string; the phrase screen's
  scan button uses it.
- `src/i18n/`: the translation system; see "Language" above.
- `src/components/IconBadge.tsx`: the small round glyph buttons in a
  screen's top bar (add, settings) - text glyphs, not an icon font/SVG set,
  matching `QRScanner`'s own "QR" label and the back chevron already used
  elsewhere.
- `src/screens/`: Connections (the overview of saved connections and the
  app's landing screen), RelayConnect (the phrase, the one way to add
  connections), Downloads (one instance's live queue), Add Download, Captchas,
  Settings, Language (the picker Settings opens).
- `src/theme/`: GlimStone, the same design language the web UI carries:
  `tokens.ts` (palette, radii, type scale), `appearance.ts` (the
  framework-free helpers, a straight copy of the shared reference so the two
  never disagree about what "Sunflower" is) and `AppearanceContext.tsx`,
  which resolves instance settings, a local override and the device's
  light/dark into the one object every screen reads. React Native has no CSS
  custom properties and no cascade, so a screen applies colours and radii
  inline from that object rather than from its stylesheet: a
  `StyleSheet.create` block is built once and cannot follow a theme change.

  Motion is the second axis and sits in its own pair, `motion.ts` (the table
  of figures per level, plus the gesture that reveals the level no picker
  lists) and `MotionContext.tsx`. Most of the table is GlimStone's
  `reference/motionNative.ts`, copied unchanged as `motionNative.ts`, so the
  cards arriving on a page, a button giving way under the finger and a flung
  page springing back at its edge move as they do in the other apps of the
  family. `src/components/Moving.tsx` puts them on the pages and lists, and
  `motion.ts` adds only the gestures this app has on its own. Motion is
  separate from appearance because an instance may lead on colour and corners
  and has no business leading on this: how much a phone moves belongs to that
  phone, and half of it is an operating-system setting. `MotionContext` is the **only** reader of
  `AccessibilityInfo.isReduceMotionEnabled` in the app, and that is the phone's
  stand-in for the web's `@media (prefers-reduced-motion: no-preference)`
  block: there the accessibility signal wins because of where the CSS sits,
  here because one function hands every level out and answers `off` while the
  signal is on. `check-hidden-motion-level.mjs` fails if a second reader
  appears.

  The type is Noto Sans, GlimStone's house font, from
  `@expo-google-fonts/noto-sans` as one static cut per weight (400, 500, 600,
  700) in `font.ts`, loaded by `expo-font` before the first screen draws.
  Android cannot pick a weight out of a font loaded at runtime, so
  `src/components/Text.tsx` wraps `Text` and `TextInput` and trades each
  style's `fontWeight` for the family of that cut. Screens import those two
  from there, never from `react-native`; `check-house-font.mjs` fails if one
  does not.

## No cleartext traffic

Android refuses plain HTTP to an app whose `targetSdk` is 28 or higher (this
one targets 36), and `app.json` leaves it at that. Everything the app opens
goes to a relay over `wss://`: the default relay, or the one an instance's QR
code names, which `src/api/pairingCode.ts` refuses unless it is `wss://`.
Debug builds allow cleartext anyway, because Expo turns it on for the dev
server, so only a release build shows what is blocked.

The one thing left that would need HTTP is a connection saved by address in a
build before 1.5.0, when the app still reached a LAN address such as
`http://192.168.x.x:8749` directly. `needsAddingAgain` in `src/api/types.ts`
marks such a connection, and `request()` refuses it with a sentence that asks
for the twelve words rather than a transport error that names nothing.

## App icon

`assets/icon.png`/`favicon.png` (full-bleed, own white/grey backdrop baked
in) and `assets/android-icon-foreground.png` (the logo alone, on true
transparency, scaled to ~56% of the canvas) are generated from
`.github/assets/kl_app_logo.svg`, a dedicated square variant of the repo's
logo made for exactly this, by `.github/assets/gen-mobile-icon.mjs`
(`node .github/assets/gen-mobile-icon.mjs` from the repo root). Never hand-export
a new `android-icon-foreground.png` by just re-exporting the flat square icon
at a smaller size: Android's adaptive-icon system masks that layer itself
(circle, squircle, rounded square... the shape varies by launcher) and only
guarantees the inner ~61% of the canvas survives every shape, so a full-bleed
or lightly-padded export gets its edges clipped on most of them, confirmed
live, and that is exactly what went wrong the first time. A square source
image is right, but Android's own masking still needs the extra padding on
the exported foreground layer specifically, a different requirement than the
plain `icon.png`/`favicon.png` a square source is otherwise already correct
for.

The logo is the author's own drawing and falls under the repository's licence,
AGPL-3.0-only, like the code. The KnightLoader name and logo remain the author's
trademarks, so a fork needs a name and logo of its own.

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
moment the Windows account name contains a space. Windows falls back to an
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
 The NDK version itself doesn't matter:
whatever the SDK Manager installs by default works fine once the path is
space-free; don't chase an NDK-version pin, `expo-build-properties` doesn't
even have an `ndkVersion` option in the version this project uses.

**Expo Go** (see "Running it" above) is still the faster loop for day-to-day
UI/logic iteration, no build step at all. **EAS Build** (below) is the way to
get a build without a local toolchain at all, e.g. from a machine that isn't
set up for Android development, or once actual signed release builds are
needed.

## Building without a Mac (iOS)

Xcode only runs on macOS, so a local `ios` build is not possible from this
machine. [EAS Build](https://docs.expo.dev/build/introduction/) (Expo's
hosted build service) builds signed iOS binaries in the cloud from any
machine, with no local Mac needed to produce the `.ipa`. It does need:

- An Expo account, logged in via `eas login` (`npx eas-cli login`).
- An Apple Developer Program membership to sign and eventually submit to
  TestFlight/the App Store, the same kind of account-gated step as the
  browser extension's store listings, not something that can be set up on
  your behalf.

Once both exist: `npx eas-cli build --platform ios` for a cloud build, or
`--platform android` for a cloud Android build instead of a local one. Not
run yet: this repo has no `eas.json` committed until there's an Expo
project to point it at.

## Not done yet

- No keepalive on the relay socket. The Go client pings every 30s to hold the
  connection open through a reverse proxy that drops idle upstreams; the
  WebSocket API React Native exposes cannot send a ping frame at all, so this
  client relies on reconnecting after the drop instead. In practice an open
  Downloads screen polls often enough to keep the link warm, and a backgrounded
  app reconnects when it comes back.
- reCAPTCHA and hCaptcha over the relay; see "Captchas" above for why.
- Per-task actions beyond adding links: pause/resume/delete a single task
  exist on the server's `/api/tasks/*` routes but have no UI here yet; only
  the queue's whole master switch does (the halted/running toggle on the
  Downloads screen).
- RTL layout mirroring. Arabic, Hebrew and Persian have real translations
  (see "Language" above), but the screens themselves are not mirrored for
  right-to-left reading yet, unlike the web UI.
